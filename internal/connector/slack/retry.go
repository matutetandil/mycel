package slack

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// maxRetryAfter caps the honored Retry-After delay. Slack rarely asks for
// more than a few seconds, and a notification connector waiting minutes for a
// single message would defeat the purpose of the batcher feeding it.
const maxRetryAfter = 30 * time.Second

// defaultRetryAfter is used when Slack returns a 429 with no parseable
// Retry-After header — better than retrying immediately and getting throttled
// again.
const defaultRetryAfter = 1 * time.Second

// doWithRateLimitRetry executes the request and, on a 429 response, parses
// Retry-After, waits the indicated delay (honoring the context), and retries
// the request exactly once. This complements the connector's default-on
// batching: the batcher keeps you under Slack's soft "high volume" cap,
// and this retry covers the rare burst that still trips the hard rate limit.
//
// Body is taken as bytes so the second attempt can rebuild a fresh
// io.Reader — http.Request bodies are single-use.
func (c *Connector) doWithRateLimitRetry(
	ctx context.Context,
	method, url string,
	headers map[string]string,
	body []byte,
) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		// Only the first attempt may retry.
		if resp.StatusCode != http.StatusTooManyRequests || attempt > 0 {
			return resp, nil
		}

		delay := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		// Drain + close the throttled response so the connection can be
		// reused for the retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		c.logger.Info("slack rate-limited (429), retrying after Retry-After",
			"connector", c.name,
			"delay", delay)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// Unreachable: the loop returns inside.
	return nil, nil
}

// parseRetryAfter interprets the Retry-After header per RFC 7231: either a
// non-negative integer of seconds, or an HTTP-date. Unknown / malformed
// values fall back to defaultRetryAfter; the result is clamped to
// [defaultRetryAfter, maxRetryAfter] so a malicious or buggy server cannot
// stall the connector indefinitely.
func parseRetryAfter(header string, now time.Time) time.Duration {
	if header == "" {
		return defaultRetryAfter
	}
	if n, err := strconv.Atoi(header); err == nil {
		return clampRetry(time.Duration(n) * time.Second)
	}
	if t, err := http.ParseTime(header); err == nil {
		return clampRetry(t.Sub(now))
	}
	return defaultRetryAfter
}

func clampRetry(d time.Duration) time.Duration {
	if d < defaultRetryAfter {
		return defaultRetryAfter
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
