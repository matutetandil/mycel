package examples

import (
	"encoding/json"
	"fmt"
	"testing"
)

// The transforms example, checked on what it produced.
//
// Its README commands all answer 200 whatever the expressions do — the point
// of the example is the values, so this posts the two records it shows and
// reads back what was stored.
func TestTheTransformsExampleTidiesTheRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "transforms")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	post := func(body string) {
		t.Helper()
		status, answer := svc.run(t, fmt.Sprintf(
			`curl -X POST %s/contacts -H 'Content-Type: application/json' -d '%s'`, base, body))
		if status != 200 {
			t.Fatalf("posting answered %d: %s", status, answer)
		}
	}

	post(`{"email":"  Ada.Lovelace@Example.COM ","name":"Ada Lovelace","nickname":"Ada","tags":["vip","beta"],"signed_up":"2026-03-14T09:00:00Z"}`)
	// No name, one tag as a bare string, no date: every fallback at once.
	post(`{"email":"BOB@corp.io","tags":"lead"}`)

	status, listed := svc.run(t, fmt.Sprintf(`curl %s/contacts`, base))
	if status != 200 {
		t.Fatalf("listing answered %d: %s", status, listed)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(listed), &rows); err != nil {
		t.Fatalf("the listing was not JSON: %v: %s", err, listed)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows stored, want 2: %s", len(rows), listed)
	}

	byEmail := map[string]map[string]interface{}{}
	for _, row := range rows {
		email, _ := row["email"].(string)
		byEmail[email] = row
	}

	// The address was typed with spaces and capitals, and is stored neither.
	ada, ok := byEmail["ada.lovelace@example.com"]
	if !ok {
		t.Fatalf("the address was not trimmed and lowercased: %s", listed)
	}
	for field, want := range map[string]interface{}{
		"domain":    "example.com", // split on @
		"display":   "Ada",         // the nickname won
		"initials":  "A",           // substring + upper
		"tags":      "vip,beta",    // as_list then join
		"tag_count": float64(2),    // size of the list
		"signed_up": "2026-03-14",  // format_date dropped the time
		"email_len": float64(24),   // len of the tidy address
	} {
		if ada[field] != want {
			t.Errorf("%s = %v, want %v", field, ada[field], want)
		}
	}
	// A fingerprint is SHA-256 of the tidied address: stable, and 64 hex
	// characters, which is what makes it usable as a key.
	if fingerprint, _ := ada["fingerprint"].(string); len(fingerprint) != 64 {
		t.Errorf("fingerprint = %q, want 64 hex characters", fingerprint)
	}

	// The sparse record: every fallback taken.
	bob, ok := byEmail["bob@corp.io"]
	if !ok {
		t.Fatalf("the second record is not there: %s", listed)
	}
	if bob["display"] != "friend" {
		t.Errorf("display = %v, want the last fallback", bob["display"])
	}
	if bob["tags"] != "lead" || bob["tag_count"] != float64(1) {
		t.Errorf("a tag sent as a bare string did not become a list of one: %v", bob)
	}
	if signed, _ := bob["signed_up"].(string); len(signed) != len("2026-08-26") {
		t.Errorf("signed_up = %q, want today's date", signed)
	}
}

// The list helpers, on the flow that has no destination at all.
func TestTheTransformsExampleFlattensLists(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "transforms")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}

	status, answer := svc.run(t, fmt.Sprintf(
		`curl -X POST http://localhost:%d/tags/flatten -H 'Content-Type: application/json' -d '{"groups":[["a","b"],["c"],["a"]]}'`, port))
	if status != 200 {
		t.Fatalf("answered %d: %s", status, answer)
	}

	var got struct {
		Flat     []string `json:"flat"`
		Newest   []string `json:"newest"`
		Distinct []string `json:"distinct"`
		Total    int      `json:"total"`
	}
	if err := json.Unmarshal([]byte(answer), &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, answer)
	}

	if fmt.Sprint(got.Flat) != "[a b c a]" {
		t.Errorf("flatten gave %v", got.Flat)
	}
	if fmt.Sprint(got.Newest) != "[a c b a]" {
		t.Errorf("reverse gave %v", got.Newest)
	}
	if fmt.Sprint(got.Distinct) != "[a b c]" {
		t.Errorf("unique gave %v", got.Distinct)
	}
	if got.Total != 4 {
		t.Errorf("size gave %d, want 4", got.Total)
	}
}
