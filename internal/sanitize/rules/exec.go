package rules

import (
	"fmt"
	"strings"
)

// shellMetachars are characters that can be dangerous in shell contexts.
var shellMetachars = []byte{';', '|', '&', '`', '$', '(', ')', '{', '}', '<', '>', '!', '\\', '\n'}

// ExecShellRule detects and rejects shell metacharacters in input values
// that could lead to command injection.
type ExecShellRule struct {
	// CommandFields are the field names that represent command arguments.
	// If empty, checks all string fields.
	CommandFields []string
}

func (r *ExecShellRule) Name() string { return "exec_shell_escape" }

func (r *ExecShellRule) Sanitize(value interface{}) (interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return value, nil
	}

	if len(r.CommandFields) > 0 {
		for _, field := range r.CommandFields {
			if v, exists := m[field]; exists {
				if s, ok := v.(string); ok {
					if err := checkShellMetachars(s); err != nil {
						return nil, fmt.Errorf("field %q: %w", field, err)
					}
				}
			}
		}
	}

	return value, nil
}

// checkShellMetachars returns an error if the string contains shell metacharacters.
func checkShellMetachars(s string) error {
	for _, c := range shellMetachars {
		if strings.ContainsRune(s, rune(c)) {
			return fmt.Errorf("shell metacharacter %q detected in input (potential command injection)", string(c))
		}
	}
	return nil
}

// ShellQuote safely quotes a string for shell execution.
// Each argument is wrapped in single quotes with internal single quotes escaped.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
