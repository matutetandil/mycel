package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The auth endpoints are the public face of the whole package: whatever the
// manager decides, these translate into a status code and a body, and getting
// that translation wrong is how a rejected login turns into a 500 or, worse, a
// 200. Every handler shares the same shape — method check, decode, validate,
// call the manager, respond — so the table below walks that shape per endpoint.

func newHandler(t *testing.T) (*Handler, *Manager) {
	t.Helper()
	m := newTestManager(t)
	return NewHandler(m), m
}

// call runs one request against a handler and returns the recorder.
func call(h http.HandlerFunc, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

// errorCode pulls error.code out of a response body, or "" if there is none.
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code
}

func TestHandlersRejectTheWrongMethod(t *testing.T) {
	h, _ := newHandler(t)

	// Every endpoint pins its method; the wrong one is 405 rather than a
	// decode error or a panic.
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		wrong   string
	}{
		{"register", h.handleRegister, http.MethodGet},
		{"login", h.handleLogin, http.MethodGet},
		{"logout", h.handleLogout, http.MethodGet},
		{"refresh", h.handleRefresh, http.MethodGet},
		{"me", h.handleMe, http.MethodPost},
		{"change-password", h.handleChangePassword, http.MethodGet},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(tc.handler, tc.wrong, "/auth/"+tc.name, "", nil)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", rec.Code)
			}
			if got := errorCode(t, rec); got != "method_not_allowed" {
				t.Errorf("error code = %q, want method_not_allowed", got)
			}
		})
	}
}

func TestHandleRegister(t *testing.T) {
	h, _ := newHandler(t)

	tests := []struct {
		name      string
		body      string
		wantCode  int
		wantError string
	}{
		{"body that is not JSON", "not json", http.StatusBadRequest, "invalid_request"},
		{"missing email", `{"password":"password123"}`, http.StatusBadRequest, "invalid_request"},
		{"missing password", `{"email":"a@b.com"}`, http.StatusBadRequest, "invalid_request"},
		{"valid registration", `{"email":"reg@example.com","password":"password123"}`, http.StatusCreated, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(h.handleRegister, http.MethodPost, "/auth/register", tc.body, nil)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantError != "" {
				if got := errorCode(t, rec); got != tc.wantError {
					t.Errorf("error code = %q, want %q", got, tc.wantError)
				}
				return
			}
			// A successful registration must hand back usable tokens and must
			// not leak the password hash in the user object.
			var body map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			for _, k := range []string{"user", "access_token", "refresh_token", "token_type"} {
				if body[k] == nil || body[k] == "" {
					t.Errorf("response is missing %q", k)
				}
			}
			user, _ := body["user"].(map[string]interface{})
			for _, leaked := range []string{"password", "password_hash", "PasswordHash"} {
				if _, present := user[leaked]; present {
					t.Errorf("the user object exposes %q", leaked)
				}
			}
		})
	}
}

