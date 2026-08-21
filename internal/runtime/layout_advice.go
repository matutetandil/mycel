package runtime

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/parser"
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

	// A file that declares exactly one named thing should be named after it.
	// The point of splitting is that the filename tells you where to look; a
	// flows/create_user.mycel holding delete_customer defeats it. Restricted
	// to single-declaration files, so a collection file — flows.mycel with
	// five flows — is exempt by construction rather than by a name rule.
	names := map[string][]string{}
	for key, files := range config.SourceFiles {
		i := strings.IndexByte(key, ':')
		if i < 0 {
			continue
		}
		kind, name := key[:i], key[i+1:]
		for _, f := range files {
			names[f] = append(names[f], kind+" "+strconv.Quote(name))
		}
	}

	var advice []string
	for file, decls := range names {
		if len(decls) != 1 {
			continue
		}
		kind, quoted, _ := strings.Cut(decls[0], " ")
		name, _ := strconv.Unquote(quoted)
		base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		// Names are scoped by kind, so two kinds may share one: a lock and a
		// sequence_guard both called "collection" cannot both live in
		// collection.mycel. Qualifying the file with the kind is the correct
		// convention there, and the production services this was measured
		// against use it, so accept it as naming the contents.
		if base == name || base == name+"_"+kind || base == kind+"_"+name {
			continue
		}
		advice = append(advice, fmt.Sprintf(
			"%s declares only %s %s — renaming it %s.mycel would say so",
			filepath.Base(file), kind, quoted, name))
	}

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
