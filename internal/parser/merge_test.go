package parser

import "testing"

// Configuration.Merge is what turns a directory of .mycel files into one
// configuration, and everything a service is comes through it. A field left out
// of it is not a parse error and not a validation error: the block is read
// correctly and then dropped on the floor.
//
// That is what happened to auth. The block parsed, the schema described it, the
// examples used it — and because Merge carried ServiceConfig, MockConfig and
// Security while forgetting Auth, the runtime saw nothing, so no auth system
// was ever built and every endpoint it defines answered 404.

func TestMergeCarriesEverySingleField(t *testing.T) {
	cfg, err := tryParseFiles(t, map[string]string{
		"service.mycel": `
service {
  name = "merged"
}
`,
		"auth.mycel": `
auth {
  preset = "development"

  jwt {
    secret = "a-secret-long-enough-to-be-plausible"
  }
}
`,
		"security.mycel": `
security {
  max_input_length = 4096
}
`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if cfg.ServiceConfig == nil || cfg.ServiceConfig.Name != "merged" {
		t.Errorf("the service block did not survive the merge: %+v", cfg.ServiceConfig)
	}
	if cfg.Auth == nil {
		t.Fatal("the auth block did not survive the merge, so no auth system would be built")
	}
	if cfg.Auth.Preset != "development" {
		t.Errorf("preset = %q", cfg.Auth.Preset)
	}
	if cfg.Auth.JWT == nil || cfg.Auth.JWT.Secret == "" {
		t.Error("the auth block arrived without its jwt configuration")
	}
	if cfg.Security == nil {
		t.Error("the security block did not survive the merge")
	}
}

func TestMergeKeepsTheAuthBlockWhateverTheFileOrder(t *testing.T) {
	// The merge walks files in directory order, so a field that is only kept
	// when it arrives first would work or not depending on the file's name.
	for _, name := range []string{"a-auth.mycel", "z-auth.mycel"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := tryParseFiles(t, map[string]string{
				"m-service.mycel": "service {\n  name = \"x\"\n}\n",
				name: `
auth {
  preset = "strict"
}
`,
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if cfg.Auth == nil {
				t.Fatal("the auth block was dropped")
			}
		})
	}
}
