package hcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// The functions a configuration file can call.
//
// These are what stands between a configuration and the things it must not
// contain: a password, a private key, a host that differs per environment.
// Nothing here had a test, and each one is evaluated below the way a real file
// evaluates it — through the HCL parser, with the same table the parser and the
// plugin loader install — rather than by calling the implementation directly.
// Wired in wrongly, a function is not "wrong", it is `undefined function`, and
// a service that will not start.

func evaluate(t *testing.T, expression string) (string, error) {
	t.Helper()

	expr, diags := hclsyntax.ParseExpression([]byte(expression), "test.mycel", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse %s: %v", expression, diags)
	}

	value, diags := expr.Value(&hcl.EvalContext{Functions: Functions()})
	if diags.HasErrors() {
		return "", diags
	}
	if value.Type() != cty.String {
		t.Fatalf("%s returned %s, want a string", expression, value.Type().FriendlyName())
	}
	return value.AsString(), nil
}

func TestEveryFunctionAConfigurationCanCall(t *testing.T) {
	// The names are the contract: a file calls them by name, so one renamed or
	// left out of the table is a service that will not start.
	want := []string{"env", "coalesce", "file", "base64decode", "base64encode", "abspath"}
	available := Functions()
	for _, name := range want {
		if _, ok := available[name]; !ok {
			t.Errorf("%s() cannot be called from a configuration", name)
		}
	}
	if len(available) != len(want) {
		t.Errorf("the table has %d functions, want %d — a new one needs a test here", len(available), len(want))
	}
}

func TestReadingTheEnvironment(t *testing.T) {
	t.Setenv("MYCEL_TEST_DB_HOST", "db.internal")
	t.Setenv("MYCEL_TEST_EMPTY", "")

	for name, tc := range map[string]struct {
		expression string
		want       string
	}{
		"a variable that is set":                  {`env("MYCEL_TEST_DB_HOST")`, "db.internal"},
		"one that is set, with a default":         {`env("MYCEL_TEST_DB_HOST", "localhost")`, "db.internal"},
		"one that is not set, with a default":     {`env("MYCEL_TEST_MISSING", "localhost")`, "localhost"},
		"one set to nothing falls to the default": {`env("MYCEL_TEST_EMPTY", "localhost")`, "localhost"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := evaluate(t, tc.expression)
			if err != nil {
				t.Fatalf("%s: %v", tc.expression, err)
			}
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.expression, got, tc.want)
			}
		})
	}
}

func TestAVariableThatIsNotSetAndHasNoDefault(t *testing.T) {
	// It reads as empty rather than failing, which is why a connector whose
	// host is unset reports a missing property instead of a missing variable —
	// and why the parser looks for these calls separately to name the variable
	// when a connector fails to register.
	got, err := evaluate(t, `env("MYCEL_TEST_DEFINITELY_NOT_SET")`)
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if got != "" {
		t.Errorf("env of an unset variable = %q, want empty", got)
	}
}

func TestTheFirstThingThatIsThere(t *testing.T) {
	t.Setenv("MYCEL_TEST_PRIMARY", "")
	t.Setenv("MYCEL_TEST_FALLBACK", "fallback.internal")

	got, err := evaluate(t, `coalesce(env("MYCEL_TEST_PRIMARY"), env("MYCEL_TEST_FALLBACK"), "localhost")`)
	if err != nil {
		t.Fatalf("coalesce: %v", err)
	}
	if got != "fallback.internal" {
		t.Errorf("coalesce = %q", got)
	}

	// Nothing anywhere is empty, not an error: the attribute is then missing,
	// which is what the connector reports.
	got, err = evaluate(t, `coalesce("", "")`)
	if err != nil {
		t.Fatalf("coalesce: %v", err)
	}
	if got != "" {
		t.Errorf("coalesce of nothing = %q", got)
	}
}

func TestReadingASecretFromAFile(t *testing.T) {
	// The point of file() is that a private key or a password is not in the
	// configuration, and so not in the repository.
	dir := t.TempDir()
	secret := filepath.Join(dir, "db-password.txt")
	if err := os.WriteFile(secret, []byte("s3cret"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := evaluate(t, `file("`+secret+`")`)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("file = %q", got)
	}

	// A file that is not there has to fail loudly. Read as empty, the service
	// would start with no password and fail later against the database, which
	// is a much longer way round to the same news.
	if _, err := evaluate(t, `file("`+filepath.Join(dir, "absent.txt")+`")`); err == nil {
		t.Error("a file that does not exist was read as empty")
	}
}

func TestAPathIsResolvedAgainstTheWorkingDirectory(t *testing.T) {
	// Worth being explicit about: relative paths are resolved against the
	// process's working directory, not the directory the configuration is in.
	// A service started from elsewhere resolves them elsewhere.
	got, err := evaluate(t, `abspath("./data/db.sqlite")`)
	if err != nil {
		t.Fatalf("abspath: %v", err)
	}
	cwd, _ := os.Getwd()
	if got != filepath.Join(cwd, "data/db.sqlite") {
		t.Errorf("abspath = %q, want it under %q", got, cwd)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("abspath returned a relative path: %q", got)
	}
}

func TestTextThatTravelsAsBase64(t *testing.T) {
	// Certificates and keys arrive base64-encoded in a lot of secret stores,
	// and go back out the same way.
	encoded, err := evaluate(t, `base64encode("Hello World")`)
	if err != nil {
		t.Fatalf("base64encode: %v", err)
	}
	if encoded != "SGVsbG8gV29ybGQ=" {
		t.Errorf("base64encode = %q", encoded)
	}

	decoded, err := evaluate(t, `base64decode("SGVsbG8gV29ybGQ=")`)
	if err != nil {
		t.Fatalf("base64decode: %v", err)
	}
	if decoded != "Hello World" {
		t.Errorf("base64decode = %q", decoded)
	}

	// Round trip, which is what a value that passes through a store does.
	back, err := evaluate(t, `base64decode(base64encode("-----BEGIN PRIVATE KEY-----"))`)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if back != "-----BEGIN PRIVATE KEY-----" {
		t.Errorf("round trip = %q", back)
	}

	// Something that is not base64 at all is an error rather than an empty
	// credential: a truncated secret is the failure that looks like anything
	// except a truncated secret.
	if _, err := evaluate(t, `base64decode("not base64!")`); err == nil {
		t.Error("text that is not base64 was decoded to nothing")
	}
}

func TestFunctionsCompose(t *testing.T) {
	// How they are actually written in a configuration.
	t.Setenv("MYCEL_TEST_KEY_B64", "cGFzc3dvcmQ=")

	got, err := evaluate(t, `base64decode(env("MYCEL_TEST_KEY_B64"))`)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got != "password" {
		t.Errorf("decoded environment value = %q", got)
	}
}

func TestCallingSomethingThatIsNotThere(t *testing.T) {
	// The failure a configuration gets when it calls a function this table
	// does not have — a Terraform habit, most often.
	_, err := evaluate(t, `jsonencode("x")`)
	if err == nil {
		t.Fatal("a function that does not exist was called")
	}
	if !strings.Contains(err.Error(), "jsonencode") {
		t.Errorf("the error does not name the function: %v", err)
	}
}
