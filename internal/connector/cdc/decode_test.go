package cdc

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgtype"
)

// What a row change becomes by the time a flow reads it. A column decoded as
// the wrong type is a flow comparing a number to a string and finding they
// differ — which looks like the data being wrong rather than the decoding.

func relation() *RelationInfo {
	return &RelationInfo{
		Schema: "public",
		Table:  "orders",
		Columns: []ColumnInfo{
			{Name: "id", DataType: pgtype.Int8OID, IsKey: true},
			{Name: "total", DataType: pgtype.NumericOID},
			{Name: "paid", DataType: pgtype.BoolOID},
			{Name: "placed_at", DataType: pgtype.TimestamptzOID},
			{Name: "reference", DataType: pgtype.TextOID},
		},
	}
}

func tuple(values ...string) *pglogrepl.TupleData {
	columns := make([]*pglogrepl.TupleDataColumn, len(values))
	for i, v := range values {
		switch v {
		case "\x00null":
			columns[i] = &pglogrepl.TupleDataColumn{DataType: 'n'}
		case "\x00toast":
			columns[i] = &pglogrepl.TupleDataColumn{DataType: 'u'}
		default:
			columns[i] = &pglogrepl.TupleDataColumn{DataType: 't', Data: []byte(v)}
		}
	}
	return &pglogrepl.TupleData{Columns: columns}
}

func TestAColumnArrivesAsTheTypeItIs(t *testing.T) {
	// Postgres sends every value as text on the wire; what a flow gets is
	// decided here.
	row := decodeTuple(relation(), tuple("42", "199.95", "t", "2026-08-15 09:30:00+00", "ORD-1"))

	if row["id"] != int64(42) {
		t.Errorf("id = %#v, want a number", row["id"])
	}
	if row["total"] != 199.95 {
		t.Errorf("total = %#v, want a number", row["total"])
	}
	if row["paid"] != true {
		t.Errorf("paid = %#v, want a boolean", row["paid"])
	}
	// A timestamp a flow can compare and a client can parse, rather than
	// whichever layout the server happened to send.
	placed, ok := row["placed_at"].(string)
	if !ok || !strings.Contains(placed, "T") {
		t.Errorf("placed_at = %#v, want RFC 3339", row["placed_at"])
	}
	if row["reference"] != "ORD-1" {
		t.Errorf("reference = %#v", row["reference"])
	}
}

func TestABooleanIsWhicheverWayPostgresWroteIt(t *testing.T) {
	for text, want := range map[string]bool{
		"t": true, "true": true, "TRUE": true,
		"f": false, "false": false, "": false,
	} {
		if got := decodeColumnValue(pgtype.BoolOID, []byte(text)); got != want {
			t.Errorf("%q decoded to %v, want %v", text, got, want)
		}
	}
}

