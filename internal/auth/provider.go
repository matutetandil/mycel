package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
)

// compiledProvider pairs a ProviderConfig with its pre-compiled CEL programs so
// response mapping is validated once at startup, not per request.
type compiledProvider struct {
	cfg     *ProviderConfig
	success cel.Program
	userID  cel.Program
	email   cel.Program
	roles   cel.Program
	token   cel.Program
}

// ProviderValidator validates incoming credentials against external HTTP
// identity providers declared via `auth { provider "name" { ... } }`.
//
// For each request whose token is not a valid local JWT, the configured
// providers are tried in order: the provider's `validate` URL is called with
// the token (templated into the URL and/or request headers via `{token}`), and
// the JSON response is mapped to a User/Claims through CEL expressions. The
// first provider whose `success` expression is truthy wins.
type ProviderValidator struct {
	providers []*compiledProvider
	client    *http.Client
	logger    *slog.Logger
}

// providerCELEnv builds the CEL environment used for response mapping. Two
// variables are available to every expression: `status` (the HTTP status code)
// and `body` (the parsed JSON response object).
func providerCELEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("status", cel.IntType),
		cel.Variable("body", cel.MapType(cel.StringType, cel.DynType)),
	)
}

func compileProviderExpr(env *cel.Env, expr string) (cel.Program, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, nil
	}
	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}
	return env.Program(ast)
}

// NewProviderValidator compiles the provider configs into a validator. It fails
// fast on an unsupported provider type, a missing `validate`/`success`, or an
// invalid CEL expression, so a misconfigured provider is caught at startup
// instead of silently accepting (or rejecting) every request.
func NewProviderValidator(providers []*ProviderConfig, client *http.Client, logger *slog.Logger) (*ProviderValidator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	env, err := providerCELEnv()
	if err != nil {
		return nil, fmt.Errorf("auth provider: building CEL env: %w", err)
	}

	compiled := make([]*compiledProvider, 0, len(providers))
	for _, p := range providers {
		if p == nil {
			continue
		}
		if p.Type != "" && p.Type != "http" {
			return nil, fmt.Errorf("auth provider %q: unsupported type %q (only \"http\" is supported)", p.Name, p.Type)
		}
		if strings.TrimSpace(p.Validate) == "" {
			return nil, fmt.Errorf("auth provider %q: a `validate` URL is required", p.Name)
		}
		if p.Response == nil || strings.TrimSpace(p.Response.Success) == "" {
			return nil, fmt.Errorf("auth provider %q: a `response { success = ... }` expression is required", p.Name)
		}
		// sync_to is parsed but not yet executed — surface it loudly rather
		// than silently ignoring it.
		if p.SyncTo != "" {
			logger.Warn("auth provider: sync_to is parsed but not executed yet (planned feature)",
				"provider", p.Name, "sync_to", p.SyncTo)
		}

		cp := &compiledProvider{cfg: p}
		for _, field := range []struct {
			name string
			expr string
			dst  *cel.Program
		}{
			{"success", p.Response.Success, &cp.success},
			{"user_id", p.Response.UserID, &cp.userID},
			{"email", p.Response.Email, &cp.email},
			{"roles", p.Response.Roles, &cp.roles},
			{"token", p.Response.Token, &cp.token},
		} {
			prg, err := compileProviderExpr(env, field.expr)
			if err != nil {
				return nil, fmt.Errorf("auth provider %q: invalid `%s` expression: %w", p.Name, field.name, err)
			}
			*field.dst = prg
		}
		compiled = append(compiled, cp)
	}

	return &ProviderValidator{providers: compiled, client: client, logger: logger}, nil
}

// HasProviders reports whether any provider is configured. Safe on a nil receiver.
func (v *ProviderValidator) HasProviders() bool {
	return v != nil && len(v.providers) > 0
}

// Validate tries each configured provider in order. The first whose `success`
// expression is truthy yields the User/Claims. Returns ErrInvalidToken if no
// provider accepts the credential (including when a provider is unreachable).
func (v *ProviderValidator) Validate(ctx context.Context, token string) (*User, *Claims, error) {
	if !v.HasProviders() || token == "" {
		return nil, nil, ErrInvalidToken
	}
	for _, p := range v.providers {
		if user, claims, ok := v.validateOne(ctx, p, token); ok {
			return user, claims, nil
		}
	}
	return nil, nil, ErrInvalidToken
}

func (v *ProviderValidator) validateOne(ctx context.Context, p *compiledProvider, token string) (*User, *Claims, bool) {
	url := strings.ReplaceAll(p.cfg.Validate, "{token}", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		v.logger.Warn("auth provider: building request failed", "provider", p.cfg.Name, "error", err)
		return nil, nil, false
	}
	for k, val := range p.cfg.Request {
		req.Header.Set(k, strings.ReplaceAll(val, "{token}", token))
	}

	resp, err := v.client.Do(req)
	if err != nil {
		// Provider unreachable / timed out → treat as a validation failure for
		// this provider, not a 5xx for the caller.
		v.logger.Warn("auth provider: request failed", "provider", p.cfg.Name, "error", err)
		return nil, nil, false
	}
	defer resp.Body.Close()

	body := map[string]interface{}{}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap at 1MB
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			v.logger.Warn("auth provider: response body is not a JSON object", "provider", p.cfg.Name, "error", err)
			body = map[string]interface{}{}
		}
	}

	vars := map[string]interface{}{
		"status": int64(resp.StatusCode),
		"body":   body,
	}

	if !v.evalBool(p.success, vars) {
		return nil, nil, false
	}

	userID := v.evalString(p.userID, vars)
	email := v.evalString(p.email, vars)
	roles := v.evalRoles(p.roles, vars)
	providerToken := v.evalString(p.token, vars)

	claims := &Claims{
		UserID:    userID,
		Email:     email,
		Roles:     roles,
		TokenType: "access",
		Custom:    body, // full provider response available as auth.claims.*
	}
	claims.Subject = userID
	if providerToken != "" {
		claims.SessionID = providerToken
	}

	user := &User{
		ID:       userID,
		Email:    email,
		Roles:    roles,
		Metadata: map[string]interface{}{"auth_provider": p.cfg.Name},
	}
	return user, claims, true
}

func (v *ProviderValidator) evalBool(prg cel.Program, vars map[string]interface{}) bool {
	if prg == nil {
		return false
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		return false
	}
	b, ok := out.Value().(bool)
	return ok && b
}

func (v *ProviderValidator) evalString(prg cel.Program, vars map[string]interface{}) string {
	if prg == nil {
		return ""
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		return ""
	}
	switch val := out.Value().(type) {
	case nil:
		return ""
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

func (v *ProviderValidator) evalRoles(prg cel.Program, vars map[string]interface{}) []string {
	if prg == nil {
		return nil
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		return nil
	}
	// Preferred shape: a list of strings.
	if native, err := out.ConvertToNative(reflect.TypeOf([]string{})); err == nil {
		if rs, ok := native.([]string); ok {
			return rs
		}
	}
	// Fallback: a comma-separated string.
	if s, ok := out.Value().(string); ok {
		parts := strings.Split(s, ",")
		roles := make([]string, 0, len(parts))
		for _, part := range parts {
			if t := strings.TrimSpace(part); t != "" {
				roles = append(roles, t)
			}
		}
		return roles
	}
	return nil
}
