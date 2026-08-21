package file

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A file mode is octal — that is how chmod takes it and how the documentation
// writes it. It was read as a decimal number and handed to the operating system
// as one, so `permissions = "0644"` created files as mode 0o1204: --w----r--,
// which nothing can read back. The files example set exactly that, so an upload
// succeeded and the download that followed was refused by the filesystem.

func TestTheModeIsReadAsOctal(t *testing.T) {
	cases := []struct {
		written interface{}
		want    uint32
	}{
		{"0644", 0o644},
		{"644", 0o644},
		{644, 0o644},
		{float64(600), 0o600},
		{"0755", 0o755},
		{"0600", 0o600},
	}

	for _, tc := range cases {
		got := filePermissions(map[string]interface{}{"permissions": tc.written}, 0o644)
		if got != tc.want {
			t.Errorf("permissions = %v gave mode %#o, want %#o", tc.written, got, tc.want)
		}
	}
}

func TestAModeThatIsNotOneLeavesTheDefault(t *testing.T) {
	for _, written := range []interface{}{"", "rw-r--r--", nil, true} {
		if got := filePermissions(map[string]interface{}{"permissions": written}, 0o644); got != 0o644 {
			t.Errorf("permissions = %v gave mode %#o, want the default", written, got)
		}
	}
	if got := filePermissions(map[string]interface{}{}, 0o600); got != 0o600 {
		t.Errorf("with nothing written the mode was %#o", got)
	}
}

func TestAFileWrittenWithTheDocumentedModeCanBeReadBack(t *testing.T) {
	dir := t.TempDir()
	c := New("files", &Config{
		BasePath:    dir,
		Format:      "text",
		CreateDirs:  true,
		Permissions: filePermissions(map[string]interface{}{"permissions": "0644"}, 0o644),
	})

	_, err := c.writeFile(context.Background(), &connector.Data{
		Target: "note.txt",
		Params: map[string]interface{}{"content": "hello", "format": "text"},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o644) {
		t.Errorf("the file is mode %v, want -rw-r--r--", perm)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "note.txt")); err != nil {
		t.Errorf("the file it wrote cannot be read back: %v", err)
	}
}
