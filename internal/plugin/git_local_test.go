package plugin

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Resolving a plugin from git, against a real repository rather than the
// network. What version a service ends up running is decided here, and getting
// it wrong means a deployment quietly running something other than what the
// configuration pins.

// repository builds a git repository with the given tags on it.
func repository(t *testing.T, tags ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "plugin.mycel"), []byte(`
plugin {
  name    = "remote-plugin"
  version = "1.0.0"
}
`), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "the plugin")
	for _, tag := range tags {
		run("tag", tag)
	}
	return dir
}

func resolver() *GitResolver {
	return &GitResolver{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestTheVersionsAPluginHasPublishedAreRead(t *testing.T) {
	repo := repository(t, "v1.0.0", "v1.2.0", "v2.0.0-rc.1", "not-a-version")

	versions, err := resolver().listTagsFromURL(context.Background(), repo)
	if err != nil {
		t.Fatalf("listTagsFromURL: %v", err)
	}

	published := map[string]bool{}
	for _, v := range versions {
		published[v.String()] = true
	}

	for _, want := range []string{"v1.0.0", "v1.2.0"} {
		if !published[want] {
			t.Errorf("%s is not among %v", want, published)
		}
	}
	// A tag that is not a version is not one: reading it as 0.0.0 would make
	// it the answer to any constraint that allows an old version.
	if published["not-a-version"] || published["v0.0.0"] {
		t.Error("a tag that is not a version was read as one")
	}

	// A release candidate keeps its name. It used to be read as v2.0.0, which
	// made it indistinguishable from the release it is a candidate for.
	if !published["v2.0.0-rc.1"] {
		t.Errorf("the pre-release was flattened into a release: %v", published)
	}
}

func TestAReleaseCandidateIsNotShippedToEverybody(t *testing.T) {
	// A plugin author pushing v2.0.0-rc.1 used to ship it to every service
	// whose constraint allowed 2.0.0 — and once the real v2.0.0 existed the
	// two compared equal, so which one ran came down to the order the tags
	// arrived in.
	versions := []Version{}
	for _, tag := range []string{"v1.9.0", "v2.0.0-rc.1", "v2.0.0-rc.2"} {
		v, err := ParseVersion(tag)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tag, err)
		}
		versions = append(versions, v)
	}

	constraint, err := ParseConstraint(">= 1.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint: %v", err)
	}

	best, ok := BestMatch(versions, constraint)
	if !ok {
		t.Fatal("no version matched a constraint two candidates and a release satisfy")
	}
	if best.IsPreRelease() {
		t.Errorf("resolved to %s, want the released version", best)
	}
	if best.String() != "v1.9.0" {
		t.Errorf("resolved to %s", best)
	}
}

func TestAReleaseCandidateCanBeAskedForByName(t *testing.T) {
	// Which is the only way to run one, and the reason it is still listed.
	v, err := ParseVersion("v2.0.0-rc.1")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}

	constraint, err := ParseConstraint("v2.0.0-rc.1")
	if err != nil {
		t.Fatalf("ParseConstraint: %v", err)
	}

	best, ok := BestMatch([]Version{v}, constraint)
	if !ok {
		t.Fatal("a pre-release asked for by name was not resolved")
	}
	if best.String() != "v2.0.0-rc.1" {
		t.Errorf("resolved to %s", best)
	}
}

func TestAPluginIsClonedAtTheVersionAsked(t *testing.T) {
	repo := repository(t, "v1.0.0")
	dest := filepath.Join(t.TempDir(), "cloned")

	if err := resolver().cloneFromURL(context.Background(), repo, "v1.0.0", dest, repo); err != nil {
		t.Fatalf("cloneFromURL: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dest, "plugin.mycel"))
	if err != nil {
		t.Fatalf("the clone has no manifest in it: %v", err)
	}
	if !strings.Contains(string(body), "remote-plugin") {
		t.Errorf("manifest = %s", body)
	}
}

func TestAVersionNobodyPublishedIsReported(t *testing.T) {
	// Rather than a clone of whatever the default branch happens to be, which
	// is how a service ends up running something nobody pinned.
	repo := repository(t, "v1.0.0")
	dest := filepath.Join(t.TempDir(), "cloned")

	if err := resolver().cloneFromURL(context.Background(), repo, "v9.9.9", dest, repo); err == nil {
		t.Error("a version that does not exist was cloned")
	}
}

func TestARepositoryThatIsNotThereIsReported(t *testing.T) {
	if _, err := resolver().listTagsFromURL(context.Background(), filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Error("versions were read from a repository that does not exist")
	}
}

func TestWhereAPluginSourceResolvesTo(t *testing.T) {
	// A short source is the ordinary form, and which protocol it becomes
	// decides whether a developer's SSH key or a CI runner's token is what
	// fetches it — so both are tried, and this is the order.
	for name, tc := range map[string]struct {
		source, ssh, https string
	}{
		"a known host": {
			"github.com/acme/mycel-salesforce",
			"git@github.com:acme/mycel-salesforce.git",
			"https://github.com/acme/mycel-salesforce.git",
		},
		"a host nobody knows": {
			"git.acme.internal/plugins/store",
			"https://git.acme.internal/plugins/store.git",
			"https://git.acme.internal/plugins/store.git",
		},
		"an address already written out": {
			"https://github.com/acme/plugin.git",
			"https://github.com/acme/plugin.git",
			"https://github.com/acme/plugin.git",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := NormalizeGitURL(tc.source); got != tc.ssh {
				t.Errorf("ssh = %q, want %q", got, tc.ssh)
			}
			if got := NormalizeGitURLHTTPS(tc.source); got != tc.https {
				t.Errorf("https = %q, want %q", got, tc.https)
			}
		})
	}
}
