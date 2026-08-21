package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v2/internal/parser"
)

// What a step does when its call fails.
//
// Three words are implemented — fail, skip and default — and anything else
// falls through to failing. So `on_error = "ignore"`, which is what somebody
// writing from memory reaches for, means the opposite of what it says: the
// step's failure takes the whole flow down.
//
// `default` with nothing to default to falls through the same way, which is a
// setting that reads as handled and behaves as unhandled.

// stepErrorWords are the dispositions a step understands.
var stepErrorWords = map[string]bool{"fail": true, "skip": true, "default": true}

// ValidateStepErrorHandling reports a step whose on_error cannot do what it
// says.
func ValidateStepErrorHandling(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil {
			continue
		}
		for i, step := range f.Steps {
			if step == nil {
				continue
			}
			name := step.Name
			if name == "" {
				name = fmt.Sprintf("%d", i)
			}

			word := strings.TrimSpace(step.OnError)
			if word != "" && !stepErrorWords[word] {
				errs = append(errs, fmt.Errorf(
					"flow %q: step %q has on_error = %q, which is not one of %s — anything else fails the flow",
					f.Name, name, step.OnError, wordList(stepErrorWords)))
				continue
			}
			if word == "default" && step.Default == nil {
				errs = append(errs, fmt.Errorf(
					`flow %q: step %q asks for on_error = "default" and gives no default, `+
						`so a failure takes the flow down as if it said "fail"`,
					f.Name, name))
			}
		}
	}
	return errs
}

func wordList(words map[string]bool) string {
	out := make([]string, 0, len(words))
	for w := range words {
		out = append(out, w)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
