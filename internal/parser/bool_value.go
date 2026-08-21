package parser

import (
	"fmt"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

// boolValue reads an attribute that holds a yes or a no, accepting the several
// ways a person writes one.
//
// cty's True() panics on anything that is not a boolean, so a string where a
// boolean was wanted took the whole binary down — and that is not only a typo:
// env() always returns a string, so `mfa { enabled = env("MFA_ON") }` handed a
// string to a boolean every time. Integers already had a coercion for exactly
// this reason; booleans did not.
func boolValue(name string, val cty.Value) (bool, error) {
	if val.IsNull() {
		return false, nil
	}

	switch val.Type() {
	case cty.Bool:
		return val.True(), nil
	case cty.String:
		switch strings.ToLower(strings.TrimSpace(val.AsString())) {
		case "true", "yes", "on", "1":
			return true, nil
		case "", "false", "no", "off", "0":
			return false, nil
		}
		return false, fmt.Errorf("attribute %q must be true or false, and %q is neither",
			name, val.AsString())
	case cty.Number:
		// 1 and 0, which is how a number ends up here at all.
		return !val.RawEquals(cty.NumberIntVal(0)), nil
	}

	return false, fmt.Errorf("attribute %q must be true or false, and %s cannot be read as either",
		name, val.Type().FriendlyName())
}

// boolOrFalse is boolValue where the caller has nowhere to put an error. An
// unreadable value is false, which is what it was before this existed — minus
// the crash.
func boolOrFalse(val cty.Value) bool {
	b, _ := boolValue("", val)
	return b
}
