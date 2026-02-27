package cdc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresListener implements the Listener interface using PostgreSQL
// logical replication via the pgoutput plugin.
type PostgresListener struct {
	config    *Config
	conn      *pgconn.PgConn
	relations map[uint32]*RelationInfo
	logger    *slog.Logger
	mu        sync.Mutex
	closed    bool
}

// RelationInfo stores table schema information received from the replication stream.
type RelationInfo struct {
	Schema  string
	Table   string
	Columns []ColumnInfo
}

// ColumnInfo describes a single column in a relation.
type ColumnInfo struct {
	Name     string
	DataType uint32
	IsKey    bool
}

// NewPostgresListener creates a new PostgreSQL CDC listener.
func NewPostgresListener(config *Config, logger *slog.Logger) *PostgresListener {
	if logger == nil {
		logger = slog.Default()
	}
	return &PostgresListener{
		config:    config,
		relations: make(map[uint32]*RelationInfo),
		logger:    logger,
	}
}

// connString builds a PostgreSQL connection string with replication mode.
func (p *PostgresListener) connString() string {
	sslMode := p.config.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s replication=database",
		p.config.Host, p.config.Port, p.config.Database, p.config.User, p.config.Password, sslMode,
	)
}

// Start connects to PostgreSQL, sets up logical replication, and streams events.
func (p *PostgresListener) Start(ctx context.Context, eventCh chan<- *Event) error {
	conn, err := pgconn.Connect(ctx, p.connString())
	if err != nil {
		return fmt.Errorf("cdc: connect failed: %w", err)
	}
	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()

	// Ensure publication exists
	if err := p.ensurePublication(ctx); err != nil {
		conn.Close(ctx)
		return fmt.Errorf("cdc: ensure publication: %w", err)
	}

	// Ensure replication slot exists
	startLSN, err := p.ensureReplicationSlot(ctx)
	if err != nil {
		conn.Close(ctx)
		return fmt.Errorf("cdc: ensure slot: %w", err)
	}

	// Start replication
	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", p.config.Publication),
	}
	err = pglogrepl.StartReplication(ctx, conn, p.config.SlotName, startLSN, pglogrepl.StartReplicationOptions{
		PluginArgs: pluginArgs,
	})
	if err != nil {
		conn.Close(ctx)
		return fmt.Errorf("cdc: start replication: %w", err)
	}

	p.logger.Info("CDC replication started",
		"slot", p.config.SlotName,
		"publication", p.config.Publication,
		"start_lsn", startLSN,
	)

	return p.streamLoop(ctx, conn, eventCh, startLSN)
}

// ensurePublication creates the publication if it doesn't exist.
func (p *PostgresListener) ensurePublication(ctx context.Context) error {
	sql := fmt.Sprintf(
		"SELECT 1 FROM pg_publication WHERE pubname = '%s'",
		p.config.Publication,
	)
	result := p.conn.Exec(ctx, sql)
	_, err := result.ReadAll()
	if err != nil {
		return err
	}

	// Try to create - will fail silently if exists
	createSQL := fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", p.config.Publication)
	result = p.conn.Exec(ctx, createSQL)
	_, _ = result.ReadAll() // ignore error (publication may already exist)
	return nil
}

// ensureReplicationSlot creates the slot if it doesn't exist and returns the start LSN.
func (p *PostgresListener) ensureReplicationSlot(ctx context.Context) (pglogrepl.LSN, error) {
	// Try to create the slot
	res, err := pglogrepl.CreateReplicationSlot(ctx, p.conn, p.config.SlotName, "pgoutput",
		pglogrepl.CreateReplicationSlotOptions{Temporary: false},
	)
	if err != nil {
		// Slot may already exist - get the confirmed flush LSN
		sysID, err2 := pglogrepl.IdentifySystem(ctx, p.conn)
		if err2 != nil {
			return 0, fmt.Errorf("identify system: %w (original: %v)", err2, err)
		}
		return sysID.XLogPos, nil
	}

	lsn, err := pglogrepl.ParseLSN(res.ConsistentPoint)
	if err != nil {
		return 0, fmt.Errorf("parse LSN: %w", err)
	}
	return lsn, nil
}

// streamLoop is the main replication message loop.
func (p *PostgresListener) streamLoop(ctx context.Context, conn *pgconn.PgConn, eventCh chan<- *Event, startLSN pglogrepl.LSN) error {
	clientXLogPos := startLSN
	standbyTicker := time.NewTicker(10 * time.Second)
	defer standbyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-standbyTicker.C:
			err := pglogrepl.SendStandbyStatusUpdate(ctx, conn, pglogrepl.StandbyStatusUpdate{
				WALWritePosition: clientXLogPos,
			})
			if err != nil {
				return fmt.Errorf("send standby status: %w", err)
			}
		default:
		}

		// Receive with a short timeout so we can check context/send standby
		recvCtx, cancel := context.WithDeadline(ctx, time.Now().Add(1*time.Second))
		rawMsg, err := conn.ReceiveMessage(recvCtx)
		cancel()

		if err != nil {
			if pgconn.Timeout(err) {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("receive message: %w", err)
		}

		if errMsg, ok := rawMsg.(*pgproto3.ErrorResponse); ok {
			return fmt.Errorf("postgres error: %s: %s", errMsg.Severity, errMsg.Message)
		}

		msg, ok := rawMsg.(*pgproto3.CopyData)
		if !ok {
			continue
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("parse keepalive: %w", err)
			}
			if pkm.ReplyRequested {
				err = pglogrepl.SendStandbyStatusUpdate(ctx, conn, pglogrepl.StandbyStatusUpdate{
					WALWritePosition: clientXLogPos,
				})
				if err != nil {
					return fmt.Errorf("send standby status: %w", err)
				}
			}

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("parse xlog data: %w", err)
			}

			event, err := p.processWALMessage(xld.WALData)
			if err != nil {
				p.logger.Error("CDC WAL processing error", "error", err)
				continue
			}
			if event != nil {
				select {
				case eventCh <- event:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			clientXLogPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))
		}
	}
}

