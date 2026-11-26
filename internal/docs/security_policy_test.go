package docs

import (
	"os"
	"regexp"
	"testing"
)

// The supported-versions table in SECURITY.md has to name the current minor.
//
// The policy is "the latest minor of the current major", and the table is the
// only place a reader sees which one that is. It said 3.7.x while 3.10.1 was
// the release: three minors after the promise moved, the page still told
// people on 3.7 that they were covered and people on 3.10 that they were not.
// The version a release reports is the constant in cmd/mycel/main.go, which
// every bump already edits, so the table is checked against it.
func TestSecurityPolicyNamesTheCurrentMinor(t *testing.T) {
	src, err := os.ReadFile("../../cmd/mycel/main.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`version\s*=\s*"(\d+)\.(\d+)\.\d+"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("cmd/mycel/main.go: no version constant found")
	}
	minor := string(m[1]) + "." + string(m[2])

	policy, err := os.ReadFile("../../SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		`\|\s*` + regexp.QuoteMeta(minor) + `\.x\s*\|\s*Yes\s*\|`,
		`\|\s*<\s*` + regexp.QuoteMeta(minor) + `\s*\|\s*No\s*\|`,
	} {
		if !regexp.MustCompile(row).Match(policy) {
			t.Errorf("SECURITY.md: the supported-versions table should support %s.x and nothing before it (release is %s.*)", minor, minor)
			return
		}
	}
}
