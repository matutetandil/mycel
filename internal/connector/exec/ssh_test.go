package exec

import (
	"context"
	"strings"
	"testing"
)

// Running a command on another machine.
//
// The command is whatever the flow was going to run, on a host reached over
// the network, so the two questions are whether the host is who it says it is
// and whether we can authenticate to it at all. Both were configured and
// neither was honoured.

func sshConnector(t *testing.T, ssh *SSHConfig) *Connector {
	t.Helper()
	return New("deploy", &Config{Driver: "ssh", Command: "/usr/local/bin/deploy.sh", SSH: ssh})
}

// sshFlags returns the options the connector would run ssh with.
func sshFlags(t *testing.T, c *Connector) []string {
	t.Helper()
	cmd := c.buildSSHCommand(context.Background(), nil)
	return cmd.Args
}

func TestVerifyingTheHostOnTheOtherEnd(t *testing.T) {
	// known_hosts was read from the configuration and never used, while
	// StrictHostKeyChecking=no sat hardcoded beside it: the one setting whose
	// purpose is to stop somebody standing in the middle of this connection
	// was accepted, stored and overridden.
	c := sshConnector(t, &SSHConfig{
		Host: "deploy.internal", User: "mycel", Port: 22,
		KeyFile: "/etc/mycel/id_ed25519", KnownHosts: "/etc/mycel/known_hosts",
	})

	flags := strings.Join(sshFlags(t, c), " ")

	if strings.Contains(flags, "StrictHostKeyChecking=no") {
		t.Errorf("host checking is off although a known_hosts file was given: %s", flags)
	}
	if !strings.Contains(flags, "StrictHostKeyChecking=yes") {
		t.Errorf("host checking was not turned on: %s", flags)
	}
	// And pointed at the file, or ssh checks against a file that is not
	// there and refuses every connection.
	if !strings.Contains(flags, "UserKnownHostsFile=/etc/mycel/known_hosts") {
		t.Errorf("ssh was not told which file to check against: %s", flags)
	}
}

func TestAHostNobodyPinned(t *testing.T) {
	// No known_hosts: the previous behaviour is kept, because turning
	// checking on for everybody would stop every deployment that has no such
	// file. It is a warning at start-up rather than a surprise.
	c := sshConnector(t, &SSHConfig{Host: "deploy.internal", User: "mycel", KeyFile: "/etc/mycel/id"})

	flags := strings.Join(sshFlags(t, c), " ")
	if !strings.Contains(flags, "StrictHostKeyChecking=no") {
		t.Errorf("a connector with no known_hosts changed behaviour: %s", flags)
	}
}

func TestWhatTheCommandIsRunAs(t *testing.T) {
	c := sshConnector(t, &SSHConfig{
		Host: "deploy.internal", User: "mycel", Port: 2222, KeyFile: "/etc/mycel/id_ed25519",
	})

	flags := sshFlags(t, c)
	joined := strings.Join(flags, " ")

	// Nothing may stop to ask: there is nobody at a terminal.
	if !strings.Contains(joined, "BatchMode=yes") {
		t.Errorf("ssh could stop to prompt: %s", joined)
	}
	if !strings.Contains(joined, "-i /etc/mycel/id_ed25519") {
		t.Errorf("the key was not offered: %s", joined)
	}
	if !strings.Contains(joined, "-p 2222") {
		t.Errorf("the port was not used: %s", joined)
	}
	if !strings.Contains(joined, "mycel@deploy.internal") {
		t.Errorf("the target is wrong: %s", joined)
	}
}

func TestArgumentsCannotBecomeCommands(t *testing.T) {
	// The command comes from the configuration and is trusted; the arguments
	// come from the message and are not. Unquoted, a value of
	// "; rm -rf /" runs on the remote host.
	c := sshConnector(t, &SSHConfig{Host: "deploy.internal", User: "mycel"})

	cmd := c.buildSSHCommand(context.Background(), []string{"; rm -rf /", "$(whoami)", "a b"})
	remote := cmd.Args[len(cmd.Args)-1]

	if strings.Contains(remote, "; rm -rf /'") == false && strings.Contains(remote, "'; rm -rf /'") == false {
		t.Errorf("an argument was not quoted: %s", remote)
	}
	// The dangerous forms must be inside quotes, not standing on their own.
	if strings.HasSuffix(remote, "; rm -rf /") {
		t.Errorf("an argument ended the command and started another: %s", remote)
	}
	if strings.Contains(remote, " $(whoami)") {
		t.Errorf("an argument was left for the remote shell to evaluate: %s", remote)
	}
}

func TestAConnectorThatCannotAuthenticate(t *testing.T) {
	// ssh has no way to be handed a password without a terminal, and this
	// runs it in batch mode precisely so nothing prompts. Accepting the
	// setting and ignoring it left a connector configured to authenticate
	// that could not, failing later in ssh's own words.
	c := sshConnector(t, &SSHConfig{Host: "deploy.internal", User: "mycel", Password: "secret"})

	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("a connector that can only authenticate with a password was accepted")
	}
	for _, want := range []string{"key_file", "password"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}

	// A key and a password together is fine: the key is what will be used.
	both := sshConnector(t, &SSHConfig{
		Host: "deploy.internal", User: "mycel",
		KeyFile: "/etc/mycel/id", Password: "secret",
	})
	if err := both.Connect(context.Background()); err != nil {
		t.Errorf("a connector with a key was refused: %v", err)
	}
}

func TestASSHConnectorWithNothingConfigured(t *testing.T) {
	c := New("deploy", &Config{Driver: "ssh", Command: "/usr/local/bin/deploy.sh"})

	if err := c.Connect(context.Background()); err == nil {
		t.Error("an ssh connector with no ssh block was accepted")
	}
}
