package connector

import (
	"fmt"
	"strings"
)

// ResolveTarget returns the addressee a push connector should deliver to.
//
// For a database, a target is a table: a name written once in the
// configuration. For the connectors that push to whoever is connected, it is a
// value carried by the message — a room taken from the path, a user id from the
// body — and the documentation writes that the way everything else in Mycel
// refers to a field of the message:
//
//	to {
//	  connector = "events"
//	  operation = "send_to_user"
//	  target    = "input.user_id"
//	}
//
// Nothing evaluated it, so that reached the connector as the literal string
// "input.user_id" and the message was delivered to a user of that name — which
// is to say, to nobody, and reported as sent.
//
// A target that does not name a field is returned unchanged, so a literal room
// still addresses that room.
func ResolveTarget(target string, payload map[string]interface{}) string {
	const prefix = "input."
	if !strings.HasPrefix(target, prefix) || payload == nil {
		return target
	}

	value, found := lookupPath(payload, strings.TrimPrefix(target, prefix))
	if !found {
		return target
	}
	return identifier(value)
}

// identifier renders a value as the id it stands for, however the number was
// typed: an id from a database is as likely to be a number as a word, and JSON
// makes every number a float.
func identifier(value interface{}) string {
	switch id := value.(type) {
	case nil:
		return ""
	case string:
		return id
	case float64:
		if id == float64(int64(id)) {
			return fmt.Sprintf("%d", int64(id))
		}
		return fmt.Sprintf("%v", id)
	default:
		return fmt.Sprintf("%v", id)
	}
}

// lookupPath walks a dotted path through nested maps.
func lookupPath(values map[string]interface{}, path string) (interface{}, bool) {
	current := interface{}(values)
	for _, part := range strings.Split(path, ".") {
		asMap, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = asMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
