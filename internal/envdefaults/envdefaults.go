package envdefaults

// Defaults holds environment-aware default values.
// These are applied when the user has not explicitly configured a value.
// Priority: explicit HCL config > CLI flag > env var > environment default.
type Defaults struct {
	LogLevel         string // "debug", "info", "warn"
	LogFormat        string // "text", "json"
	HotReload        bool
	Playground       bool // GraphQL playground / GraphiQL
	DetailedHealth   bool // show component latencies in /health
	RateLimitEnabled bool
	CORSPermissive   bool // allow all origins when no CORS config
	VerboseErrors    bool // include debug info in error responses
	StackTraces      bool // include stack traces in HTTP errors
}

// ForEnvironment returns the defaults for the given environment.
// Unknown environments fall back to development defaults.
func ForEnvironment(env string) Defaults {
	switch env {
	case "production", "prod":
		return Defaults{
			LogLevel:         "warn",
			LogFormat:        "json",
			HotReload:        false,
			Playground:       false,
			DetailedHealth:   false,
			RateLimitEnabled: true,
			CORSPermissive:   false,
			VerboseErrors:    false,
			StackTraces:      false,
		}
	case "staging", "stage":
		return Defaults{
			LogLevel:         "info",
			LogFormat:        "json",
			HotReload:        true,
			Playground:       true,
			DetailedHealth:   true,
			RateLimitEnabled: true,
			CORSPermissive:   false,
			VerboseErrors:    true,
			StackTraces:      true,
		}
	default: // development, dev, or anything else
		return Defaults{
			LogLevel:         "debug",
			LogFormat:        "text",
			HotReload:        true,
			Playground:       true,
			DetailedHealth:   true,
			RateLimitEnabled: false,
			CORSPermissive:   true,
			VerboseErrors:    true,
			StackTraces:      true,
		}
	}
}
