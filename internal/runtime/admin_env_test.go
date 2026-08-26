package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What a running pod was started with, without a shell inside it.
//
// The question is the operational one — did the configmap change land, is
// MYCEL_PAYLOAD_SHOW on in this replica — and answering it used to mean
// `kubectl exec` into the container, which is a package manager, a busybox
// and an openssh in the image for the sake of reading a few names.
func TestTheEnvironmentIsReportedWithoutItsSecrets(t *testing.T) {
	// The settings somebody actually goes looking for.
	t.Setenv("MYCEL_PAYLOAD_SHOW", "true")
	t.Setenv("MYCEL_ENV", "production")
	// Mycel's own, and still a secret.
	t.Setenv("MYCEL_JWT_SECRET", "super-secret-value")
	// Not Mycel's, and carrying a password inside it.
	t.Setenv("DATABASE_URL", "postgres://user:hunter2@host/db")

	mux := http.NewServeMux()
	registerEnvHandler(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/debug/env", nil))

	if recorder.Code != 200 {
		t.Fatalf("status = %d", recorder.Code)
	}

	var report []EnvVariable
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("not JSON: %v: %s", err, recorder.Body.String())
	}

	byName := map[string]EnvVariable{}
	for _, variable := range report {
		byName[variable.Name] = variable
	}

	// Shown, because that is the point.
	for name, want := range map[string]string{
		"MYCEL_PAYLOAD_SHOW": "true",
		"MYCEL_ENV":          "production",
	} {
		if byName[name].Value != want {
			t.Errorf("%s = %q, want %q", name, byName[name].Value, want)
		}
	}

	// Not shown, and the body must not carry them anywhere.
	for _, name := range []string{"MYCEL_JWT_SECRET", "DATABASE_URL"} {
		variable, present := byName[name]
		if !present {
			t.Errorf("%s is set and was not reported at all; knowing it is there is half the answer", name)
			continue
		}
		if !variable.Redacted || variable.Value != "" {
			t.Errorf("%s was printed: %+v", name, variable)
		}
		if variable.Length == 0 {
			t.Errorf("%s reports no length, so 'set to the wrong thing' cannot be told from 'set to nothing'", name)
		}
	}

	for _, secret := range []string{"super-secret-value", "hunter2"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Errorf("the response carries %q", secret)
		}
	}
}

// The rule is an allow-list on purpose: redacting what looks secret fails open
// for every name nobody thought of, and an environment is where the database
// password lives.
func TestOnlyMycelsOwnNonSecretSettingsAreShown(t *testing.T) {
	for name, showable := range map[string]bool{
		"MYCEL_PAYLOAD_SHOW":    true,
		"MYCEL_LOG_LEVEL":       true,
		"MYCEL_TRACING":         true,
		"MYCEL_JWT_SECRET":      false,
		"MYCEL_API_KEY":         false,
		"MYCEL_DB_PASSWORD":     false,
		"MYCEL_POSTGRES_DSN":    false,
		"MYCEL_WEBHOOK_URL":     false,
		"MYCEL_AUTH_TOKEN":      false,
		"PATH":                  false,
		"HOME":                  false,
		"AWS_SECRET_ACCESS_KEY": false,
		"SOMETHING_HARMLESS":    false,
	} {
		if got := valueIsShowable(name); got != showable {
			t.Errorf("valueIsShowable(%q) = %v, want %v", name, got, showable)
		}
	}
}
