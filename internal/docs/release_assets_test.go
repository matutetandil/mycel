package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A download the documentation offers has to be one that exists.
//
// The installation page told a reader to fetch
// `releases/latest/download/mycel_2.15.0_linux_amd64.deb`. That path needs the
// exact file name of the latest release, and the name carries the version — so
// from the moment 2.16.0 was tagged, the documented way to install the package
// answered 404. Nothing noticed, because nobody follows an install page twice.
//
// The fix was to stop naming a version. This keeps it that way: an asset name
// in the documentation must be built from a variable, not typed.
func TestNoPageNamesAReleaseAssetByVersion(t *testing.T) {
	pinned := regexp.MustCompile(`mycel[_-]\d+\.\d+\.\d+[_-][a-z0-9]+`)

	var offenders []string
	for _, root := range []string{"../../docs", "../../README.md"} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			// The changelog is a record of what was released, so naming
			// versions is the whole point of it.
			if strings.Contains(path, "/archive/") || strings.HasSuffix(path, "CHANGELOG.md") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			for _, line := range strings.Split(string(body), "\n") {
				if !pinned.MatchString(line) {
					continue
				}
				// A helm chart archive is named the same way and is fetched
				// by tag on purpose; only the release-download path is the
				// one that goes stale.
				if !strings.Contains(line, "releases/latest/download") &&
					!strings.Contains(line, "dpkg -i") &&
					!strings.Contains(line, "rpm -i") &&
					!strings.Contains(line, "apk add") {
					continue
				}
				offenders = append(offenders, path+": "+strings.TrimSpace(line))
			}
			return nil
		})
	}

	for _, o := range offenders {
		t.Errorf("a release asset is named with a version, so it 404s from the next release on:\n  %s", o)
	}
}
