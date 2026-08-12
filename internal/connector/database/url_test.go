package database

import "testing"

// Discrete fields stay the primary way to configure a database. A URL exists
// for the one thing they cannot do: accept what a managed platform hands over,
// which is always a single DATABASE_URL and which HCL cannot take apart.

func TestParseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want URLFields
	}{
		{
			name: "postgres with everything",
			raw:  "postgres://alice:s3cret@db.example.com:5433/app?sslmode=require",
			want: URLFields{Host: "db.example.com", Port: 5433, Database: "app", User: "alice", Password: "s3cret",
				Options: map[string]string{"sslmode": "require"}},
		},
		{
			name: "the postgresql scheme spelt out",
			raw:  "postgresql://alice@db:5432/app",
			want: URLFields{Host: "db", Port: 5432, Database: "app", User: "alice", Options: map[string]string{}},
		},
		{
			name: "mysql",
			raw:  "mysql://root:pw@127.0.0.1:3306/shop?charset=utf8mb4",
			want: URLFields{Host: "127.0.0.1", Port: 3306, Database: "shop", User: "root", Password: "pw",
				Options: map[string]string{"charset": "utf8mb4"}},
		},
		{
			name: "no port, so the factory default applies",
			raw:  "postgres://alice:pw@db.example.com/app",
			want: URLFields{Host: "db.example.com", Database: "app", User: "alice", Password: "pw", Options: map[string]string{}},
		},
		{
			name: "no credentials",
			raw:  "postgres://db:5432/app",
			want: URLFields{Host: "db", Port: 5432, Database: "app", Options: map[string]string{}},
		},
		{
			// A password with reserved characters is exactly why this is parsed
			// rather than split on punctuation.
			name: "percent-encoded password",
			raw:  "postgres://alice:p%40ss%3Aword@db:5432/app",
			want: URLFields{Host: "db", Port: 5432, Database: "app", User: "alice", Password: "p@ss:word", Options: map[string]string{}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseURL(tc.raw)
			if err != nil {
				t.Fatalf("ParseURL: %v", err)
			}
			if got.Host != tc.want.Host || got.Port != tc.want.Port || got.Database != tc.want.Database ||
				got.User != tc.want.User || got.Password != tc.want.Password {
				t.Errorf("got %+v, want %+v", *got, tc.want)
			}
			for k, want := range tc.want.Options {
				if got.Options[k] != want {
					t.Errorf("option %s = %q, want %q", k, got.Options[k], want)
				}
			}
		})
	}
}

func TestParseURLRejectsNonsense(t *testing.T) {
	for _, raw := range []string{"", "   ", "just-a-string", "://missing-scheme"} {
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("ParseURL(%q) was accepted", raw)
		}
	}
}

func TestApplyURLFillsProperties(t *testing.T) {
	props := map[string]interface{}{
		"url": "postgres://alice:pw@db.example.com:5433/app?sslmode=require",
	}
	if err := ApplyURL(props); err != nil {
		t.Fatalf("ApplyURL: %v", err)
	}
	for k, want := range map[string]interface{}{
		"host": "db.example.com", "port": 5433, "database": "app",
		"user": "alice", "password": "pw", "sslmode": "require",
	} {
		if props[k] != want {
			t.Errorf("%s = %#v, want %#v", k, props[k], want)
		}
	}
}

func TestExplicitFieldsWinOverTheURL(t *testing.T) {
	// Writing both is how you point one connection string at a different
	// database on the same server; and on the reading of an accident, the
	// hand-written value is the one that was meant.
	props := map[string]interface{}{
		"url":      "postgres://alice:pw@db.example.com:5433/app",
		"database": "analytics",
	}
	if err := ApplyURL(props); err != nil {
		t.Fatalf("ApplyURL: %v", err)
	}
	if props["database"] != "analytics" {
		t.Errorf("database = %v, want the explicit analytics", props["database"])
	}
	// Everything not written by hand still comes from the URL.
	if props["host"] != "db.example.com" || props["user"] != "alice" {
		t.Errorf("host/user = %v / %v", props["host"], props["user"])
	}
}

func TestApplyURLWithoutAURLIsANoOp(t *testing.T) {
	props := map[string]interface{}{"host": "localhost", "database": "app"}
	if err := ApplyURL(props); err != nil {
		t.Fatalf("ApplyURL: %v", err)
	}
	if len(props) != 2 {
		t.Errorf("properties grew to %#v", props)
	}
}

func TestApplyURLReportsABadURL(t *testing.T) {
	// A malformed connection string has to fail at startup with the string in
	// hand, not at the first query with a driver-level error.
	props := map[string]interface{}{"url": "not a url at all"}
	if err := ApplyURL(props); err == nil {
		t.Error("a malformed url was accepted")
	}
}