func TestAValueThatIsNotWhatTheColumnSaysIsKeptAsText(t *testing.T) {
	// Rather than dropped or turned into a zero, which a flow reads as a real
	// value of zero.
	if got := decodeColumnValue(pgtype.Int8OID, []byte("not a number")); got != "not a number" {
		t.Errorf("got %#v, want the text kept", got)
	}
	if got := decodeColumnValue(pgtype.TimestamptzOID, []byte("whenever")); got != "whenever" {
		t.Errorf("got %#v, want the text kept", got)
	}
	// A type nothing special is done with travels as text.
	if got := decodeColumnValue(pgtype.JSONBOID, []byte(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("got %#v", got)
	}
}

func TestANullAndAnUnchangedLargeValueBothArriveAsNothing(t *testing.T) {
	// The second is the one worth knowing about: Postgres sends "unchanged"
	// for a large column an update did not touch, and reading it as a value
	// would blank the column downstream.
	row := decodeTuple(relation(), tuple("1", "\x00null", "\x00toast", "\x00null", "ORD-1"))

	if row["total"] != nil {
		t.Errorf("a null arrived as %#v", row["total"])
	}
	if row["paid"] != nil {
		t.Errorf("an untouched large value arrived as %#v", row["paid"])
	}
	if row["id"] != int64(1) || row["reference"] != "ORD-1" {
		t.Errorf("the columns that were sent were lost: %v", row)
	}
}

func TestATupleWithNoRowIsNothing(t *testing.T) {
	if row := decodeTuple(relation(), nil); row != nil {
		t.Errorf("row = %v, want nothing", row)
	}
}

func TestMoreColumnsThanTheRelationKnowsAreIgnored(t *testing.T) {
	// A table altered while the connector is running: the relation it has is
	// the old one until the server sends a new one, and reading past its end
	// would panic.
	row := decodeTuple(relation(), tuple("1", "2", "t", "2026-08-15 09:30:00+00", "ORD-1", "a-new-column"))
	if len(row) != 5 {
		t.Errorf("%d columns decoded, want the five the relation knows", len(row))
	}
}

func TestTheRelationSaysWhichColumnsIdentifyARow(t *testing.T) {
	// The key is what a flow dedupes and correlates on, and it comes from the
	// relation message rather than from anything configured.
	p := NewPostgresListener(&Config{Database: "orders"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	p.handleRelation(&pglogrepl.RelationMessage{
		RelationID:   42,
		Namespace:    "public",
		RelationName: "orders",
		Columns: []*pglogrepl.RelationMessageColumn{
			{Name: "id", DataType: pgtype.Int8OID, Flags: 1},
			{Name: "reference", DataType: pgtype.TextOID, Flags: 0},
		},
	})

	rel, ok := p.relations[42]
	if !ok {
		t.Fatal("the relation was not stored, so its rows cannot be decoded")
	}
	if rel.Schema != "public" || rel.Table != "orders" {
		t.Errorf("relation = %+v", rel)
	}
	if !rel.Columns[0].IsKey {
		t.Error("the key column is not marked as one")
	}
	if rel.Columns[1].IsKey {
		t.Error("a column that is not part of the key is marked as one")
	}
}

// --- What the connector says about itself ------------------------------------

func TestTheConnectorSaysWhatItIsWatching(t *testing.T) {
	c := New("orders_cdc", &Config{
		Driver: "postgres", Host: "localhost", Port: 5432,
		Database: "orders", User: "replicator", SlotName: "mycel_orders",
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	kind, source := c.SourceInfo()
	if kind == "" || source == "" {
		t.Errorf("kind = %q, source = %q, want both", kind, source)
	}

	// Stepping through changes one at a time is what an IDE drives.
	c.SetDebugMode(true)
	c.AllowOne()
	c.SetDebugMode(false)
}

func TestAFlowNeedNotNameATableTheConnectorAlreadyWatches(t *testing.T) {
	c := New("orders_cdc", &Config{Driver: "postgres"}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	params := map[string]interface{}{}
	if err := c.ValidateSourceParams(params); err != nil {
		t.Fatalf("a flow naming no table was refused: %v", err)
	}
	if params["operation"] != "*" {
		t.Errorf("operation = %v, want the catch-all", params["operation"])
	}
}

func TestTheConnectionAskedForIsAReplicationOne(t *testing.T) {
	// Without replication=database the server refuses the slot, and the error
	// names neither the setting nor the connector.
	p := NewPostgresListener(&Config{
		Host: "db.example.com", Port: 5433, Database: "orders",
		User: "replicator", Password: "secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	conn := p.connString()
	for _, want := range []string{
		"host=db.example.com", "port=5433", "dbname=orders",
		"user=replicator", "replication=database",
	} {
		if !strings.Contains(conn, want) {
			t.Errorf("the connection string does not carry %q: %s", want, conn)
		}
	}

	// A mode nobody configured still connects, rather than refusing over a
	// setting most deployments never write.
	if !strings.Contains(conn, "sslmode=prefer") {
		t.Errorf("sslmode = %s, want a default", conn)
	}

	secure := NewPostgresListener(&Config{SSLMode: "require"}, nil).connString()
	if !strings.Contains(secure, "sslmode=require") {
		t.Errorf("the configured mode was not used: %s", secure)
	}
}

func TestWhatEachKindOfChangeBecomes(t *testing.T) {
	// A flow reads input.trigger to tell an insert from a delete, and reads
	// old and new to see what changed. Getting either wrong is a flow acting
	// on the row that was there before.
	p := NewPostgresListener(&Config{Database: "orders"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.relations[42] = relation()

	insert, err := p.eventFor(&pglogrepl.InsertMessage{
		RelationID: 42,
		Tuple:      tuple("1", "199.95", "t", "2026-08-15 09:30:00+00", "ORD-1"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if insert.Trigger != "INSERT" || insert.Table != "orders" || insert.Schema != "public" {
		t.Errorf("event = %+v", insert)
	}
	if insert.New["reference"] != "ORD-1" {
		t.Errorf("the new row = %v", insert.New)
	}
	if insert.Old != nil {
		t.Error("an insert carries a previous row")
	}

	update, err := p.eventFor(&pglogrepl.UpdateMessage{
		RelationID: 42,
		OldTuple:   tuple("1", "199.95", "f", "2026-08-15 09:30:00+00", "ORD-1"),
		NewTuple:   tuple("1", "199.95", "t", "2026-08-15 09:30:00+00", "ORD-1"),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if update.Trigger != "UPDATE" {
		t.Errorf("trigger = %q", update.Trigger)
	}
	// Both sides, which is the only way a flow can tell what changed.
	if update.Old["paid"] != false || update.New["paid"] != true {
		t.Errorf("old = %v, new = %v", update.Old, update.New)
	}

	remove, err := p.eventFor(&pglogrepl.DeleteMessage{
		RelationID: 42,
		OldTuple:   tuple("1", "199.95", "t", "2026-08-15 09:30:00+00", "ORD-1"),
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if remove.Trigger != "DELETE" {
		t.Errorf("trigger = %q", remove.Trigger)
	}
	// The row that is gone is the only thing a flow has left to act on.
	if remove.Old["reference"] != "ORD-1" {
		t.Errorf("the removed row = %v", remove.Old)
	}
	if remove.New != nil {
		t.Error("a delete carries a new row")
	}
}

func TestAChangeToATableTheConnectorHasNotSeenIsReported(t *testing.T) {
	// The server sends the relation before the rows; a row for one it never
	// sent cannot be decoded, and guessing the columns would produce a row
	// with the wrong names.
	p := NewPostgresListener(&Config{Database: "orders"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := p.eventFor(&pglogrepl.InsertMessage{RelationID: 99, Tuple: tuple("1")}); err == nil {
		t.Error("a row for an unknown table was decoded anyway")
	}
}

func TestTransactionBoundariesAreNotChanges(t *testing.T) {
	// A begin and a commit wrap every change; a flow that ran for them would
	// fire twice per row.
	p := NewPostgresListener(&Config{Database: "orders"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for name, message := range map[string]pglogrepl.Message{
		"a transaction opening": &pglogrepl.BeginMessage{},
		"one committing":        &pglogrepl.CommitMessage{},
		"a table truncated":     &pglogrepl.TruncateMessage{},
		"a type definition":     &pglogrepl.TypeMessage{},
	} {
		t.Run(name, func(t *testing.T) {
			event, err := p.eventFor(message)
			if err != nil {
				t.Fatalf("eventFor: %v", err)
			}
			if event != nil {
				t.Errorf("event = %+v, want none", event)
			}
		})
	}
}
