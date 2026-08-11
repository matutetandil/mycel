package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The middleware decides whether an authenticated request reaches the handler
// at all, so its failure modes matter more than most: letting a request
// through unauthenticated, or blocking one that should pass.

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset: "development",
		JWT: &JWTConfig{
			Secret:          "test-secret-key-that-is-long-enough",
			AccessLifetime:  "15m",
			RefreshLifetime: "7d",
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// registerAndToken returns a valid access token for a freshly registered user.
func registerAndToken(t *testing.T, m *Manager, email string) string {
	t.Helper()
	_, tokens, err := m.Register(context.Background(), &RegisterRequest{
		Email:    email,
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return tokens.AccessToken
}

// okHandler records whether the request got past the middleware.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddlewareHandler(t *testing.T) {
	m := newTestManager(t)
	token := registerAndToken(t, m, "mw@example.com")

	tests := []struct {
		name       string
		config     *MiddlewareConfig
		path       string
		authHeader string
		wantStatus int
		wantNext   bool
	}{
		{
			name:       "no token and auth required is rejected",
			config:     &MiddlewareConfig{Required: true},
			path:       "/private",
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		{
			name:       "no token and auth optional passes through",
			config:     &MiddlewareConfig{Required: false},
			path:       "/public",
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "excluded path skips authentication entirely",
			config:     &MiddlewareConfig{Required: true, Exclude: []string{"/health"}},
			path:       "/health",
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "excluded wildcard covers the subtree",
			config:     &MiddlewareConfig{Required: true, Exclude: []string{"/public/*"}},
			path:       "/public/logo.png",
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "a garbage token is rejected, not passed through",
			config:     &MiddlewareConfig{Required: true},
			path:       "/private",
			authHeader: "Bearer not-a-jwt",
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		{
			name:       "an invalid token is rejected even when auth is optional",
			config:     &MiddlewareConfig{Required: false},
			path:       "/private",
			authHeader: "Bearer not-a-jwt",
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		{
			name:       "a valid token reaches the handler",
			config:     &MiddlewareConfig{Required: true},
			path:       "/private",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "a valid token without the Bearer prefix is still accepted",
			config:     &MiddlewareConfig{Required: true},
			path:       "/private",
			authHeader: token,
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			h := NewMiddleware(m, tc.config).Handler(okHandler(&reached))

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if reached != tc.wantNext {
				t.Errorf("handler reached = %v, want %v", reached, tc.wantNext)
			}
		})
	}
}

func TestMiddlewarePutsUserAndClaimsInContext(t *testing.T) {
	m := newTestManager(t)
	token := registerAndToken(t, m, "ctx@example.com")

	var gotUser *User
	var gotClaims *Claims
	h := NewMiddleware(m, &MiddlewareConfig{Required: true}).Handler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser = GetUser(r.Context())
			gotClaims = GetClaims(r.Context())
		}))

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotUser == nil {
		t.Fatal("no user in context")
	}
	if gotUser.Email != "ctx@example.com" {
		t.Errorf("user email = %q, want %q", gotUser.Email, "ctx@example.com")
	}
	if gotClaims == nil {
		t.Fatal("no claims in context")
	}
	if gotClaims.UserID != gotUser.ID {
		t.Errorf("claims user id = %q, want %q", gotClaims.UserID, gotUser.ID)
	}
}

func TestGetUserAndClaimsOnAnEmptyContext(t *testing.T) {
	// The handler may run without the middleware in front of it; the getters
	// have to say "nobody" rather than panic.
	if u := GetUser(context.Background()); u != nil {
		t.Errorf("GetUser = %v, want nil", u)
	}
	if c := GetClaims(context.Background()); c != nil {
		t.Errorf("GetClaims = %v, want nil", c)
	}
	// A value of the wrong type under the same key must not be returned either.
	ctx := context.WithValue(context.Background(), UserContextKey, "not a user")
	if u := GetUser(ctx); u != nil {
		t.Errorf("GetUser on a wrongly typed value = %v, want nil", u)
	}
}

func TestMiddlewarePathRules(t *testing.T) {
	m := newTestManager(t)
	token := registerAndToken(t, m, "rules@example.com")

	// A registered user in the development preset has no admin role, so a rule
	// demanding one must produce 403 — authenticated but not authorized.
	h := NewMiddleware(m, &MiddlewareConfig{
		Required: true,
		Rules:    map[string]*PathRule{"/admin/*": {Roles: []string{"admin"}}},
	}).Handler(okHandler(new(bool)))

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/admin/users", http.StatusForbidden},
		{"/dashboard", http.StatusOK}, // no rule matches, so no extra check
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestCheckAuthorization(t *testing.T) {
	mw := &Middleware{}

	tests := []struct {
		name   string
		claims *Claims
		rule   *PathRule
		want   bool
	}{
		{
			name:   "an empty rule allows anyone",
			claims: &Claims{},
			rule:   &PathRule{},
			want:   true,
		},
		{
			name:   "any one of the required roles is enough",
			claims: &Claims{Roles: []string{"editor"}},
			rule:   &PathRule{Roles: []string{"admin", "editor"}},
			want:   true,
		},
		{
			name:   "none of the required roles is a refusal",
			claims: &Claims{Roles: []string{"viewer"}},
			rule:   &PathRule{Roles: []string{"admin", "editor"}},
			want:   false,
		},
		{
			name:   "any one of the required permissions is enough",
			claims: &Claims{Permissions: []string{"users:read"}},
			rule:   &PathRule{Permissions: []string{"users:read", "users:write"}},
			want:   true,
		},
		{
			name:   "roles and permissions are both required when both are set",
			claims: &Claims{Roles: []string{"admin"}},
			rule:   &PathRule{Roles: []string{"admin"}, Permissions: []string{"users:write"}},
			want:   false,
		},
		{
			name:   "having both satisfies both",
			claims: &Claims{Roles: []string{"admin"}, Permissions: []string{"users:write"}},
			rule:   &PathRule{Roles: []string{"admin"}, Permissions: []string{"users:write"}},
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mw.checkAuthorization(nil, tc.claims, tc.rule); got != tc.want {
				t.Errorf("checkAuthorization = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"/health", "/health", true},
		{"/health", "/healthz", false},
		{"/health", "/other", false},

		// `/*` and `/**` both match a subtree, and neither matches the bare prefix.
		{"/public/*", "/public/logo.png", true},
		{"/public/*", "/public/css/app.css", true},
		{"/public/*", "/public", false},
		{"/public/*", "/private/logo.png", false},
		{"/api/**", "/api/v1/users", true},
		{"/api/**", "/api", false},

		// A `*` in the middle stands for exactly one segment.
		{"/users/*/posts", "/users/42/posts", true},
		{"/users/*/posts", "/users/42/comments", false},
		{"/users/*/posts", "/users/42/posts/7", false},

		// No wildcard and no exact match means no match.
		{"/a/b", "/a/b/c", false},
		{"", "/a", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+" vs "+tc.path, func(t *testing.T) {
			if got := matchPath(tc.pattern, tc.path); got != tc.want {
				t.Errorf("matchPath(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestRequireRolesAndPermissions(t *testing.T) {
	m := newTestManager(t)

	// These middlewares read claims the auth middleware already put in the
	// context; with no claims present the answer is 401, not 403, because the
	// request is unauthenticated rather than unauthorized.
	tests := []struct {
		name     string
		mw       func(http.Handler) http.Handler
		claims   *Claims
		want     int
		wantNext bool
	}{
		{"roles: no claims at all", RequireRoles(m, "admin"), nil, http.StatusUnauthorized, false},
		{"roles: wrong role", RequireRoles(m, "admin"), &Claims{Roles: []string{"viewer"}}, http.StatusForbidden, false},
		{"roles: right role", RequireRoles(m, "admin"), &Claims{Roles: []string{"admin"}}, http.StatusOK, true},
		{"roles: one of several", RequireRoles(m, "admin", "editor"), &Claims{Roles: []string{"editor"}}, http.StatusOK, true},
		{"permissions: no claims at all", RequirePermissions(m, "users:write"), nil, http.StatusUnauthorized, false},
		{"permissions: wrong permission", RequirePermissions(m, "users:write"), &Claims{Permissions: []string{"users:read"}}, http.StatusForbidden, false},
		{"permissions: right permission", RequirePermissions(m, "users:write"), &Claims{Permissions: []string{"users:write"}}, http.StatusOK, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			h := tc.mw(okHandler(&reached))

			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.claims != nil {
				req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, tc.claims))
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if reached != tc.wantNext {
				t.Errorf("handler reached = %v, want %v", reached, tc.wantNext)
			}
		})
	}
}

func TestRequireAuthAndOptionalAuth(t *testing.T) {
	m := newTestManager(t)

	reached := false
	RequireAuth(m)(okHandler(&reached)).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if reached {
		t.Error("RequireAuth let an unauthenticated request through")
	}

	reached = false
	OptionalAuth(m)(okHandler(&reached)).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !reached {
		t.Error("OptionalAuth blocked an unauthenticated request")
	}
}

func TestNewMiddlewareDefaultsToRequiringAuth(t *testing.T) {
	// A nil config must not mean "let everything through".
	mw := NewMiddleware(newTestManager(t), nil)
	if !mw.config.Required {
		t.Error("a nil config produced optional authentication")
	}
}

func TestErrorResponsesAreWellFormedJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(http.ResponseWriter, string)
		want  int
		code  string
	}{
		{"unauthorized", writeUnauthorized, http.StatusUnauthorized, "unauthorized"},
		{"forbidden", writeForbidden, http.StatusForbidden, "forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.write(rec, "some message")

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not valid JSON: %v (%s)", err, rec.Body.String())
			}
			if body.Error.Code != tc.code {
				t.Errorf("error code = %q, want %q", body.Error.Code, tc.code)
			}
			if !strings.Contains(body.Error.Message, "some message") {
				t.Errorf("message = %q, want it to carry the text passed in", body.Error.Message)
			}
		})
	}
}

func TestExtractTokenFromHeader(t *testing.T) {
	tests := []struct{ header, want string }{
		{"", ""},
		{"Bearer abc", "abc"},
		{"abc", "abc"}, // a bare token is accepted
		// The prefix check is a strict `>`, so a header that is exactly
		// "Bearer " falls through and comes back whole. It then fails
		// validation, which is the safe direction: a malformed header is
		// rejected rather than treated as an anonymous request.
		{"Bearer ", "Bearer "},
		{"bearer abc", "bearer abc"}, // the check is case-sensitive
	}
	for _, tc := range tests {
		if got := ExtractTokenFromHeader(tc.header); got != tc.want {
			t.Errorf("ExtractTokenFromHeader(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}
