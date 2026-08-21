package parser

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/mock"
)

// --mock and --no-mock decide which connectors answer from a file instead of
// reaching a real service. Getting the selection wrong in either direction is
// expensive: a connector that should have been mocked sends real traffic to a
// payment provider from a laptop, and one mocked by mistake makes a test pass
// against a service nobody called.

func flags(mockFlag, noMockFlag string) *mock.Config {
	config := &mock.Config{}
	ParseMockFlags(mockFlag, noMockFlag, config)
	return config
}

func TestMockingEverythingIsOneWord(t *testing.T) {
	config := flags("all", "")
	if !config.Enabled || len(config.MockOnly) != 0 {
		t.Errorf("config = %+v, want mocking on with nothing singled out", config)
	}
}

func TestNamingConnectorsTurnsMockingOnForThoseOnly(t *testing.T) {
	// Naming one is also how mocking gets turned on: nobody writes
	// --mock=all --mock=payments.
	config := flags("payments,shipping", "")
	if !config.Enabled {
		t.Error("naming a connector did not turn mocking on")
	}
	if len(config.MockOnly) != 2 || config.MockOnly[0] != "payments" {
		t.Errorf("mock-only = %v", config.MockOnly)
	}
}

func TestSpacesAfterCommasAreNotPartOfTheName(t *testing.T) {
	// A shell hands over exactly what was typed, and people type spaces.
	config := flags("payments, shipping ,billing", "")
	for i, want := range []string{"payments", "shipping", "billing"} {
		if config.MockOnly[i] != want {
			t.Errorf("mock-only = %v, want the names without the spacing", config.MockOnly)
		}
	}
}

func TestNoMockAllTurnsItOff(t *testing.T) {
	// The escape hatch for a configuration file that leaves mocking on.
	config := &mock.Config{Enabled: true}
	ParseMockFlags("", "all", config)
	if config.Enabled {
		t.Error("mocking stayed on")
	}
}

func TestAConnectorCanBeLeftOutOfTheSweep(t *testing.T) {
	config := flags("all", "payments")
	if !config.Enabled {
		t.Fatal("mocking is off")
	}
	if len(config.NoMock) != 1 || config.NoMock[0] != "payments" {
		t.Errorf("no-mock = %v", config.NoMock)
	}
}

func TestNoFlagsChangeNothing(t *testing.T) {
	config := &mock.Config{Enabled: true, MockOnly: []string{"a"}}
	ParseMockFlags("", "", config)
	if !config.Enabled || len(config.MockOnly) != 1 {
		t.Errorf("config = %+v, want it as the file left it", config)
	}
	// And nothing to write into is not a crash.
	ParseMockFlags("all", "all", nil)
}

func TestTheSelectionDecidesWhatIsActuallyMocked(t *testing.T) {
	// The flags are only worth parsing if the manager acts on them, so this
	// runs the decision the runtime makes for each connector.
	for name, tc := range map[string]struct {
		mockFlag, noMockFlag string
		mocked, real         []string
	}{
		"everything": {
			mockFlag: "all",
			mocked:   []string{"payments", "db", "email"},
		},
		"only what was named": {
			mockFlag: "payments,email",
			mocked:   []string{"payments", "email"},
			real:     []string{"db"},
		},
		"everything but one": {
			mockFlag: "all", noMockFlag: "payments",
			mocked: []string{"db", "email"},
			real:   []string{"payments"},
		},
		"named and then excluded": {
			// Contradictory, and the safe reading is the one that reaches the
			// real service last: exclusion wins.
			mockFlag: "payments", noMockFlag: "payments",
			real: []string{"payments"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			manager := mock.NewManager(flags(tc.mockFlag, tc.noMockFlag))
			for _, connector := range tc.mocked {
				if !manager.ShouldMock(connector) {
					t.Errorf("%q reaches the real service, want it mocked", connector)
				}
			}
			for _, connector := range tc.real {
				if manager.ShouldMock(connector) {
					t.Errorf("%q is mocked, want it reaching the real service", connector)
				}
			}
		})
	}
}

func TestWithMockingOffNothingIsMocked(t *testing.T) {
	manager := mock.NewManager(&mock.Config{Enabled: false, MockOnly: []string{"payments"}})
	if manager.ShouldMock("payments") {
		t.Error("a connector was mocked although mocking is off")
	}
}
