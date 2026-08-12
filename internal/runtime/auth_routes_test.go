package runtime

import (
	"net/http"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/auth"
)

// Nothing called the auth handler's RegisterRoutes, so every endpoint the auth
// block defines answered 404 on a running service while the log said the system
// was initialised.

func TestTheAuthEndpointsAreMounted(t *testing.T) {
	manager, err := auth.NewManager(&auth.Config{
		Preset: "development",
		JWT:    &auth.JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	mounted := map[string]bool{}
	auth.NewHandler(manager).RegisterRoutes(recordingMux(func(pattern string) {
		mounted[pattern] = true
	}))

	// The endpoints someone expects from writing an auth block at all.
	for _, path := range []string{"/auth/login", "/auth/register", "/auth/refresh", "/auth/me"} {
		if !mounted[path] {
			t.Errorf("%s was not mounted, so it would answer 404", path)
		}
	}
}

// recordingMux notes the patterns without needing a server.
type recordingMux func(pattern string)

func (m recordingMux) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	m(pattern)
}