// processWALMessage parses a WAL message and returns an Event (or nil for non-data messages).
func (p *PostgresListener) processWALMessage(walData []byte) (*Event, error) {
	logicalMsg, err := pglogrepl.Parse(walData)
	if err != nil {
		return nil, fmt.Errorf("parse WAL: %w", err)
	}

	switch m := logicalMsg.(type) {
	case *pglogrepl.RelationMessage:
		p.handleRelation(m)
		return nil, nil

	case *pglogrepl.InsertMessage:
		rel, ok := p.relations[m.RelationID]
		if !ok {
			return nil, fmt.Errorf("unknown relation ID %d", m.RelationID)
		}
		return &Event{
			Trigger:   "INSERT",
			Schema:    rel.Schema,
			Table:     rel.Table,
			New:       decodeTuple(rel, m.Tuple),
			Timestamp: time.Now(),
		}, nil

	case *pglogrepl.UpdateMessage:
		rel, ok := p.relations[m.RelationID]
		if !ok {
			return nil, fmt.Errorf("unknown relation ID %d", m.RelationID)
		}
		event := &Event{
			Trigger:   "UPDATE",
			Schema:    rel.Schema,
			Table:     rel.Table,
			New:       decodeTuple(rel, m.NewTuple),
			Timestamp: time.Now(),
		}
		if m.OldTuple != nil {
			event.Old = decodeTuple(rel, m.OldTuple)
		}
		return event, nil

	case *pglogrepl.DeleteMessage:
		rel, ok := p.relations[m.RelationID]
		if !ok {
			return nil, fmt.Errorf("unknown relation ID %d", m.RelationID)
		}
		return &Event{
			Trigger:   "DELETE",
			Schema:    rel.Schema,
			Table:     rel.Table,
			Old:       decodeTuple(rel, m.OldTuple),
			Timestamp: time.Now(),
		}, nil

	case *pglogrepl.BeginMessage, *pglogrepl.CommitMessage, *pglogrepl.TruncateMessage, *pglogrepl.TypeMessage, *pglogrepl.OriginMessage:
		// Transaction boundaries and metadata - no event needed
		return nil, nil
	}

	return nil, nil
}

// handleRelation stores relation metadata for later tuple decoding.
func (p *PostgresListener) handleRelation(m *pglogrepl.RelationMessage) {
	columns := make([]ColumnInfo, len(m.Columns))
	for i, col := range m.Columns {
		columns[i] = ColumnInfo{
			Name:     col.Name,
			DataType: col.DataType,
			IsKey:    col.Flags == 1,
		}
	}
	p.relations[m.RelationID] = &RelationInfo{
		Schema:  m.Namespace,
		Table:   m.RelationName,
		Columns: columns,
	}
}

// decodeTuple converts a pgoutput TupleData into a map using the relation's column metadata.
func decodeTuple(rel *RelationInfo, tuple *pglogrepl.TupleData) map[string]interface{} {
	if tuple == nil {
		return nil
	}

	result := make(map[string]interface{}, len(tuple.Columns))
	for i, col := range tuple.Columns {
		if i >= len(rel.Columns) {
			break
		}
		colInfo := rel.Columns[i]

		switch col.DataType {
		case 'n': // null
			result[colInfo.Name] = nil
		case 'u': // unchanged TOAST
			result[colInfo.Name] = nil
		case 't': // text
			result[colInfo.Name] = decodeColumnValue(colInfo.DataType, col.Data)
		}
	}
	return result
}

// decodeColumnValue converts a text-format column value to a Go type.
func decodeColumnValue(dataType uint32, data []byte) interface{} {
	text := string(data)

	switch dataType {
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID:
		var n int64
		if _, err := fmt.Sscanf(text, "%d", &n); err == nil {
			return n
		}
		return text

	case pgtype.Float4OID, pgtype.Float8OID, pgtype.NumericOID:
		var f float64
		if _, err := fmt.Sscanf(text, "%f", &f); err == nil {
			return f
		}
		return text

	case pgtype.BoolOID:
		return text == "t" || text == "true" || text == "TRUE"

	case pgtype.TimestampOID, pgtype.TimestamptzOID:
		// Try common PostgreSQL timestamp formats
		for _, layout := range []string{
			"2006-01-02 15:04:05.999999-07",
			"2006-01-02 15:04:05.999999",
			"2006-01-02 15:04:05-07",
			"2006-01-02 15:04:05",
			time.RFC3339,
		} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.Format(time.RFC3339)
			}
		}
		return text

	default:
		return text
	}
}

// Close terminates the replication connection.
func (p *PostgresListener) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.conn != nil {
		return p.conn.Close(context.Background())
	}
	return nil
}
