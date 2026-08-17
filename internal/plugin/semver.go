package plugin

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Version represents a parsed semantic version.
type Version struct {
	Major    int
	Minor    int
	Patch    int
	Original string // original string before parsing (e.g., "v1.2.3")

	// PreRelease is what follows the patch on a tag like v2.0.0-rc.1.
	//
	// It used to be dropped, which made a release candidate indistinguishable
	// from the release: a plugin author pushing v2.0.0-rc.1 shipped it to
	// everyone whose constraint allowed 2.0.0, and once the real v2.0.0
	// existed the two compared equal, so which one a service ran came down to
	// the order the tags happened to arrive in.
	PreRelease string
}

// String returns the version as "vMAJOR.MINOR.PATCH", with the pre-release if
// the tag carried one — a log claiming v2.0.0 for a release candidate is worse
// than no log.
func (v Version) String() string {
	if v.PreRelease != "" {
		return fmt.Sprintf("v%d.%d.%d-%s", v.Major, v.Minor, v.Patch, v.PreRelease)
	}
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// IsPreRelease reports whether this is a version published for trying out
// rather than for running.
func (v Version) IsPreRelease() bool {
	return v.PreRelease != ""
}

// IsZero returns true if the version has not been set.
func (v Version) IsZero() bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && v.Original == ""
}

// Compare returns -1, 0, or 1 comparing v to other.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// Constraint represents a single version constraint (e.g., ">= 1.0.0").
type Constraint struct {
	Op      string // "=", ">=", ">", "<=", "<", "!=", "^", "~"
	Version Version
}

// ConstraintSet is a list of constraints that must all match (AND).
type ConstraintSet []Constraint

// ParseVersion parses a version string like "1.2.3" or "v1.2.3".
func ParseVersion(s string) (Version, error) {
	original := s
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimSpace(s)

	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 1 || parts[0] == "" {
		return Version{}, fmt.Errorf("invalid version: %q", original)
	}

	v := Version{Original: original}
	var err error

	v.Major, err = strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version in %q: %w", original, err)
	}

	if len(parts) >= 2 && parts[1] != "" {
		v.Minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return Version{}, fmt.Errorf("invalid minor version in %q: %w", original, err)
		}
	}

	if len(parts) >= 3 && parts[2] != "" {
		// The patch is what precedes the pre-release and the build metadata.
		// The pre-release is kept rather than dropped: see Version.PreRelease.
		patchStr, rest, hasPreRelease := strings.Cut(parts[2], "-")
		if hasPreRelease {
			v.PreRelease = strings.SplitN(rest, "+", 2)[0]
		}
		patchStr = strings.SplitN(patchStr, "+", 2)[0]
		v.Patch, err = strconv.Atoi(patchStr)
		if err != nil {
			return Version{}, fmt.Errorf("invalid patch version in %q: %w", original, err)
		}
	}

	return v, nil
}