func TestRegisteringTheSameEmailTwiceIsRejected(t *testing.T) {
	h, _ := newHandler(t)
	body := `{"email":"dup@example.com","password":"password123"}`

	if rec := call(h.handleRegister, http.MethodPost, "/auth/register", body, nil); rec.Code != http.StatusCreated {
		t.Fatalf("first registration failed: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(h.handleRegister, http.MethodPost, "/auth/register", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("second registration status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandleLogin(t *testing.T) {
	h, _ := newHandler(t)
	call(h.handleRegister, http.MethodPost, "/auth/register",
		`{"email":"login@example.com","password":"password123"}`, nil)

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"body that is not JSON", "{", http.StatusBadRequest},
		{"missing password", `{"email":"login@example.com"}`, http.StatusBadRequest},
		{"unknown email", `{"email":"nobody@example.com","password":"password123"}`, http.StatusUnauthorized},
		{"wrong password", `{"email":"login@example.com","password":"wrong-password"}`, http.StatusUnauthorized},
		{"correct credentials", `{"email":"login@example.com","password":"password123"}`, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(h.handleLogin, http.MethodPost, "/auth/login", tc.body, nil)
			if rec.Code != tc.wantCode {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

func TestHandleMe(t *testing.T) {
	h, _ := newHandler(t)
	rec := call(h.handleRegister, http.MethodPost, "/auth/register",
		`{"email":"me@example.com","password":"password123"}`, nil)
	var reg struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		t.Fatalf("register response: %v", err)
	}

	t.Run("without a token", func(t *testing.T) {
		rec := call(h.handleMe, http.MethodGet, "/auth/me", "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("with a garbage token", func(t *testing.T) {
		rec := call(h.handleMe, http.MethodGet, "/auth/me", "",
			map[string]string{"Authorization": "Bearer not-a-jwt"})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("with a valid token", func(t *testing.T) {
		rec := call(h.handleMe, http.MethodGet, "/auth/me", "",
			map[string]string{"Authorization": "Bearer " + reg.AccessToken})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		var user map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
			t.Fatalf("response is not JSON: %v", err)
		}
		if user["email"] != "me@example.com" {
			t.Errorf("email = %v, want me@example.com", user["email"])
		}
	})
}

func TestHandleRefresh(t *testing.T) {
	h, _ := newHandler(t)
	rec := call(h.handleRegister, http.MethodPost, "/auth/register",
		`{"email":"refresh@example.com","password":"password123"}`, nil)
	var reg struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)

	t.Run("missing refresh token", func(t *testing.T) {
		rec := call(h.handleRefresh, http.MethodPost, "/auth/refresh", `{}`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("an access token is not a refresh token", func(t *testing.T) {
		rec := call(h.handleRefresh, http.MethodPost, "/auth/refresh",
			`{"refresh_token":"`+reg.AccessToken+`"}`, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("a valid refresh token yields a new pair", func(t *testing.T) {
		rec := call(h.handleRefresh, http.MethodPost, "/auth/refresh",
			`{"refresh_token":"`+reg.RefreshToken+`"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &body)
		if body["access_token"] == nil || body["access_token"] == "" {
			t.Error("no access token in the refresh response")
		}
	})
}

func TestHandleChangePassword(t *testing.T) {
	h, _ := newHandler(t)
	rec := call(h.handleRegister, http.MethodPost, "/auth/register",
		`{"email":"chpw@example.com","password":"password123"}`, nil)
	var reg struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)
	auth := map[string]string{"Authorization": "Bearer " + reg.AccessToken}

	t.Run("without a token", func(t *testing.T) {
		rec := call(h.handleChangePassword, http.MethodPost, "/auth/change-password",
			`{"current_password":"password123","new_password":"newpassword456"}`, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("with the wrong current password", func(t *testing.T) {
		rec := call(h.handleChangePassword, http.MethodPost, "/auth/change-password",
			`{"current_password":"not-it","new_password":"newpassword456"}`, auth)
		if rec.Code == http.StatusOK {
			t.Error("the password was changed without the correct current one")
		}
	})
}

func TestHandleLogoutWithoutAToken(t *testing.T) {
	h, _ := newHandler(t)
	rec := call(h.handleLogout, http.MethodPost, "/auth/logout", "", nil)
	if rec.Code == http.StatusOK {
		t.Error("logout succeeded without a token")
	}
}

func TestRegisterRoutesWiresTheDefaultPaths(t *testing.T) {
	h, _ := newHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// A registered route answers something other than 404; that is the point
	// of the assertion, not the specific status.
	for _, path := range []string{"/auth/register", "/auth/login", "/auth/me"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not registered", path)
		}
	}
}

func TestDefaultEndpointsConfigAndGetPath(t *testing.T) {
	cfg := DefaultEndpointsConfig()
	if cfg == nil {
		t.Fatal("DefaultEndpointsConfig returned nil")
	}
	// getPath falls back to the default when the config says nothing.
	if got := getPath(nil, "/auth/login"); got != "/auth/login" {
		t.Errorf("getPath(nil) = %q, want the default", got)
	}
	if got := getPath(&EndpointConfig{Path: "/custom"}, "/auth/login"); got != "/custom" {
		t.Errorf("getPath = %q, want the configured path", got)
	}
}

func TestUserToResponseOmitsEmptyOptionalFields(t *testing.T) {
	bare := userToResponse(&User{ID: "1", Email: "a@b.com"})
	for _, k := range []string{"roles", "permissions", "mfa_enabled", "last_login_at", "metadata"} {
		if _, present := bare[k]; present {
			t.Errorf("a bare user carries %q", k)
		}
	}

	full := userToResponse(&User{
		ID: "1", Email: "a@b.com",
		Roles:       []string{"admin"},
		Permissions: []string{"users:read"},
		MFAEnabled:  true,
		MFAMethods:  []string{"totp"},
	})
	for _, k := range []string{"roles", "permissions", "mfa_enabled", "mfa_methods"} {
		if _, present := full[k]; !present {
			t.Errorf("a populated user is missing %q", k)
		}
	}
}

func TestGetClientIP(t *testing.T) {
	// The order matters: a proxy header must win over RemoteAddr, or rate
	// limiting and lockout end up counting every user as the same client.
	tests := []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{"falls back to RemoteAddr", nil, "192.0.2.5:1234", "192.0.2.5"},
		{"X-Real-IP wins over RemoteAddr", map[string]string{"X-Real-IP": "203.0.113.9"}, "192.0.2.5:1234", "203.0.113.9"},
		{"X-Forwarded-For wins over X-Real-IP", map[string]string{"X-Forwarded-For": "198.51.100.7", "X-Real-IP": "203.0.113.9"}, "192.0.2.5:1234", "198.51.100.7"},
		{"the first entry of a forwarded chain is the client", map[string]string{"X-Forwarded-For": "198.51.100.7, 203.0.113.9"}, "192.0.2.5:1234", "198.51.100.7"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := getClientIP(r); got != tc.want {
				t.Errorf("getClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
