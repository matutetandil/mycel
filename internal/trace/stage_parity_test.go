package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// A stage nobody emits is a breakpoint that never fires.
//
// Five stages were declared here, given virtual lines by the debug adapter and
// offered by the editor as places to stop, and nothing in the runtime ever
// recorded them: a breakpoint on dedupe, enrich or step waited for an event
// that had no source, and a trace of a flow with steps did not show the steps
// running. The stage names existed at every layer except the one that had to
// announce them.

// allStages is every stage this package declares. Written out rather than
// derived, so that a stage added to the constants and to nothing else fails
// here.
var allStages = []Stage{
	StageInput, StageSanitize, StageFilter, StageAccept, StageDedupe,
	StageValidateIn, StageEnrich, StageTransform, StageStep,
	StageValidateOut, StageRead, StageWrite, StageResponse,
	StageCacheHit, StageCacheMiss,
}

// notEmittedYet lists stages nothing records, each with the reason it is
// allowed to stay that way. An entry here is a decision, not an oversight.
var notEmittedYet = map[Stage]string{
	// The cache stages are recorded by the renderer's vocabulary but the cache
	// path reports its hit or miss through the flow result rather than the
	// trace. Wiring them is worth doing; leaving them silent is at least
	// visible here.
	StageCacheHit:  "the cache path does not record its verdict",
	StageCacheMiss: "the cache path does not record its verdict",
}

func TestEveryStageIsEmittedSomewhere(t *testing.T) {
	root := repoRoot(t)
	sources := goSourcesUnder(t, filepath.Join(root, "internal"))

	for _, stage := range allStages {
		constant := constantNameFor(stage)
		if constant == "" {
			t.Fatalf("no constant name known for stage %q; the list in this test is out of date", stage)
		}

		emitted := false
		for path, src := range sources {
			// Its own package declares and renders them; only a use elsewhere
			// counts as something a running flow would produce.
			if strings.Contains(path, string(filepath.Separator)+"trace"+string(filepath.Separator)) {
				continue
			}
			if strings.Contains(src, "trace."+constant) {
				emitted = true
				break
			}
		}

		if emitted {
			if reason, listed := notEmittedYet[stage]; listed {
				t.Errorf("stage %q is emitted now; remove it from notEmittedYet (was: %s)", stage, reason)
			}
			continue
		}
		if _, allowed := notEmittedYet[stage]; allowed {
			continue
		}
		t.Errorf("stage %q is declared, can be broken on and appears in traces, and nothing in the runtime records it — "+
			"a breakpoint there never fires", stage)
	}
}

func constantNameFor(stage Stage) string {
	switch stage {
	case StageInput:
		return "StageInput"
	case StageSanitize:
		return "StageSanitize"
	case StageFilter:
		return "StageFilter"
	case StageAccept:
		return "StageAccept"
	case StageDedupe:
		return "StageDedupe"
	case StageValidateIn:
		return "StageValidateIn"
	case StageEnrich:
		return "StageEnrich"
	case StageTransform:
		return "StageTransform"
	case StageStep:
		return "StageStep"
	case StageValidateOut:
		return "StageValidateOut"
	case StageResponse:
		return "StageResponse"
	case StageRead:
		return "StageRead"
	case StageWrite:
		return "StageWrite"
	case StageCacheHit:
		return "StageCacheHit"
	case StageCacheMiss:
		return "StageCacheMiss"
	}
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root")
	return ""
}

func goSourcesUnder(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("no sources found under %s; this test is checking nothing", root)
	}
	return out
}

func TestThePipelineOrderNamesRealStages(t *testing.T) {
	// The ordered list lives in the schema package, where the editor and the
	// debug adapter can read it too. It has to name the same stages this
	// package declares — a name in one and not the other is a stage somebody
	// can ask to stop at and nothing will ever emit.
	declared := make(map[Stage]bool, len(allStages))
	for _, stage := range allStages {
		declared[stage] = true
	}

	for _, stage := range schema.PipelineStages() {
		if !declared[Stage(stage)] {
			t.Errorf("the pipeline order names %q, and no stage of that name is declared", stage)
		}
	}

	// And the other way, for the stages that are points in the pipeline. The
	// cache verdicts are reported after the fact rather than reached.
	notAPoint := map[Stage]bool{StageCacheHit: true, StageCacheMiss: true}
	inOrder := make(map[string]bool)
	for _, stage := range schema.PipelineStages() {
		inOrder[stage] = true
	}
	for _, stage := range allStages {
		if notAPoint[stage] {
			continue
		}
		if !inOrder[string(stage)] {
			t.Errorf("stage %q is declared and has no place in the pipeline order, so nothing can say when it runs", stage)
		}
	}
}
