package runtime

import (
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/parser"
)

// ValidateCacheKeys refuses a cache key written as a CEL expression.
//
// A cache key is a template: `${...}` is substituted and everything else is
// the key, verbatim. A key written the way a `lock` or `dedupe` key is —
// `'product:' + input.id` — is therefore used as it stands, quotes and plus
// signs included, and nothing fails: not validate, not startup, not the
// request. Every product shares that one entry, the first request fills it
// and every later request for any id gets the first product's data back for
// the life of the TTL. The symptom is users seeing the wrong record, which
// looks nothing like a cache key.
//
// The two forms are told apart by what is left once the `${...}` spans are
// removed: a template's remainder is plain text, an expression's carries
// quotes, a `+`, or a reference into the message. Nobody writes a constant
// key with quotes in it on purpose.
func ValidateCacheKeys(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil || f.Cache == nil {
			continue
		}
		if err := cacheKeyLooksLikeCEL(f.Cache.Key); err != nil {
			errs = append(errs, fmt.Errorf("flow %q: %w", f.Name, err))
		}
	}
	for _, a := range config.Aspects {
		if a == nil || a.Cache == nil {
			continue
		}
		if err := cacheKeyLooksLikeCEL(a.Cache.Key); err != nil {
			errs = append(errs, fmt.Errorf("aspect %q: %w", a.Name, err))
		}
	}
	return errs
}

// cacheKeyLooksLikeCEL reports whether the text outside a key's `${...}`
// spans is an expression rather than the plain text a template is made of.
func cacheKeyLooksLikeCEL(key string) error {
	if key == "" {
		return nil
	}

	remainder := stripTemplateSpans(key)
	var reason string
	switch {
	case strings.ContainsAny(remainder, `'"`):
		reason = "carries quotes"
	case strings.Contains(remainder, "+"):
		reason = "concatenates with `+`"
	case mentionsMessage(remainder):
		reason = "refers to the message outside a `${...}`"
	default:
		return nil
	}

	return fmt.Errorf("cache key `%s` %s, so it reads as a CEL expression — but a cache key is a template, "+
		"used verbatim except for `${...}`, and this one would be the same key for every request. "+
		"Write it as `%s`", key, reason, suggestTemplate(key))
}

// stripTemplateSpans removes every `${...}` from a key, leaving the text a
// template contributes as it is.
func stripTemplateSpans(key string) string {
	var out strings.Builder
	rest := key
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:start])
		rest = rest[start+end+1:]
	}
}

// mentionsMessage reports whether text refers to what a flow has in scope —
// `input.id` — which in a template is only meaningful inside `${...}`.
func mentionsMessage(text string) bool {
	for _, root := range []string{"input.", "output.", "step.", "ctx.", "enriched."} {
		if strings.Contains(text, root) {
			return true
		}
	}
	return false
}

// suggestTemplate turns the common expression forms into the template they
// meant: `'product:' + input.id` into `product:${input.id}`. Anything it
// cannot read is returned wrapped whole, which at least evaluates.
func suggestTemplate(key string) string {
	var out strings.Builder
	for _, part := range strings.Split(key, "+") {
		part = strings.TrimSpace(part)
		switch {
		case len(part) >= 2 && (part[0] == '\'' || part[0] == '"') && part[len(part)-1] == part[0]:
			out.WriteString(part[1 : len(part)-1])
		case part == "":
		case strings.Contains(part, "${"):
			out.WriteString(part)
		default:
			out.WriteString("${" + part + "}")
		}
	}
	return out.String()
}
