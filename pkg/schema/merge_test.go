package schema

import "testing"

// What happens when a connector describes an attribute the base already has.
//
// The base block describes what every connector block has — a type, a driver,
// the profile settings — and each connector's own schema describes what it
// makes of them. A cache says its driver is memory or redis; a database says
// which of four it is, and that saying so is not optional.
//
// The overlay has to win, and for a long time it did not: the base's bare
// "driver", with no values and nothing required, was kept and every
// connector's version thrown away. Nothing looked wrong — the attribute was
// there, it simply described nothing — so the check that exists to catch a
// misspelt word never saw a driver at all.

func TestAConnectorsOwnDescriptionWins(t *testing.T) {
	base := Block{
		Type: "connector",
		Attrs: []Attr{
			{Name: "type", Required: true},
			{Name: "driver", Doc: "Driver for the connector type"},
			{Name: "select"},
		},
	}
	overlay := Block{
		Attrs: []Attr{
			{Name: "driver", Doc: "Database driver", Required: true, Values: []string{"postgres", "mysql"}},
			{Name: "database", Required: true},
		},
	}

	merged := Merge(base, overlay)

	driver := attrNamed(t, merged, "driver")
	if !driver.Required {
		t.Error("the connector said a driver was required and the merge dropped it")
	}
	if len(driver.Values) != 2 {
		t.Errorf("driver values = %v, want the connector's list", driver.Values)
	}
	if driver.Doc != "Database driver" {
		t.Errorf("driver doc = %q, want the connector's", driver.Doc)
	}

	// Everything else survives: what the base had and the overlay did not
	// mention, and what the overlay added.
	for _, name := range []string{"type", "select", "database"} {
		attrNamed(t, merged, name)
	}
	if attrNamed(t, merged, "type").Required != true {
		t.Error("an attribute the overlay did not mention lost its meaning")
	}

	// One entry per name, not two.
	seen := map[string]int{}
	for _, a := range merged.Attrs {
		seen[a.Name]++
	}
	for name, count := range seen {
		if count != 1 {
			t.Errorf("%s appears %d times in the merged schema", name, count)
		}
	}
}

func TestMergingLeavesTheBaseAlone(t *testing.T) {
	// The base is built fresh each call today, but it is shared by every
	// connector within one merge chain: writing through it would let the first
	// connector merged change what every later one sees.
	base := Block{Attrs: []Attr{{Name: "driver", Doc: "Driver for the connector type"}}}

	Merge(base, Block{Attrs: []Attr{{Name: "driver", Doc: "Database driver", Required: true}}})

	if base.Attrs[0].Doc != "Driver for the connector type" || base.Attrs[0].Required {
		t.Errorf("merging changed the base block: %+v", base.Attrs[0])
	}
}

func TestChildBlocksAndDocs(t *testing.T) {
	base := Block{
		Doc:      "",
		Children: []Block{{Type: "pool"}, {Type: "tls"}},
	}
	overlay := Block{
		Doc:      "A database",
		Children: []Block{{Type: "replicas"}},
	}

	merged := Merge(base, overlay)

	if merged.Doc != "A database" {
		t.Errorf("doc = %q, want the connector's when the base has none", merged.Doc)
	}
	types := map[string]bool{}
	for _, c := range merged.Children {
		types[c.Type] = true
	}
	for _, want := range []string{"pool", "tls", "replicas"} {
		if !types[want] {
			t.Errorf("the %s block was lost in the merge", want)
		}
	}
}

func attrNamed(t *testing.T, block Block, name string) Attr {
	t.Helper()
	for _, a := range block.Attrs {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("%s is not in the merged schema", name)
	return Attr{}
}
