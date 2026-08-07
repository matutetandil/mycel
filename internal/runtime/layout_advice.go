package runtime

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/internal/parser"
)

// crowdedFileThreshold is when a single file stops being easy to read. Below
// this, splitting is busywork; a service with three connectors and two flows in
// one file is perfectly clear and the docs say so.
const crowdedFileThreshold = 8

// LayoutAdvice suggests splitting a file that has grown past the point of being
// readable.
//
// Deliberately advice, and deliberately only reachable from `mycel validate`.
// Where a declaration lives changes nothing at runtime — Mycel merges every
// .mycel file under the config directory — so this can never be an error, and
// it must never reach `mycel start`. A consumer restarting at 3am has no use
// for an opinion about file organisation, and a startup that cries wolf about
// style is a startup whose real warnings get ignored.
//
// Returns one line per crowded file, sorted, or nil when there is nothing worth
// saying.
func LayoutAdvice(config *parser.Configuration) []string {
	if config == nil {
		return nil
	}

	// Count declarations per file, and remember which kinds each holds: a file
	// mixing connectors and flows is the one worth splitting first, since that
	// is the split that makes a project navigable.
	counts := map[string]int{}
	kinds := map[string]map[string]bool{}

	for key, files := range config.SourceFiles {
		kind := key
		if i := strings.IndexByte(key, ':'); i >= 0 {
			kind = key[:i]
		}
		for _, f := range files {
			counts[f]++
			if kinds[f] == nil {
				kinds[f] = map[string]bool{}
			}
			kinds[f][kind] = true
		}
	}

	var advice []string
	for file, n := range counts {
		if n < crowdedFileThreshold {
			continue
		}
		dirs := suggestedDirs(kinds[file])
		switch len(dirs) {
		case 1:
			// A file holding one kind only needs splitting, not sorting.
			advice = append(advice, fmt.Sprintf(
				"%s declares %d %s — consider one per file under %s",
				filepath.Base(file), n, strings.TrimSuffix(dirs[0], "/"), dirs[0]))
		default:
			advice = append(advice, fmt.Sprintf(
				"%s declares %d things — consider splitting into %s",
				filepath.Base(file), n, joinDirs(dirs)))
		}
	}

	sort.Strings(advice)
	return advice
}

// suggestedDirs turns the kinds a file holds into the directories they would
// live in. Derived from what is actually in the file: telling someone with a
// file full of validators to use connectors/ and flows/ is advice they can
// only ignore.
func suggestedDirs(set map[string]bool) []string {
	dirs := make([]string, 0, len(set))
	for k := range set {
		dirs = append(dirs, k+"s/")
	}
	sort.Strings(dirs)
	return dirs
}

// joinDirs renders a directory list as "a/, b/ and c/".
func joinDirs(dirs []string) string {
	switch len(dirs) {
	case 0:
		return ""
	case 1:
		return dirs[0]
	}
	return strings.Join(dirs[:len(dirs)-1], ", ") + " and " + dirs[len(dirs)-1]
}
