package database

import (
	"os"
	"path/filepath"
	"strings"
)

// SQLiteDSN attaches the pragmas every SQLite connection needs to a path, and
// makes sure the directory the file goes in exists.
//
// Both callers need the same address. The connector opens the database to
// serve flows and `mycel migrate` opens it to create their tables, and when
// the two disagreed they disagreed in ways nobody could see: migrate opened
// the bare path, so it neither waited for a lock nor enforced foreign keys,
// and it could not create the file at all when the directory was not there
// yet — which is the state of a fresh clone, since the directory these
// examples keep their database in is gitignored. `mycel migrate` was the first
// command every one of those READMEs tells you to run.
func SQLiteDSN(path string) string {
	pragmas := []string{
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(1)",
	}
	// WAL is a second file next to the database, so it means nothing to an
	// in-memory one.
	if !sqliteInMemory(path) {
		pragmas = append(pragmas, "_pragma=journal_mode(WAL)")
		ensureParentDir(path)
	}

	base := path
	if !strings.HasPrefix(path, "file:") {
		base = "file:" + path
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + strings.Join(pragmas, "&")
}

func sqliteInMemory(path string) bool {
	return path == ":memory:" || strings.Contains(path, "mode=memory")
}

// ensureParentDir creates the directory the database file lives in. A failure
// is left to the open that follows, which reports it against the actual path.
func ensureParentDir(path string) {
	clean := strings.TrimPrefix(path, "file:")
	if at := strings.IndexByte(clean, '?'); at >= 0 {
		clean = clean[:at]
	}
	dir := filepath.Dir(clean)
	if dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
}
