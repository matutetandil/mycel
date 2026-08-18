package push

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sending to Firebase the way Firebase still accepts.
//
// The connector spoke the legacy API — POST /fcm/send with a server key — which
// Google retired in June 2024. Every push through it has failed since, and the
// two settings for the API that replaced it, project_id and
// service_account_json, were read by nothing: the fields existed, the
// documentation listed them, and the code kept posting to the endpoint that is
// gone.
//
// The v1 API wants a service account rather than a shared key: a JWT signed
// with the account's private key, exchanged for an access token, which then
// authorises a post to a URL naming the project.

// googleTokenURL is where a signed assertion is exchanged for an access token.
const googleTokenURL = "https://oauth2.googleapis.com/token"

// fcmScope is the only scope this needs.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// serviceAccount is the part of a Firebase service account file that matters
// here.
type serviceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

// loadServiceAccount reads a service account from a path or from the JSON
// itself.
//
// Both, because a file is what Google hands you and an environment variable is
// what a container has: a deployment writing service_account_json = env("...")
// should not have to put the file on disk first.
func loadServiceAccount(pathOrJSON string) (*serviceAccount, error) {
	raw := []byte(pathOrJSON)
	if !strings.HasPrefix(strings.TrimSpace(pathOrJSON), "{") {
		read, err := os.ReadFile(pathOrJSON)
		if err != nil {
			return nil, fmt.Errorf("could not read the service account file %q: %w", pathOrJSON, err)
		}
		raw = read
	}

	account := &serviceAccount{}
	if err := json.Unmarshal(raw, account); err != nil {
		return nil, fmt.Errorf("the service account is not JSON: %w", err)
	}
	if account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, fmt.Errorf("the service account has no client_email or private_key, so it cannot sign anything")
	}
	if account.TokenURI == "" {
		account.TokenURI = googleTokenURL
	}
	return account, nil
}

// signingKey parses the PEM private key out of a service account.
func (a *serviceAccount) signingKey() (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(a.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("the service account's private_key is not PEM")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("the service account's private_key is not an RSA key")
		}
		return rsaKey, nil
	}
	// Older files are PKCS#1.
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the service account's private_key could not be read: %w", err)
	}
	return key, nil
}

// googleTokenSource turns a service account into access tokens, and holds on to
// one until shortly before it expires.
type googleTokenSource struct {
	account *serviceAccount
	key     *rsa.PrivateKey
	client  *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newGoogleTokenSource(account *serviceAccount, client *http.Client) (*googleTokenSource, error) {
	key, err := account.signingKey()
	if err != nil {
		return nil, err
	}
	return &googleTokenSource{account: account, key: key, client: client}, nil
}

// Token returns an access token, minting a new one when the held one is close
// enough to expiry that a request might outlive it.
func (s *googleTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.expiresAt.Add(-time.Minute)) {
		return s.token, nil
	}

	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   s.account.ClientEmail,
		"scope": fcmScope,
		"aud":   s.account.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}).SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("could not sign the token request: %w", err)
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.account.TokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("could not reach Google to exchange the token: %w", err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google refused the token request with %d: %s", response.StatusCode, string(body))
	}

	var answer struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", fmt.Errorf("Google's token answer is not JSON: %w", err)
	}
	if answer.AccessToken == "" {
		return "", fmt.Errorf("Google returned no access token")
	}

	s.token = answer.AccessToken
	s.expiresAt = time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	return s.token, nil
}

// buildV1Message turns a message into the shape the v1 API takes.
//
// It is not the legacy shape with a field renamed: one message goes to one
// target, data values must be strings, and the time to live is a duration
// string under an android block rather than a number.
func buildV1Message(msg *Message, token string) map[string]interface{} {
	message := map[string]interface{}{}

	switch {
	case token != "":
		message["token"] = token
	case msg.Topic != "":
		message["topic"] = msg.Topic
	case msg.Condition != "":
		message["condition"] = msg.Condition
	}

	if msg.Title != "" || msg.Body != "" {
		message["notification"] = map[string]string{
			"title": msg.Title,
			"body":  msg.Body,
		}
	}

	// Every value has to be a string here, where the legacy API took anything.
	if len(msg.Data) > 0 {
		data := make(map[string]string, len(msg.Data))
		for k, v := range msg.Data {
			data[k] = fmt.Sprint(v)
		}
		message["data"] = data
	}

	android := map[string]interface{}{}
	if msg.Priority != "" {
		// The v1 API knows "normal" and "high" only.
		priority := strings.ToLower(msg.Priority)
		if priority != "normal" {
			priority = "high"
		}
		android["priority"] = priority
	}
	if msg.TTL > 0 {
		android["ttl"] = fmt.Sprintf("%ds", msg.TTL)
	}
	if msg.CollapseKey != "" {
		android["collapse_key"] = msg.CollapseKey
	}
	if len(android) > 0 {
		message["android"] = android
	}

	return map[string]interface{}{"message": message}
}

// sendV1 posts one message and reports what came back.
func (c *FCMConnector) sendV1(ctx context.Context, msg *Message) (*SendResult, error) {
	accessToken, err := c.tokens.Token(ctx)
	if err != nil {
		return &SendResult{Success: false, Provider: "fcm", Error: err.Error()}, err
	}

	// The v1 API takes one target per request, where the legacy one took a list.
	// Sending them one at a time is what the API allows, and it means a token
	// that has been uninstalled fails on its own rather than failing the batch.
	targets := msg.Tokens
	if msg.Token != "" {
		targets = []string{msg.Token}
	}
	if len(targets) == 0 {
		targets = []string{""} // a topic or a condition
	}

	var (
		lastID       string
		failedTokens []string
		lastErr      error
		delivered    int
	)

	for _, target := range targets {
		id, err := c.postV1(ctx, accessToken, buildV1Message(msg, target))
		if err != nil {
			lastErr = err
			if target != "" {
				failedTokens = append(failedTokens, target)
			}
			continue
		}
		delivered++
		lastID = id
	}

	if delivered == 0 {
		message := "no message was accepted"
		if lastErr != nil {
			message = lastErr.Error()
		}
		return &SendResult{
			Success: false, Provider: "fcm", Error: message, FailedTokens: failedTokens,
		}, lastErr
	}

	return &SendResult{
		Success:      true,
		Provider:     "fcm",
		MessageID:    lastID,
		FailedTokens: failedTokens,
	}, nil
}

func (c *FCMConnector) postV1(ctx context.Context, accessToken string, payload map[string]interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s/v1/projects/%s/messages:send", c.config.FCM.APIURL, c.projectID())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	answer, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("FCM answered %d: %s", response.StatusCode, string(answer))
	}

	var accepted struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(answer, &accepted)
	return accepted.Name, nil
}

// projectID is the project the messages are sent to: what was configured, or
// what the service account already says.
func (c *FCMConnector) projectID() string {
	if c.config.FCM.ProjectID != "" {
		return c.config.FCM.ProjectID
	}
	if c.account != nil {
		return c.account.ProjectID
	}
	return ""
}