// ParseConstraint parses a version constraint string.
// Supported formats:
//   - "" or "latest" → matches any version
//   - "1.0.0" or "v1.0.0" → exact match
//   - ">= 1.0.0", "> 1.0", "<= 2.0", "< 3.0", "!= 1.5.0"
//   - "^1.5.0" → >= 1.5.0, < 2.0.0 (caret: compatible with major)
//   - "~1.5.0" or "~> 1.5" → >= 1.5.0, < 1.6.0 (tilde: patch-level)
//   - ">= 1.0, < 2.0" → comma-separated AND constraints
func ParseConstraint(s string) (ConstraintSet, error) {
	s = strings.TrimSpace(s)

	// Empty or "latest" matches everything
	if s == "" || s == "latest" {
		return ConstraintSet{}, nil
	}

	// Comma-separated constraints
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		var set ConstraintSet
		for _, part := range parts {
			sub, err := ParseConstraint(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			set = append(set, sub...)
		}
		return set, nil
	}

	// Caret: ^1.5.0 → >= 1.5.0, < 2.0.0
	if strings.HasPrefix(s, "^") {
		v, err := ParseVersion(strings.TrimPrefix(s, "^"))
		if err != nil {
			return nil, fmt.Errorf("invalid caret constraint %q: %w", s, err)
		}
		upper := Version{Major: v.Major + 1}
		if v.Major == 0 {
			// ^0.5.0 → >= 0.5.0, < 0.6.0
			upper = Version{Major: 0, Minor: v.Minor + 1}
		}
		return ConstraintSet{
			{Op: ">=", Version: v},
			{Op: "<", Version: upper},
		}, nil
	}

	// Tilde: ~1.5.0 or ~> 1.5 → >= 1.5.0, < 1.6.0
	tildePrefix := ""
	if strings.HasPrefix(s, "~>") {
		tildePrefix = "~>"
	} else if strings.HasPrefix(s, "~") {
		tildePrefix = "~"
	}
	if tildePrefix != "" {
		v, err := ParseVersion(strings.TrimSpace(strings.TrimPrefix(s, tildePrefix)))
		if err != nil {
			return nil, fmt.Errorf("invalid tilde constraint %q: %w", s, err)
		}
		upper := Version{Major: v.Major, Minor: v.Minor + 1}
		return ConstraintSet{
			{Op: ">=", Version: v},
			{Op: "<", Version: upper},
		}, nil
	}

	// Operator prefixed: >=, >, <=, <, !=
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(s, op) {
			vStr := strings.TrimSpace(strings.TrimPrefix(s, op))
			v, err := ParseVersion(vStr)
			if err != nil {
				return nil, fmt.Errorf("invalid constraint %q: %w", s, err)
			}
			return ConstraintSet{{Op: op, Version: v}}, nil
		}
	}

	// Bare version: exact match
	v, err := ParseVersion(s)
	if err != nil {
		return nil, fmt.Errorf("invalid constraint %q: %w", s, err)
	}
	return ConstraintSet{{Op: "=", Version: v}}, nil
}

// Match returns true if the version satisfies all constraints.
// NamesPreRelease reports whether the constraint asked for this exact
// pre-release by name, which is the only way to run one: version = "v2.0.0-rc.1".
func (cs ConstraintSet) NamesPreRelease(v Version) bool {
	for _, c := range cs {
		if c.Op == "=" && c.Version.PreRelease == v.PreRelease && c.Version.PreRelease != "" {
			return true
		}
	}
	return false
}

func (cs ConstraintSet) Match(v Version) bool {
	// Empty constraint set matches everything
	if len(cs) == 0 {
		return true
	}
	for _, c := range cs {
		if !c.match(v) {
			return false
		}
	}
	return true
}

func (c Constraint) match(v Version) bool {
	cmp := v.Compare(c.Version)
	switch c.Op {
	case "=", "":
		return cmp == 0
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	case "!=":
		return cmp != 0
	default:
		return false
	}
}

// BestMatch returns the highest version from the list that satisfies
// the constraint set. Returns false if no version matches.
// BestMatch returns the highest version the constraint allows.
//
// A pre-release is never chosen for a constraint that does not name it. Every
// package manager works this way, and the alternative is a release candidate
// reaching everybody who asked for the version it is a candidate for.
func BestMatch(versions []Version, cs ConstraintSet) (Version, bool) {
	// Sort descending (highest first)
	sorted := make([]Version, 0, len(versions))
	for _, v := range versions {
		if v.IsPreRelease() && !cs.NamesPreRelease(v) {
			continue
		}
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Compare(sorted[j]) > 0
	})

	for _, v := range sorted {
		if cs.Match(v) {
			return v, true
		}
	}
	return Version{}, false
}

// SortVersions sorts versions in ascending order.
func SortVersions(versions []Version) {
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(versions[j]) < 0
	})
}
