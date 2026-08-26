package runtime

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
)

// registerEnvHandler serves what the process was started with, at /debug/env.
//
// The question it answers is the one somebody asks of a running pod: did the
// configmap change land, is MYCEL_PAYLOAD_SHOW actually on in this replica.
// Until now that meant a shell inside the container — which is a whole
// package manager, a busybox and an openssh in the image for the sake of
// reading a few names, and which does not exist at all on a distroless one.
//
// Values are redacted unless the name says plainly that it holds no secret.
// The opposite rule — redact the ones that look secret — is the one that
// leaks: it fails open for every name nobody thought of, and an environment
// is where the database password lives. So a value is shown when its name
// starts with MYCEL_ and does not look like a credential, and every other
// variable is reported as present, with its length, and nothing else.
func registerEnvHandler(mux *http.ServeMux) {
	mux.HandleFunc("GET /debug/env", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")

		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(environmentReport())
	})
}

// EnvVariable is one variable as this reports it.
type EnvVariable struct {
	Name string `json:"name"`
	// Value is the value itself, for the ones that carry no secret.
	Value string `json:"value,omitempty"`
	// Redacted says the variable is set and its value is not shown.
	Redacted bool `json:"redacted,omitempty"`
	// Length is how long the value is, which is enough to tell "set to the
	// wrong thing" from "set to nothing" without showing it.
	Length int `json:"length"`
}

func environmentReport() []EnvVariable {
	entries := os.Environ()
	report := make([]EnvVariable, 0, len(entries))

	for _, entry := range entries {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		variable := EnvVariable{Name: name, Length: len(value)}
		if valueIsShowable(name) {
			variable.Value = value
		} else {
			variable.Redacted = true
		}
		report = append(report, variable)
	}

	sort.Slice(report, func(i, j int) bool { return report[i].Name < report[j].Name })
	return report
}

// credentialWords name the things an environment is used to carry that must
// not be read back out, whatever else the name says.
var credentialWords = []string{
	"SECRET", "PASSWORD", "PASSWD", "TOKEN", "KEY", "CREDENTIAL",
	"AUTH", "DSN", "URL", "URI", "CONN", "SALT", "PRIVATE", "SIGNATURE",
}

// valueIsShowable reports whether a variable's value may be printed.
//
// Only Mycel's own settings qualify, and only the ones whose names do not
// suggest a credential — MYCEL_JWT_SECRET is Mycel's and is still a secret,
// and a DSN carries a password inside it.
func valueIsShowable(name string) bool {
	if !strings.HasPrefix(name, "MYCEL_") {
		return false
	}
	upper := strings.ToUpper(name)
	for _, word := range credentialWords {
		if strings.Contains(upper, word) {
			return false
		}
	}
	return true
}
