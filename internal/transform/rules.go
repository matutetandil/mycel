package transform

import "sort"

// RulesFromMappings converts a `field = "<celExpr>"` mapping into the rule list
// the transformer evaluates, in the order the fields were declared in the
// source file.
//
// Order is load-bearing: each rule's result is written into `output` before the
// next rule runs, so an expression may reference a field computed above it
// (`tax = "output.subtotal * 0.21"`). A Go map has no order, so ranging over
// the mapping directly picked a fresh random order on every message and a
// backward reference resolved — or didn't — at random. The parser records the
// declaration order alongside the map and hands it here.
//
// Keys missing from order are appended sorted rather than dropped: configs
// built in code (tests, generators) carry no order, and a deterministic
// fallback beats map iteration even when it cannot honour the source.
func RulesFromMappings(mappings map[string]string, order []string) []Rule {
	rules := make([]Rule, 0, len(mappings))
	placed := make(map[string]bool, len(mappings))

	for _, target := range order {
		expr, ok := mappings[target]
		if !ok || placed[target] {
			continue
		}
		placed[target] = true
		rules = append(rules, Rule{Target: target, Expression: expr})
	}

	if len(rules) == len(mappings) {
		return rules
	}

	rest := make([]string, 0, len(mappings)-len(rules))
	for target := range mappings {
		if !placed[target] {
			rest = append(rest, target)
		}
	}
	sort.Strings(rest)
	for _, target := range rest {
		rules = append(rules, Rule{Target: target, Expression: mappings[target]})
	}

	return rules
}

// MergeOrder combines a named block's field order with the inline overrides
// layered on top of it. Fields the inline block redefines keep their position
// in the base — an override replaces a value, it does not move the field — and
// fields only the inline block declares follow, in their own declared order.
func MergeOrder(base, inline []string) []string {
	merged := make([]string, 0, len(base)+len(inline))
	seen := make(map[string]bool, len(base)+len(inline))
	for _, name := range append(append([]string{}, base...), inline...) {
		if seen[name] {
			continue
		}
		seen[name] = true
		merged = append(merged, name)
	}
	return merged
}
