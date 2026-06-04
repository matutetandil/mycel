package logging

import (
	"testing"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"512", 512, false},
		{"4k", 4096, false},
		{"4K", 4096, false},
		{"1m", 1024 * 1024, false},
		{"1M", 1024 * 1024, false},
		{"2048b", 2048, false},
		{" 4k ", 4096, false},
		{"0", 0, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-1", 0, true},
		{"4kk", 0, true},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSize(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPayloadLogFromEnv(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		t.Setenv(EnvPayloadShow, "")
		t.Setenv(EnvPayloadSize, "")
		cfg := PayloadLogFromEnv()
		if cfg.Show {
			t.Error("Show should default to false")
		}
		if cfg.MaxBytes != DefaultPayloadMaxBytes {
			t.Errorf("MaxBytes = %d, want %d", cfg.MaxBytes, DefaultPayloadMaxBytes)
		}
	})

	t.Run("show true and custom size", func(t *testing.T) {
		t.Setenv(EnvPayloadShow, "true")
		t.Setenv(EnvPayloadSize, "8k")
		cfg := PayloadLogFromEnv()
		if !cfg.Show {
			t.Error("Show should be true")
		}
		if cfg.MaxBytes != 8192 {
			t.Errorf("MaxBytes = %d, want 8192", cfg.MaxBytes)
		}
	})

	t.Run("invalid size falls back to default", func(t *testing.T) {
		t.Setenv(EnvPayloadShow, "1")
		t.Setenv(EnvPayloadSize, "garbage")
		cfg := PayloadLogFromEnv()
		if !cfg.Show {
			t.Error("Show should be true for \"1\"")
		}
		if cfg.MaxBytes != DefaultPayloadMaxBytes {
			t.Errorf("MaxBytes = %d, want default %d", cfg.MaxBytes, DefaultPayloadMaxBytes)
		}
	})
}
