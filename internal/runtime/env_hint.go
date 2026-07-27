package runtime

import (
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/internal/connector"
)

// missingEnvHint builds a suffix naming the environment variables the connector
// referenced through env() that are not set. Those calls evaluate to an empty
// string, so factories report a generic "requires X" error; this points at the
// real cause. Returns an empty string when nothing is missing.
func missingEnvHint(cfg *connector.Config) string {
	if cfg == nil || len(cfg.MissingEnv) == 0 {
		return ""
	}

	var b strings.Builder
	for _, m := range cfg.MissingEnv {
		b.WriteString(fmt.Sprintf(
			"\n      → Missing environment variable %q, required by connector %q (%s)",
			m.Name, cfg.Name, m.Attr))
	}
	return b.String()
}
