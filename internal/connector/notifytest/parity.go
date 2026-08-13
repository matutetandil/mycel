// Package notifytest holds the check that keeps a notification connector's
// message and its payload reader in step.
//
// Every notification connector translates what a flow wrote — a map, because
// that is what a transform produces — into the message it sends. Each one is
// written by hand, and each one forgot different things: email dropped
// attachments and the copies, push dropped the data payload an app acts on,
// Discord dropped the card and the rules that stop a message pinging a whole
// server, Slack dropped the blocks that are the message.
//
// None of them failed. The notification arrived, looking almost right, missing
// the part somebody was relying on — which is why they went unnoticed and why
// counting on the next person to remember is not a plan.
//
// So this asks the question mechanically: for every field the message can
// carry, does the reader read it? A field that is deliberately not readable
// from a payload is exempted by name, with the reason written down — the point
// being that leaving a field out becomes a decision somebody made rather than
// one nobody noticed.
package notifytest

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Reader turns a payload into a message, which is what every one of these
// connectors does somewhere.
type Reader func(payload map[string]interface{}) (interface{}, error)

// Check fills in every field the message type declares, hands the result to the
// reader, and reports the ones that did not survive.
//
// exempt maps a payload key to the reason it is not read. An empty reason is
// refused: the value of this check is the reasons.
func Check(t *testing.T, prototype interface{}, read Reader, exempt map[string]string) {
	t.Helper()

	for key, reason := range exempt {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is exempted with no reason; say why it cannot be written by a flow", key)
		}
	}

	messageType := reflect.TypeOf(prototype)
	for messageType.Kind() == reflect.Ptr {
		messageType = messageType.Elem()
	}
	if messageType.Kind() != reflect.Struct {
		t.Fatalf("prototype is %s, want a message struct", messageType.Kind())
	}

	payload := map[string]interface{}{}
	expected := map[string]string{} // payload key -> field name
	for i := 0; i < messageType.NumField(); i++ {
		field := messageType.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		key := payloadKey(field)
		if key == "" || key == "-" {
			continue
		}
		if _, skipped := exempt[key]; skipped {
			continue
		}
		value, ok := sampleFor(field.Type, map[reflect.Type]bool{})
		if !ok {
			t.Errorf("field %s is a %s, which this check cannot make a sample of — "+
				"exempt it with a reason, or teach the check about the type",
				field.Name, field.Type)
			continue
		}
		payload[key] = value
		expected[key] = field.Name
	}

	if len(payload) == 0 {
		t.Fatal("the message type declares nothing a flow could write")
	}

	result, err := read(payload)
	if err != nil {
		t.Fatalf("reading a payload with every field set: %v", err)
	}

	message := reflect.ValueOf(result)
	for message.Kind() == reflect.Ptr {
		message = message.Elem()
	}

	var dropped []string
	for key, fieldName := range expected {
		field := message.FieldByName(fieldName)
		if !field.IsValid() {
			t.Errorf("the reader returned something with no %s field", fieldName)
			continue
		}
		if field.IsZero() {
			dropped = append(dropped, fmt.Sprintf("%s (%s)", key, fieldName))
		}
	}

	if len(dropped) > 0 {
		sort.Strings(dropped)
		t.Errorf("a flow can write these and the message does not carry them:\n  %s\n"+
			"Either read them, or exempt each by payload key with the reason it cannot be written.",
			strings.Join(dropped, "\n  "))
	}
}

// payloadKey is the name a flow writes a field under: its JSON name, since that
// is what a payload decoded from JSON or built by a transform uses.
func payloadKey(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return strings.ToLower(field.Name)
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return strings.ToLower(field.Name)
	}
	return name
}

// sampleFor builds a value of the right shape for a field, in the form a
// payload actually arrives in — a map rather than a struct, a []interface{}
// rather than a typed slice — since that is what the readers have to cope with.
//
// `seen` stops a type that contains itself from recursing for ever. Discord's
// component tree is one: an action row holds the components inside it. A
// sample one level deep is enough to tell whether the field is read at all,
// which is the question here.
func sampleFor(t reflect.Type, seen map[reflect.Type]bool) (interface{}, bool) {
	switch t.Kind() {
	case reflect.String:
		return "sample", true
	case reflect.Bool:
		return true, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return 7, true
	case reflect.Float32, reflect.Float64:
		return 7.5, true
	case reflect.Map:
		return map[string]interface{}{"key": "value"}, true

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return []byte("sample"), true // []byte is content, not a list
		}
		element, ok := sampleFor(t.Elem(), seen)
		if !ok {
			return nil, false
		}
		return []interface{}{element}, true

	case reflect.Ptr:
		return sampleFor(t.Elem(), seen)

	case reflect.Struct:
		if seen[t] {
			// Already inside one of these; stop rather than build for ever.
			return map[string]interface{}{}, true
		}
		seen[t] = true
		defer delete(seen, t)

		sample := map[string]interface{}{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			key := payloadKey(field)
			if key == "" || key == "-" {
				continue
			}
			value, ok := sampleFor(field.Type, seen)
			if !ok {
				continue // a nested field this check cannot make; the outer one still counts
			}
			sample[key] = value
		}
		if len(sample) == 0 {
			return nil, false
		}
		return sample, true
	}

	return nil, false
}
