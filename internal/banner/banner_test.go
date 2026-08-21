package banner

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/mock"
)

// What a service prints when it starts.
//
// This is the first thing anybody sees, and the only place several facts about
// a running service appear at all — which mocks are on, which environment was
// selected, what each flow was registered as. None of it had a test.

// captured runs f with stdout redirected and returns what was printed.
func captured(t *testing.T, f func()) string {
	t.Helper()

	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		text, _ := io.ReadAll(r)
		done <- string(text)
	}()

	f()

	_ = w.Close()
	os.Stdout = original
	return <-done
}

// withColors runs f with colouring forced on or off, restoring it afterwards.
func withColors(t *testing.T, enabled bool, f func()) {
	t.Helper()
	previous := colorsEnabled
	colorsEnabled = enabled
	defer func() { colorsEnabled = previous }()
	f()
}

func TestTheVersionIsOnTheBanner(t *testing.T) {
	// It is how somebody says which version is running, in a screenshot or a
	// support conversation.
	out := captured(t, func() {
		withColors(t, false, func() { Print("2.19.0") })
	})

	if !strings.Contains(out, "v2.19.0") {
		t.Errorf("banner does not name the version:\n%s", out)
	}
	if !strings.Contains(out, "Declarative Microservice Runtime") {
		t.Errorf("banner does not say what this is:\n%s", out)
	}
}

func TestOutputWithoutColour(t *testing.T) {
	// Logs are captured to files and shipped to log systems far more often
	// than they are read in a terminal, and escape codes in a log line make it
	// unsearchable.
	out := captured(t, func() {
		withColors(t, false, func() {
			PrintServiceInfo("orders", "1.0.0", "production", 8080)
			PrintConnector("pg", "database", "postgres://…")
			PrintFlow("GET", "/orders", "database.orders")
			PrintReady()
		})
	})

	if strings.Contains(out, "\033[") {
		t.Errorf("colour codes were printed with colouring off:\n%q", out)
	}

	// And with it on, they are there — otherwise the setting does nothing.
	coloured := captured(t, func() {
		withColors(t, true, func() { PrintConnector("pg", "database", "") })
	})
	if !strings.Contains(coloured, "\033[") {
		t.Error("nothing was coloured with colouring on")
	}
}

func TestWhatAServiceSaysAboutItself(t *testing.T) {
	out := captured(t, func() {
		withColors(t, false, func() { PrintServiceInfo("orders", "1.0.0", "production", 8080) })
	})

	for _, want := range []string{"orders", "v1.0.0", "production", "8080"} {
		if !strings.Contains(out, want) {
			t.Errorf("startup output does not mention %q:\n%s", want, out)
		}
	}

	// A service with no HTTP port — a consumer, which is most of them — must
	// not print a port of zero as though it were listening on one.
	consumer := captured(t, func() {
		withColors(t, false, func() { PrintServiceInfo("consumer", "1.0.0", "production", 0) })
	})
	if strings.Contains(consumer, "Port:") {
		t.Errorf("a service that listens on nothing printed a port:\n%s", consumer)
	}
}

func TestEachFlowIsPrintedAsItWasRegistered(t *testing.T) {
	out := captured(t, func() {
		withColors(t, false, func() {
			PrintFlow("GET", "/orders", "database.orders")
			PrintFlow("QUERY", "/orders/search", "database.orders")
			PrintFlow("TCP", "orders", "queue.orders")
		})
	})

	for _, want := range []string{"GET", "/orders", "database.orders", "QUERY", "/orders/search", "TCP"} {
		if !strings.Contains(out, want) {
			t.Errorf("flow output does not mention %q:\n%s", want, out)
		}
	}

	// Methods are padded so the paths line up; a longer one is not truncated.
	if got := padMethod("GET"); got != "GET   " {
		t.Errorf("padMethod(GET) = %q", got)
	}
	if got := padMethod("QUERY"); got != "QUERY " {
		t.Errorf("padMethod(QUERY) = %q", got)
	}
	if got := padMethod("DELETE"); got != "DELETE" {
		t.Errorf("padMethod(DELETE) = %q", got)
	}
}

