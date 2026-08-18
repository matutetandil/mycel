package ftp

import (
	"strings"
	"testing"
	"time"
)

// The mode transfers happen in.
//
// FTP opens a second connection for the data, and which end opens it is the
// whole difference: in passive mode we connect out to the server, which works
// from behind NAT; in active mode the server connects back to us, which almost
// never survives a firewall. The setting was read from the configuration into
// a field nothing looked at.

func TestActiveTransfersAreRefusedRatherThanIgnored(t *testing.T) {
	// The library underneath has no active mode, so `passive = false` cannot
	// be honoured. Ignoring it left somebody who asked for active transfers
	// getting passive ones with no word about it — and if they asked, it was
	// because they believed they needed the other one.
	_, err := newFTPClient(&Config{
		Host: "ftp.example.test", Port: 21, Passive: false, Timeout: time.Second,
	})

	if err == nil {
		t.Fatal("active mode was accepted, and transfers would have been passive anyway")
	}
	if !strings.Contains(err.Error(), "passive") {
		t.Errorf("the error does not say what to do about it: %v", err)
	}
	// Refused before anything is dialled: this is a configuration mistake, not
	// a network one, and it should not depend on a server being reachable.
	if strings.Contains(err.Error(), "dial") {
		t.Errorf("the refusal came from the network rather than the configuration: %v", err)
	}
}

func TestPassiveIsTheDefault(t *testing.T) {
	// A connector that says nothing gets passive, which is what works.
	config := DefaultConfig()
	if !config.Passive {
		t.Error("transfers default to active, which almost never survives a firewall")
	}
}
