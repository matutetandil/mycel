package connector

import (
	"testing"
)

// Every connector reads its settings through these, and everything sourced
// from env() arrives as a string — `port = env("PORT")` is how a port is given
// to a container. A reader that refuses a numeric string does not fail: it
// returns zero, the factory falls back to its default, and the service runs
// somewhere other than where it was told to.
//
// Which is what happened. Five factories read their port with GetInt while the
// startup banner read the same property with IntFromProps, so the banner
// announced 18777 and the service listened on 3000.

func config(props map[string]interface{}) *Config {
	return &Config{Name: "api", Type: "rest", Properties: props}
}

func TestANumberIsReadHoweverItArrives(t *testing.T) {
	for name, value := range map[string]interface{}{
		"as HCL writes it":             8080,
		"as a wider integer":           int64(8080),
		"as a float, which JSON gives": float64(8080),
		"as text, which env() gives":   "8080",
	} {
		t.Run(name, func(t *testing.T) {
			if got := config(map[string]interface{}{"port": value}).GetInt("port"); got != 8080 {
				t.Errorf("port = %d, want 8080", got)
			}
		})
	}
}

func TestSomethingThatIsNotANumberIsZero(t *testing.T) {
	// The factories treat zero as "not set" and apply their own default, which
	// is the right answer for a value nobody can read as a number.
	for name, props := range map[string]map[string]interface{}{
		"absent":        {},
		"empty":         {"port": ""},
		"nothing":       {"port": nil},
		"text":          {"port": "eight thousand"},
		"a list":        {"port": []interface{}{8080}},
		"no properties": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if got := config(props).GetInt("port"); got != 0 {
				t.Errorf("port = %d, want 0 so the default applies", got)
			}
		})
	}
}

func TestAFlagIsReadHoweverItArrives(t *testing.T) {
	// The same story for a switch: a flag set in a container's environment is
	// text, and reading it as false turns a setting that was switched on into
	// one that is silently off.
	for _, value := range []interface{}{true, "true", "TRUE", "True", "1", "t"} {
		if !config(map[string]interface{}{"playground": value}).GetBool("playground") {
			t.Errorf("%v (%T) read as off", value, value)
		}
	}
	for _, value := range []interface{}{false, "false", "FALSE", "0", "f"} {
		if config(map[string]interface{}{"playground": value}).GetBool("playground") {
			t.Errorf("%v (%T) read as on", value, value)
		}
	}
}

func TestSomethingThatIsNotAFlagIsOff(t *testing.T) {
	for name, props := range map[string]map[string]interface{}{
		"absent":  {},
		"nothing": {"playground": nil},
		"text":    {"playground": "yes please"},
		"number":  {"playground": 1},
	} {
		t.Run(name, func(t *testing.T) {
			if config(props).GetBool("playground") {
				t.Errorf("%v was read as on", props)
			}
		})
	}
}

func TestTheTwoReadersOfTheSamePropertyAgree(t *testing.T) {
	// The bug was not that either was wrong on its own — it was that a
	// property read one way in the factory and another way in the banner gave
	// two answers, and the one printed was not the one used.
	for _, value := range []interface{}{8080, int64(8080), float64(8080), "8080", "", nil, "nonsense"} {
		props := map[string]interface{}{"port": value}
		viaConfig := config(props).GetInt("port")
		viaProps := IntFromProps(props, "port", 0)
		if viaConfig != viaProps {
			t.Errorf("%v (%T): the config reads %d and the runtime reads %d",
				value, value, viaConfig, viaProps)
		}
	}
}

func TestStrictReadingTellsUnsetFromZero(t *testing.T) {
	// A caller that needs to know whether a port was given at all — rather
	// than treating 0 as absent — has to be able to ask.
	if _, ok := IntFromPropsStrict(map[string]interface{}{}, "port"); ok {
		t.Error("an absent property was reported as set")
	}
	value, ok := IntFromPropsStrict(map[string]interface{}{"port": "0"}, "port")
	if !ok || value != 0 {
		t.Errorf("an explicit zero read as (%d, %v), want it reported as set", value, ok)
	}

	if _, ok := BoolFromPropsStrict(map[string]interface{}{}, "enabled"); ok {
		t.Error("an absent flag was reported as set")
	}
	on, ok := BoolFromPropsStrict(map[string]interface{}{"enabled": "false"}, "enabled")
	if !ok || on {
		t.Errorf("an explicit false read as (%v, %v), want it reported as set", on, ok)
	}
}

func TestTextAndMapsAreReadAsWritten(t *testing.T) {
	cfg := config(map[string]interface{}{
		"host": "db.internal",
		"cors": map[string]interface{}{"origins": []interface{}{"https://app.example.com"}},
	})

	if got := cfg.GetString("host"); got != "db.internal" {
		t.Errorf("host = %q", got)
	}
	if got := cfg.GetString("absent"); got != "" {
		t.Errorf("an absent property read as %q", got)
	}
	if cors := cfg.GetMap("cors"); cors == nil {
		t.Error("the cors block was not read")
	}
	if cfg.GetMap("host") != nil {
		t.Error("a string was read as a block")
	}
}

func TestAStatusCodeIsTakenOutOfTheAnswer(t *testing.T) {
	// A response block may name the status to answer with. It has to be
	// removed as it is read, or it is also sent to the caller as a field.
	for name, value := range map[string]interface{}{
		"as a number": 404,
		"as text":     "404",
	} {
		t.Run(name, func(t *testing.T) {
			result := map[string]interface{}{"http_status_code": value, "error": "not found"}
			code, ok := ExtractStatusCode(result, "http_status_code")
			if !ok || code != 404 {
				t.Fatalf("code = %d ok = %v", code, ok)
			}
			if _, present := result["http_status_code"]; present {
				t.Error("the status was left in the body as well")
			}
			if result["error"] != "not found" {
				t.Error("the rest of the answer was disturbed")
			}
		})
	}

	result := map[string]interface{}{"error": "not found"}
	if _, ok := ExtractStatusCode(result, "http_status_code"); ok {
		t.Error("an answer naming no status reported one")
	}

	notACode := map[string]interface{}{"http_status_code": "not a number"}
	if _, ok := ExtractStatusCode(notACode, "http_status_code"); ok {
		t.Error("something that is not a status was read as one")
	}
}