func TestMethodsAreToldApartByColour(t *testing.T) {
	// Reading a startup listing is scanning it, and the colour is what makes a
	// DELETE stand out from a GET. QUERY reads as safe, like GET, because it
	// is: it has a body but does not change anything.
	for method, want := range map[string]string{
		"GET":    BrightGreen,
		"QUERY":  BrightGreen,
		"POST":   BrightYellow,
		"PUT":    BrightBlue,
		"PATCH":  BrightBlue,
		"DELETE": BrightMagenta,
		"TCP":    BrightCyan,
		"WEIRD":  White,
	} {
		if got := methodToColor(method); got != want {
			t.Errorf("methodToColor(%s) = %q, want %q", method, got, want)
		}
	}

	for when, want := range map[string]string{
		"before":  BrightYellow,
		"after":   BrightBlue,
		"around":  BrightMagenta,
		"on_drop": White,
	} {
		if got := whenToColor(when); got != want {
			t.Errorf("whenToColor(%s) = %q, want %q", when, got, want)
		}
	}
}

func TestAnAspectSaysWhatItAppliesTo(t *testing.T) {
	out := captured(t, func() {
		withColors(t, false, func() {
			PrintAspect("audit_log", "after", []string{"create_*", "update_*", "delete_*"})
		})
	})

	if !strings.Contains(out, "audit_log") || !strings.Contains(out, "[after]") {
		t.Errorf("aspect output = %q", out)
	}
	// Only the first pattern fits on the line, so the rest are counted rather
	// than dropped — an aspect matching more than somebody thinks is exactly
	// the surprise this line exists to prevent.
	if !strings.Contains(out, "create_*") || !strings.Contains(out, "+2 more") {
		t.Errorf("aspect output does not account for every pattern: %q", out)
	}

	single := captured(t, func() {
		withColors(t, false, func() { PrintAspect("audit_log", "before", []string{"create_*"}) })
	})
	if strings.Contains(single, "more") {
		t.Errorf("one pattern was printed as though there were others: %q", single)
	}

	none := captured(t, func() {
		withColors(t, false, func() { PrintAspect("audit_log", "around", nil) })
	})
	if !strings.Contains(none, "audit_log") {
		t.Errorf("an aspect with no patterns was not printed: %q", none)
	}
}

func TestWhenMocksAreInUseItSaysSo(t *testing.T) {
	// The one that matters: a service answering from recorded data while
	// somebody believes it is talking to the real thing. It must be impossible
	// to miss, and equally must not appear when mocks are off.
	for name, tc := range map[string]struct {
		config *mock.Config
		want   []string
		absent bool
	}{
		"no mock configuration at all": {nil, nil, true},
		"configured but switched off":  {&mock.Config{Enabled: false}, nil, true},
		"on for everything":            {&mock.Config{Enabled: true}, []string{"Mock mode", "enabled"}, false},
		"on for named connectors": {
			&mock.Config{Enabled: true, MockOnly: []string{"payments"}},
			[]string{"only:", "payments"}, false,
		},
		"on except for named connectors": {
			&mock.Config{Enabled: true, NoMock: []string{"payments"}},
			[]string{"excluding:", "payments"}, false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			out := captured(t, func() {
				withColors(t, false, func() { PrintMockInfo(tc.config) })
			})

			if tc.absent {
				if strings.TrimSpace(out) != "" {
					t.Errorf("mocks were announced while they were off: %q", out)
				}
				return
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("mock output does not mention %q: %q", want, out)
				}
			}
		})
	}
}

func TestTheRestOfTheLifecycle(t *testing.T) {
	out := captured(t, func() {
		withColors(t, false, func() {
			PrintReady()
			PrintShutdown()
			PrintGoodbye()
			PrintError("the database refused the connection")
		})
	})

	for _, want := range []string{
		"Ready!",
		"Shutting down gracefully",
		"Goodbye!",
		"the database refused the connection",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}

func TestColourIsGivenUpWhenAsked(t *testing.T) {
	// NO_COLOR is the convention every command-line tool is expected to
	// honour, and it is read once at start-up.
	t.Setenv("NO_COLOR", "1")

	previous := colorsEnabled
	colorsEnabled = true
	defer func() { colorsEnabled = previous }()

	initColors()

	if colorsEnabled {
		t.Error("NO_COLOR was set and output was still coloured")
	}
	if got := color(BrightGreen, "text"); got != "text" {
		t.Errorf("color() = %q, want the text alone", got)
	}
}
