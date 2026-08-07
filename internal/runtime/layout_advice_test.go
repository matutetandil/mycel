package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/parser"
)

func configWithSources(sources map[string][]string) *parser.Configuration {
	return &parser.Configuration{SourceFiles: sources}
}

// Below the threshold there is nothing worth saying. A service with a handful
// of declarations in one file is perfectly readable, and the documentation
// says so — advice that fires on it is noise.
func TestLayoutAdvice_QuietOnSmallFiles(t *testing.T) {
	sources := map[string][]string{}
	for _, n := range []string{"api", "db"} {
		sources["connector:"+n] = []string{"service.mycel"}
	}
	for _, n := range []string{"a", "b", "c"} {
		sources["flow:"+n] = []string{"service.mycel"}
	}

	if got := LayoutAdvice(configWithSources(sources)); len(got) != 0 {
		t.Errorf("expected silence for a 5-declaration file, got: %v", got)
	}
}

func TestLayoutAdvice_SuggestsSplittingACrowdedFile(t *testing.T) {
	sources := map[string][]string{}
	for i := 0; i < 5; i++ {
		sources["connector:c"+string(rune('a'+i))] = []string{"everything.mycel"}
	}
	for i := 0; i < 5; i++ {
		sources["flow:f"+string(rune('a'+i))] = []string{"everything.mycel"}
	}

	got := LayoutAdvice(configWithSources(sources))
	if len(got) != 1 {
		t.Fatalf("expected 1 line of advice, got %d: %v", len(got), got)
	}
	for _, want := range []string{"everything.mycel", "10 things", "connectors/", "flows/"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("advice missing %q: %s", want, got[0])
		}
	}
}

// The suggested directory comes from what the file actually holds. Telling
// someone with a file full of validators to use connectors/ and flows/ is
// advice they can only ignore.
func TestLayoutAdvice_NamesTheDirectoryForTheKindPresent(t *testing.T) {
	sources := map[string][]string{}
	for i := 0; i < 9; i++ {
		sources["validator:v"+string(rune('a'+i))] = []string{"validators.mycel"}
	}

	got := LayoutAdvice(configWithSources(sources))
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %v", got)
	}
	if !strings.Contains(got[0], "validators/") {
		t.Errorf("advice should name validators/: %s", got[0])
	}
	for _, unwanted := range []string{"connectors/", "flows/"} {
		if strings.Contains(got[0], unwanted) {
			t.Errorf("advice should not mention %s for a validators file: %s", unwanted, got[0])
		}
	}
}

// A declaration spread across files is normal; only the per-file count matters.
func TestLayoutAdvice_CountsPerFileNotOverall(t *testing.T) {
	sources := map[string][]string{}
	for i := 0; i < 20; i++ {
		// Twenty declarations, but two per file.
		file := "flows/f" + string(rune('a'+i/2)) + ".mycel"
		sources["flow:f"+string(rune('a'+i))] = []string{file}
	}

	if got := LayoutAdvice(configWithSources(sources)); len(got) != 0 {
		t.Errorf("a well-split project should get no advice, got: %v", got)
	}
}

func TestLayoutAdvice_NilSafe(t *testing.T) {
	if got := LayoutAdvice(nil); got != nil {
		t.Errorf("expected nil for nil config, got %v", got)
	}
}
