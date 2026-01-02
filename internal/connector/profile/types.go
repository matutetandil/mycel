// Package profile provides connector profile functionality.
// Profiles allow a single logical connector to have multiple backend implementations.
package profile

import (
	"github.com/matutetandil/mycel/internal/connector"
)

// Config holds the profile configuration for a connector.
type Config struct {
	// Select is a CEL expression that returns the profile name to use.
	// Example: env("PRICE_SOURCE")
	Select string

	// Default is the profile to use when Select evaluates to empty/null.
	Default string

	// Fallback is an ordered list of profiles to try if the active profile fails.
	Fallback []string

	// Profiles maps profile names to their configurations.
	Profiles map[string]*ProfileDef
}

// ProfileDef defines a single profile with its connector configuration and transform.
type ProfileDef struct {
	// Name is the profile identifier.
	Name string

	// ConnectorConfig is the underlying connector configuration.
	ConnectorConfig *connector.Config

	// Transform holds CEL expressions for normalizing data.
	// Keys are output field names, values are CEL expressions.
	Transform map[string]string
}

// HasProfiles returns true if the connector has profiles defined.
func (c *Config) HasProfiles() bool {
	return len(c.Profiles) > 0
}

// GetProfile returns a profile by name.
func (c *Config) GetProfile(name string) (*ProfileDef, bool) {
	if c.Profiles == nil {
		return nil, false
	}
	p, ok := c.Profiles[name]
	return p, ok
}

// ProfileNames returns the names of all defined profiles.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	return names
}
