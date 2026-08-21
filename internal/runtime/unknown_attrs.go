package runtime

import (
	"sort"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// Attributes a connector was given and does not read.
//
// The parser keeps one list of connector attributes for all twenty-odd
// connectors, so every connector accepts every connector's words. A `pool_size`
// on a database whose pool is a block, a `url` on a REST server that only
// listens, a `path` on SQLite that reads `database` — each parses, each is
// stored, and none is ever looked at. The service starts with the default the
// setting was written to replace.
//
// All three of those were in this repository's own examples.
//
// This is said rather than refused. A schema that is missing a real setting
// would otherwise turn a working service into one that will not start, and the
// grpc schema was missing two when this was written — so the cost of being
// wrong here has to fall on a log line, not on a deployment.
func (r *Runtime) warnAboutUnreadAttributes(reg *schema.Registry) {
	if reg == nil || r.config == nil {
		return
	}

	base := map[string]bool{}
	for _, a := range schema.BaseConnectorSchema().Attrs {
		base[a.Name] = true
	}
	for _, c := range schema.BaseConnectorSchema().Children {
		base[c.Type] = true
	}

	for _, cfg := range r.config.Connectors {
		for _, name := range unreadAttributes(cfg, reg, base) {
			r.logger.Warn("a connector was given a setting it does not read",
				"connector", cfg.Name,
				"type", cfg.Type,
				"attribute", name,
				"detail", "it parses and is stored, and the connector uses its default instead")
		}
	}
}

// unreadAttributes lists the properties this connector's schema does not
// describe, in a stable order.
func unreadAttributes(cfg *connector.Config, reg *schema.Registry, base map[string]bool) []string {
	if cfg == nil || cfg.Type == "" || reg.Lookup(cfg.Type, cfg.Driver) == nil {
		return nil
	}

	block := reg.ConnectorSchema(cfg.Type, cfg.Driver)
	declared := make(map[string]bool, len(block.Attrs)+len(block.Children))
	for _, a := range block.Attrs {
		declared[a.Name] = true
	}
	for _, c := range block.Children {
		declared[c.Type] = true
	}
	// An open schema takes anything by design.
	if block.Open {
		return nil
	}

	var unread []string
	for name := range cfg.Properties {
		// Names beginning with an underscore are the parser's own marks, not
		// anything somebody wrote.
		if declared[name] || base[name] || (len(name) > 0 && name[0] == '_') {
			continue
		}
		unread = append(unread, name)
	}
	sort.Strings(unread)
	return unread
}
