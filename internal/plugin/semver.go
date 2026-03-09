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
}

// String returns the version as "vMAJOR.MINOR.PATCH".
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
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
	Op      string  // "=", ">=", ">", "<=", "<", "!=", "^", "~"
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
		// Strip pre-release/build metadata for simplicity
		patchStr := strings.SplitN(parts[2], "-", 2)[0]
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
func BestMatch(versions []Version, cs ConstraintSet) (Version, bool) {
	// Sort descending (highest first)
	sorted := make([]Version, len(versions))
	copy(sorted, versions)
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
