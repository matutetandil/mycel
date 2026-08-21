package main

import (
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/ide"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Where `mycel add` writes and where an editor expects to find it have to be
// the same place.
//
// They were not: `mycel add state-machine` wrote to state_machines/, an
// editor's list of known directories did not include it, and a project laid out
// by Mycel's own command was told to lay itself out.

func TestEveryKindTheCommandWritesHasADirectoryTheEditorKnows(t *testing.T) {
	// The kinds `mycel add` can create. Written out rather than derived from
	// the map under test, so that removing an entry from the map is a failure
	// rather than a smaller test.
	kinds := []string{
		"connector", "flow", "type", "transform",
		"aspect", "validator", "saga", "state_machine",
	}

	for _, kind := range kinds {
		dir := schema.DirectoryFor(kind)
		if dir == "" {
			t.Errorf("`mycel add %s` has no directory of its own", kind)
			continue
		}

		// The editor reads the mapping both ways: it has to recognise the
		// directory as organised, and suggest the same one for a block of this
		// type found elsewhere.
		if got := ide.DirectoryForBlock(kind); got != dir {
			t.Errorf("the command writes %s to %s/ and the editor expects %s/", kind, dir, got)
		}
	}
}

func TestABlockDeclaredOnceForTheServiceIsNotMoved(t *testing.T) {
	// service, auth and security are declared once and live wherever the
	// author put them; suggesting a directory for them would be noise.
	for _, kind := range []string{"service", "auth", "security"} {
		if dir := schema.DirectoryFor(kind); dir != "" {
			t.Errorf("%s was given directory %s/, and it is declared once for the whole service", kind, dir)
		}
	}
}
