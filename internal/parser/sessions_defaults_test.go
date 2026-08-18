package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionsDefaultsHoldThroughTheParser(t *testing.T) {
	dir := t.TempDir()
	body := `
auth {
  jwt { secret = "a-secret-long-enough-for-signing" }
  sessions {
    max_active   = 5
    allow_revoke = false
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "auth.mycel"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Written off stays off; not written stays on, because the block being
	// there is not a decision about a setting nobody mentioned.
	if cfg.Auth.Sessions.AllowRevoke {
		t.Error("allow_revoke was written false and came back true")
	}
	if !cfg.Auth.Sessions.AllowList {
		t.Error("allow_list was not mentioned and came back false, which closes an endpoint nobody closed")
	}
}

func TestASessionBlockThatSaysNothingStillSlides(t *testing.T) {
	// Every deployment with a sessions block. A boolean nobody wrote is false,
	// and false here means a fixed window, so decoding straight into the
	// struct would turn every one of them into a session that ends thirty
	// minutes after sign-in however busy the person was.
	dir := t.TempDir()
	body := `
auth {
  jwt { secret = "a-secret-long-enough-for-signing" }
  sessions {
    idle_timeout = "30m"
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "auth.mycel"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Auth.Sessions.ExtendOnActivity {
		t.Error("a sessions block that never mentioned extend_on_activity came back fixed")
	}
}

func TestAFixedWindowSurvivesTheParser(t *testing.T) {
	dir := t.TempDir()
	body := `
auth {
  preset = "strict"
  jwt { secret = "a-secret-long-enough-for-signing" }
  sessions {
    idle_timeout       = "30m"
    extend_on_activity = false
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "auth.mycel"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Auth.Sessions.ExtendOnActivity {
		t.Error("extend_on_activity was written false and came back true")
	}
}
