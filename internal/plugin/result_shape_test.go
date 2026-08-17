package plugin

import "testing"

// What a plugin answers with, and what the runtime makes of it.
//
// A module that answered with "rows" — the word the runtime itself uses for
// what a read produces, and the one a plugin author reaches for — had them
// dropped: the read came back empty with nothing to say why. Only "data" was
// read, and nothing documented that.

func TestAReadAnswersWithRowsUnderEitherName(t *testing.T) {
	c := &WASMConnector{}

	for name, answer := range map[string]interface{}{
		"rows": map[string]interface{}{
			"rows": []interface{}{
				map[string]interface{}{"sku": "WIDGET-1", "on_hand": float64(10)},
			},
		},
		"data": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"sku": "WIDGET-1", "on_hand": float64(10)},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := c.parseResult(answer)
			if err != nil {
				t.Fatalf("parseResult: %v", err)
			}
			if len(result.Rows) != 1 {
				t.Fatalf("%d rows, want the one the module answered with", len(result.Rows))
			}
			if result.Rows[0]["sku"] != "WIDGET-1" {
				t.Errorf("row = %v", result.Rows[0])
			}
		})
	}
}

func TestOneRowNeedNotBeAList(t *testing.T) {
	c := &WASMConnector{}

	result, err := c.parseResult(map[string]interface{}{
		"rows": map[string]interface{}{"sku": "WIDGET-1"},
	})
	if err != nil {
		t.Fatalf("parseResult: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("%d rows, want one", len(result.Rows))
	}
}

func TestAWriteSaysHowMuchItChanged(t *testing.T) {
	c := &WASMConnector{}

	result, err := c.parseResult(map[string]interface{}{
		"affected": float64(1),
		"rows":     []interface{}{map[string]interface{}{"sku": "WIDGET-2", "on_hand": float64(28)}},
	})
	if err != nil {
		t.Fatalf("parseResult: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d", result.Affected)
	}
	if len(result.Rows) != 1 {
		t.Errorf("the row the write produced was lost")
	}
}

func TestARefusalFromTheModuleIsAnError(t *testing.T) {
	// A plugin's own rules are the reason to write one, so its refusal has to
	// reach the flow as a failure rather than an empty answer.
	c := &WASMConnector{}

	_, err := c.parseResult(map[string]interface{}{
		"error": "only 3 of WIDGET-2 on hand, 99 asked for",
	})
	if err == nil {
		t.Fatal("the module's refusal was read as an answer")
	}
	if err.Error() != "only 3 of WIDGET-2 on hand, 99 asked for" {
		t.Errorf("error = %q, want what the module said", err)
	}
}
