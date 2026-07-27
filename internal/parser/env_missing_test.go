package parser

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// parseBody parses src and returns the body of its first block.
func parseBody(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()

	file, diags := hclsyntax.ParseConfig([]byte(src), "test.mycel", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse error: %s", diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok || len(body.Blocks) == 0 {
		t.Fatalf("expected at least one block")
	}
	return body.Blocks[0].Body
}

func TestCollectMissingEnv(t *testing.T) {
	t.Setenv("MYCEL_TEST_SET", "http://example.com")

	tests := []struct {
		name string
		src  string
		want []string // "ATTR=ENV_NAME"
	}{
		{
			name: "unset variable is reported",
			src: `connector "url_ms" {
				type     = "http"
				base_url = env("MYCEL_TEST_MISSING")
			}`,
			want: []string{"base_url=MYCEL_TEST_MISSING"},
		},
		{
			name: "variable with default is not reported",
			src: `connector "url_ms" {
				base_url = env("MYCEL_TEST_MISSING", "http://localhost")
			}`,
			want: nil,
		},
		{
			name: "set variable is not reported",
			src: `connector "url_ms" {
				base_url = env("MYCEL_TEST_SET")
			}`,
			want: nil,
		},
		{
			name: "interpolated variable is reported",
			src: `connector "url_ms" {
				base_url = "${env("MYCEL_TEST_MISSING")}/api"
			}`,
			want: []string{"base_url=MYCEL_TEST_MISSING"},
		},
		{
			name: "nested block attribute keeps its path",
			src: `connector "mq" {
				consumer {
					queue = env("MYCEL_TEST_QUEUE")
				}
			}`,
			want: []string{"consumer.queue=MYCEL_TEST_QUEUE"},
		},
		{
			name: "multiple attributes are all reported",
			src: `connector "pg" {
				host     = env("MYCEL_TEST_HOST")
				password = env("MYCEL_TEST_PASSWORD")
			}`,
			want: []string{"host=MYCEL_TEST_HOST", "password=MYCEL_TEST_PASSWORD"},
		},
		{
			name: "no env calls at all",
			src: `connector "sqlite" {
				database = "./data/app.db"
			}`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectMissingEnv(parseBody(t, tt.src))

			if len(got) != len(tt.want) {
				t.Fatalf("got %d missing vars %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i, m := range got {
				if key := m.Attr + "=" + m.Name; key != tt.want[i] {
					t.Errorf("entry %d = %q, want %q", i, key, tt.want[i])
				}
			}
		})
	}
}
