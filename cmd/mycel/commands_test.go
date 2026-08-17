package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The commands somebody runs before anything is working: version, check, and
// the exports. None of them were covered.

const checkableService = `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9397
}

connector "api" {
  type = "rest"
  port = 3397
}

connector "store" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }
  to {
    connector = "store"
    target    = "orders"
  }
}
`

func projectWith(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()

	previous := configDir
	configDir = dir
	t.Cleanup(func() { configDir = previous })

	if err := os.WriteFile(filepath.Join(dir, "service.mycel"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	return dir
}

func TestVersionSaysWhichBuildThisIs(t *testing.T) {
	// The first thing anybody is asked for in a bug report, and it was
	// documented in four places before the command existed at all.
	if err := runVersion(versionCmd, nil); err != nil {
		t.Errorf("version: %v", err)
	}
}

func TestValidateAcceptsAServiceThatIsWellFormed(t *testing.T) {
	projectWith(t, checkableService)

	if err := runValidate(validateCmd, nil); err != nil {
		t.Errorf("a well-formed service was rejected: %v", err)
	}
}

func TestValidateRefusesWhatTheRuntimeWouldNotStart(t *testing.T) {
	// The whole point of the command: the answer here has to match what
	// starting would do, or it is worse than not having it.
	projectWith(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

connector "api" {
  type = "rest"
  port = 3396
}

flow "list_orders" {
  from {
    connector = "api"
  }
}
`)

	err := runValidate(validateCmd, nil)
	if err == nil {
		t.Fatal("a flow missing the operation its connector requires was accepted")
	}
	if !strings.Contains(err.Error(), "flow") {
		t.Errorf("error = %q, want it to say where the problem is", err)
	}
}

func TestCheckReportsWhatItCouldReach(t *testing.T) {
	// check used to report success without opening a socket. It connects now,
	// so a service whose database is unreachable says so here rather than on
	// the first request.
	projectWith(t, checkableService)

	// The connectors above are reachable — a REST server and an in-memory
	// database — so this reports success. What matters is that it ran the
	// connections rather than reading the configuration.
	if err := runCheck(checkCmd, nil); err != nil {
		t.Errorf("check: %v", err)
	}
}

func TestCheckOnSomethingThatIsNotAProjectIsReported(t *testing.T) {
	previous := configDir
	configDir = filepath.Join(t.TempDir(), "not-a-project")
	t.Cleanup(func() { configDir = previous })

	if err := runCheck(checkCmd, nil); err == nil {
		t.Error("a directory with no configuration in it was checked successfully")
	}
}

func TestAConnectorIsDescribedByWhatItIs(t *testing.T) {
	// What the banner and the reports print. A connector described as nothing
	// reads as one that failed to load.
	for name, tc := range map[string]struct {
		connType, driver, want string
	}{
		"a type and a driver":   {"database", "postgres", "database/postgres"},
		"a type on its own":     {"rest", "", "rest"},
		"neither":               {"", "", ""},
		"a driver with no type": {"", "postgres", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := describeConnectorKind(tc.connType, tc.driver); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheEnvironmentIsReadFromADotEnvFile(t *testing.T) {
	// env() in the configuration resolves against the process environment, and
	// a .env beside the configuration is how a service is run locally.
	dir := projectWith(t, checkableService)

	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("MYCEL_TEST_FROM_DOTENV=it-was-read\n"), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}
	// Not set at all: godotenv fills in what the environment does not say,
	// and a variable set to the empty string counts as said.
	t.Cleanup(func() { _ = os.Unsetenv("MYCEL_TEST_FROM_DOTENV") })

	loadDotEnv()

	if os.Getenv("MYCEL_TEST_FROM_DOTENV") != "it-was-read" {
		t.Errorf("the .env beside the configuration was not read")
	}
}

func TestAVariableAlreadySetIsNotOverwritten(t *testing.T) {
	// A .env is for filling in what the environment does not already say —
	// overriding it would mean a deployment's own settings lose to a file
	// somebody left in the directory.
	dir := projectWith(t, checkableService)

	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("MYCEL_TEST_ALREADY_SET=from-the-file\n"), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}
	t.Setenv("MYCEL_TEST_ALREADY_SET", "from-the-environment")

	loadDotEnv()

	if os.Getenv("MYCEL_TEST_ALREADY_SET") != "from-the-environment" {
		t.Errorf("a variable the environment set was overwritten by the file")
	}
}
