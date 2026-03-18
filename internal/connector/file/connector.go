package file

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/xuri/excelize/v2"
)

// HandlerFunc is a function that handles file watch events.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// fileState tracks the last known state of a file for change detection.
type fileState struct {
	modTime time.Time
	size    int64
}

// Connector provides file system operations.
type Connector struct {
	name   string
	config *Config

	// Watch mode fields
	handlers map[string]HandlerFunc // glob pattern → handler
	known    map[string]fileState   // relative path → last known state
	cancel   context.CancelFunc     // stops the watcher
	started  bool
	logger   *slog.Logger

	mu sync.RWMutex

	// Debug throttling: single-file processing when debugger is connected
	debugGate connector.DebugGate
}

// New creates a new file connector.
func New(name string, config *Config) *Connector {
	if config.Format == "" {
		config.Format = "json"
	}
	if config.Permissions == 0 {
		config.Permissions = 0644
	}
	if config.Watch && config.WatchInterval == 0 {
		config.WatchInterval = 5 * time.Second
	}

	return &Connector{
		name:     name,
		config:   config,
		handlers: make(map[string]HandlerFunc),
		known:    make(map[string]fileState),
		logger:   slog.Default(),
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "file"
}

// Connect validates the configuration.
func (c *Connector) Connect(ctx context.Context) error {
	// Verify base path exists if specified
	if c.config.BasePath != "" {
		info, err := os.Stat(c.config.BasePath)
		if err != nil {
			if os.IsNotExist(err) && c.config.CreateDirs {
				return os.MkdirAll(c.config.BasePath, 0755)
			}
			return fmt.Errorf("base path error: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("base path is not a directory: %s", c.config.BasePath)
		}
	}
	return nil
}

// Close stops the file watcher if running.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.started = false
	return nil
}

// SetDebugMode enables or disables single-file debug throttling.
func (c *Connector) SetDebugMode(enabled bool) {
	c.debugGate.SetEnabled(enabled)
	if enabled {
		c.logger.Info("debug mode enabled: single-file processing", "name", c.name)
	} else {
		c.logger.Info("debug mode disabled: concurrent processing restored", "name", c.name)
	}
}

// RegisterRoute registers a handler for a file watch pattern (e.g., "*.csv").
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.handlers[operation]; ok {
		c.handlers[operation] = HandlerFunc(connector.ChainEventDriven(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "operation", operation)
	} else {
		c.handlers[operation] = handler
	}
}

// Start begins the file watcher polling loop if watch mode is enabled.
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	if !c.config.Watch || len(c.handlers) == 0 {
		c.mu.Unlock()
		return nil
	}

	if c.started {
		c.mu.Unlock()
		return nil
	}

	watchCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.started = true
	c.mu.Unlock()

	// Seed known files before starting the poll loop so that
	// files created after Start() returns are properly detected as new.
	c.seedKnown()

	go c.pollLoop(watchCtx)

	c.logger.Info("file watcher started",
		"connector", c.name,
		"path", c.config.BasePath,
		"interval", c.config.WatchInterval,
		"patterns", len(c.handlers),
	)

	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if c.config.BasePath != "" {
		_, err := os.Stat(c.config.BasePath)
		return err
	}
	return nil
}

// Read reads from a file.
func (c *Connector) Read(ctx context.Context, query *connector.Query) ([]map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.resolvePath(query.Target)
	format := getParamString(query.Params, "format", "")
	if format == "" {
		format = c.detectFormat(path)
	}

	// Check if it's a directory listing request
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}

	if info.IsDir() {
		return c.listDirectory(path)
	}

	return c.readFile(path, format, query.Params)
}

// Write writes data to a file.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (map[string]interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := c.resolvePath(data.Target)
	format := getParamString(data.Params, "format", "")
	if format == "" {
		format = c.detectFormat(path)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if c.config.CreateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Determine write mode
	flags := os.O_WRONLY | os.O_CREATE
	appendMode := getParamBool(data.Params, "append", false)
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(path, flags, fs.FileMode(c.config.Permissions))
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Use Payload for content
	var content interface{}
	if data.Payload != nil {
		content = data.Payload
	} else {
		content = data.Params["content"]
	}

	bytesWritten, err := c.writeData(file, content, format, data.Params)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"path":    path,
		"written": bytesWritten,
	}, nil
}

// Call executes a file operation.
func (c *Connector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	switch operation {
	case "read":
		path, _ := params["path"].(string)
		format, _ := params["format"].(string)
		return c.Read(ctx, &connector.Query{
			Target: path,
			Params: map[string]interface{}{
				"format": format,
			},
		})

	case "write":
		path, _ := params["path"].(string)
		format, _ := params["format"].(string)
		append_, _ := params["append"].(bool)
		return c.Write(ctx, &connector.Data{
			Target: path,
			Params: map[string]interface{}{
				"content": params["content"],
				"format":  format,
				"append":  append_,
			},
		})

	case "delete":
		path, _ := params["path"].(string)
		return c.deleteFile(path)

	case "exists":
		path, _ := params["path"].(string)
		return c.fileExists(path)

	case "stat":
		path, _ := params["path"].(string)
		return c.fileStat(path)

	case "list":
		path, _ := params["path"].(string)
		return c.listDirectory(c.resolvePath(path))

	case "copy":
		src, _ := params["source"].(string)
		dst, _ := params["destination"].(string)
		return c.copyFile(src, dst)

	case "move":
		src, _ := params["source"].(string)
		dst, _ := params["destination"].(string)
		return c.moveFile(src, dst)

	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

// resolvePath resolves a path relative to the base path with traversal protection.
// When BasePath is configured, all paths are resolved relative to it and
// validated to prevent directory traversal attacks.
func (c *Connector) resolvePath(path string) string {
	if c.config.BasePath == "" {
		return filepath.Clean(path)
	}

	// Strip any absolute prefix — always treat as relative to BasePath
	cleaned := filepath.Clean("/" + path)
	resolved := filepath.Join(c.config.BasePath, cleaned)

	// Verify the resolved path stays within BasePath
	absBase, _ := filepath.Abs(c.config.BasePath)
	absResolved, _ := filepath.Abs(resolved)

	if absResolved != absBase && !strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) {
		// Path escapes BasePath — fall back to BasePath itself
		return absBase
	}

	return absResolved
}

// detectFormat detects the file format from extension.
func (c *Connector) detectFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".csv":
		return "csv"
	case ".tsv", ".tab":
		return "tsv"
	case ".xlsx", ".xls":
		return "excel"
	case ".txt", ".log", ".md":
		return "text"
	default:
		return c.config.Format
	}
}

// readFile reads and parses a file.
func (c *Connector) readFile(path, format string, params map[string]interface{}) ([]map[string]interface{}, error) {
	// Excel needs the file path directly (not an io.Reader)
	if format == "excel" {
		sheet := getParamString(params, "sheet", "")
		return c.readExcel(path, sheet)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	switch format {
	case "json":
		return c.readJSON(file)
	case "csv":
		return c.readCSV(file, params)
	case "tsv":
		if params == nil {
			params = make(map[string]interface{})
		}
		params["delimiter"] = "\t"
		return c.readCSV(file, params)
	case "text", "lines":
		return c.readText(file, format == "lines")
	case "binary":
		return c.readBinary(file)
	default:
		return c.readJSON(file)
	}
}

// readJSON reads a JSON file.
func (c *Connector) readJSON(r io.Reader) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Try to decode as array
	var arr []map[string]interface{}
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}

	// Try to decode as single object
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return []map[string]interface{}{obj}, nil
}

// readCSV reads a CSV file with configurable options.
// Options can be set at the connector level (c.config.CSV) and overridden
// per-operation via params: delimiter, comment, skip_rows, no_header, columns, trim_space.
func (c *Connector) readCSV(r io.Reader, params map[string]interface{}) ([]map[string]interface{}, error) {
	opts := c.resolveCSVOptions(params)

	// Strip UTF-8 BOM if present
	br := newBOMReader(r)

	reader := csv.NewReader(br)
	if opts.Delimiter != 0 {
		reader.Comma = opts.Delimiter
	}
	if opts.Comment != 0 {
		reader.Comment = opts.Comment
	}
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // allow variable field counts

	// Skip leading rows (metadata, blank lines before header)
	for i := 0; i < opts.SkipRows; i++ {
		if _, err := reader.Read(); err != nil {
			return nil, fmt.Errorf("failed to skip row %d: %w", i+1, err)
		}
	}

	// Determine headers
	var headers []string
	if len(opts.Columns) > 0 {
		headers = opts.Columns
		// If there IS a header row but we're overriding names, consume it
		if !opts.NoHeader {
			if _, err := reader.Read(); err != nil {
				return nil, fmt.Errorf("failed to read CSV header: %w", err)
			}
		}
	} else if opts.NoHeader {
		// Read first data row to determine column count, then use it
		firstRow, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to read first CSV row: %w", err)
		}
		headers = make([]string, len(firstRow))
		for i := range firstRow {
			headers[i] = fmt.Sprintf("column_%d", i+1)
		}
		// Process this first row as data
		row := make(map[string]interface{})
		for i, header := range headers {
			val := firstRow[i]
			if opts.TrimSpace {
				val = strings.TrimSpace(val)
			}
			row[header] = val
		}
		var results []map[string]interface{}
		results = append(results, row)
		// Continue reading remaining rows
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("failed to read CSV record: %w", err)
			}
			row := make(map[string]interface{})
			for i, header := range headers {
				if i < len(record) {
					val := record[i]
					if opts.TrimSpace {
						val = strings.TrimSpace(val)
					}
					row[header] = val
				}
			}
			results = append(results, row)
		}
		return results, nil
	} else {
		// Read header row
		var err error
		headers, err = reader.Read()
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV header: %w", err)
		}
		if opts.TrimSpace {
			for i := range headers {
				headers[i] = strings.TrimSpace(headers[i])
			}
		}
	}

	// Read data records
	var results []map[string]interface{}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV record: %w", err)
		}

		row := make(map[string]interface{})
		for i, header := range headers {
			if i < len(record) {
				val := record[i]
				if opts.TrimSpace {
					val = strings.TrimSpace(val)
				}
				row[header] = val
			}
		}
		results = append(results, row)
	}

	return results, nil
}

// resolveCSVOptions merges connector-level CSV defaults with per-operation params.
func (c *Connector) resolveCSVOptions(params map[string]interface{}) CSVOptions {
	opts := c.config.CSV

	if params == nil {
		return opts
	}

	// Delimiter: string param → rune
	if d := getParamString(params, "delimiter", ""); d != "" {
		switch d {
		case "\\t", "\t", "tab":
			opts.Delimiter = '\t'
		case ";", "semicolon":
			opts.Delimiter = ';'
		case "|", "pipe":
			opts.Delimiter = '|'
		default:
			if len(d) > 0 {
				opts.Delimiter = rune(d[0])
			}
		}
	}

	if cm := getParamString(params, "comment", ""); cm != "" && len(cm) > 0 {
		opts.Comment = rune(cm[0])
	}

	if sr := getParamInt(params, "skip_rows", 0); sr > 0 {
		opts.SkipRows = sr
	}

	if getParamBool(params, "no_header", false) {
		opts.NoHeader = true
	}

	if getParamBool(params, "trim_space", false) {
		opts.TrimSpace = true
	}

	if cols, ok := params["columns"]; ok {
		switch v := cols.(type) {
		case []string:
			opts.Columns = v
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					opts.Columns = append(opts.Columns, s)
				}
			}
		}
	}

	return opts
}

// readText reads a text file.
func (c *Connector) readText(r io.Reader, asLines bool) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	content := string(data)

	if asLines {
		lines := strings.Split(content, "\n")
		results := make([]map[string]interface{}, 0, len(lines))
		for i, line := range lines {
			results = append(results, map[string]interface{}{
				"line":    i + 1,
				"content": line,
			})
		}
		return results, nil
	}

	return []map[string]interface{}{
		{"content": content},
	}, nil
}

// readBinary reads a binary file.
func (c *Connector) readBinary(r io.Reader) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return []map[string]interface{}{
		{
			"data": data,
			"size": len(data),
		},
	}, nil
}

// readExcel reads an Excel (.xlsx) file.
func (c *Connector) readExcel(path, sheet string) ([]map[string]interface{}, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel file: %w", err)
	}
	defer f.Close()

	// Use specified sheet or default to the first one
	if sheet == "" {
		sheet = f.GetSheetName(0)
		if sheet == "" {
			return nil, fmt.Errorf("Excel file has no sheets")
		}
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("failed to read sheet %q: %w", sheet, err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	// First row = headers
	headers := rows[0]

	var results []map[string]interface{}
	for _, row := range rows[1:] {
		// Skip completely empty rows
		empty := true
		for _, cell := range row {
			if cell != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}

		record := make(map[string]interface{})
		for i, header := range headers {
			if i < len(row) {
				record[header] = row[i]
			} else {
				record[header] = ""
			}
		}
		results = append(results, record)
	}

	return results, nil
}

// writeData writes data to a file.
func (c *Connector) writeData(w io.Writer, data interface{}, format string, params map[string]interface{}) (int64, error) {
	switch format {
	case "json":
		return c.writeJSON(w, data)
	case "csv":
		return c.writeCSV(w, data, params)
	case "tsv":
		if params == nil {
			params = make(map[string]interface{})
		}
		params["delimiter"] = "\t"
		return c.writeCSV(w, data, params)
	case "excel":
		return c.writeExcel(w, data)
	case "text":
		return c.writeText(w, data)
	case "binary":
		return c.writeBinary(w, data)
	default:
		return c.writeJSON(w, data)
	}
}

// writeJSON writes data as JSON.
func (c *Connector) writeJSON(w io.Writer, data interface{}) (int64, error) {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	n, err := w.Write(jsonData)
	return int64(n), err
}

// writeCSV writes data as CSV with configurable options.
func (c *Connector) writeCSV(w io.Writer, data interface{}, params map[string]interface{}) (int64, error) {
	opts := c.resolveCSVOptions(params)
	writer := csv.NewWriter(w)
	if opts.Delimiter != 0 {
		writer.Comma = opts.Delimiter
	}

	// Convert data to rows
	var rows []map[string]interface{}
	switch v := data.(type) {
	case []map[string]interface{}:
		rows = v
	case map[string]interface{}:
		rows = []map[string]interface{}{v}
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				rows = append(rows, m)
			}
		}
	default:
		return 0, fmt.Errorf("cannot convert %T to CSV", data)
	}

	if len(rows) == 0 {
		return 0, nil
	}

	// Determine headers: explicit columns > first row keys (sorted)
	var headers []string
	if len(opts.Columns) > 0 {
		headers = opts.Columns
	} else {
		for key := range rows[0] {
			headers = append(headers, key)
		}
		sort.Strings(headers)
	}

	// Write header row (unless no_header is set)
	if !opts.NoHeader {
		if err := writer.Write(headers); err != nil {
			return 0, err
		}
	}

	// Write data rows
	for _, row := range rows {
		record := make([]string, len(headers))
		for i, header := range headers {
			if val, ok := row[header]; ok {
				record[i] = fmt.Sprintf("%v", val)
			}
		}
		if err := writer.Write(record); err != nil {
			return 0, err
		}
	}

	writer.Flush()
	return 0, writer.Error()
}

// writeExcel writes data as an Excel (.xlsx) file.
func (c *Connector) writeExcel(w io.Writer, data interface{}) (int64, error) {
	// Convert data to rows
	var rows []map[string]interface{}
	switch v := data.(type) {
	case []map[string]interface{}:
		rows = v
	case map[string]interface{}:
		rows = []map[string]interface{}{v}
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				rows = append(rows, m)
			}
		}
	default:
		return 0, fmt.Errorf("cannot convert %T to Excel", data)
	}

	if len(rows) == 0 {
		return 0, nil
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"

	// Extract and sort headers for deterministic column order
	var headers []string
	for key := range rows[0] {
		headers = append(headers, key)
	}
	sort.Strings(headers)

	// Write headers
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	// Write data rows
	for rowIdx, row := range rows {
		for colIdx, header := range headers {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			if val, ok := row[header]; ok {
				f.SetCellValue(sheet, cell, val)
			}
		}
	}

	n, err := f.WriteTo(w)
	return n, err
}

// writeText writes data as text.
func (c *Connector) writeText(w io.Writer, data interface{}) (int64, error) {
	var content string
	switch v := data.(type) {
	case string:
		content = v
	case []byte:
		content = string(v)
	case map[string]interface{}:
		if c, ok := v["content"].(string); ok {
			content = c
		} else {
			jsonData, _ := json.Marshal(v)
			content = string(jsonData)
		}
	default:
		content = fmt.Sprintf("%v", data)
	}

	n, err := w.Write([]byte(content))
	return int64(n), err
}

// writeBinary writes binary data.
func (c *Connector) writeBinary(w io.Writer, data interface{}) (int64, error) {
	var bytes []byte
	switch v := data.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	case map[string]interface{}:
		if b, ok := v["data"].([]byte); ok {
			bytes = b
		}
	default:
		return 0, fmt.Errorf("cannot write %T as binary", data)
	}

	n, err := w.Write(bytes)
	return int64(n), err
}

// listDirectory lists files in a directory.
func (c *Connector) listDirectory(path string) ([]map[string]interface{}, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var results []map[string]interface{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		results = append(results, map[string]interface{}{
			"name":     entry.Name(),
			"path":     filepath.Join(path, entry.Name()),
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
			"mod_time": info.ModTime(),
			"mode":     info.Mode().String(),
		})
	}

	return results, nil
}

// deleteFile deletes a file or directory.
func (c *Connector) deleteFile(path string) (map[string]interface{}, error) {
	fullPath := c.resolvePath(path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		err = os.RemoveAll(fullPath)
	} else {
		err = os.Remove(fullPath)
	}

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"deleted": true,
		"path":    fullPath,
	}, nil
}

// fileExists checks if a file exists.
func (c *Connector) fileExists(path string) (map[string]interface{}, error) {
	fullPath := c.resolvePath(path)
	_, err := os.Stat(fullPath)

	return map[string]interface{}{
		"exists": err == nil,
		"path":   fullPath,
	}, nil
}

// fileStat returns file information.
func (c *Connector) fileStat(path string) (map[string]interface{}, error) {
	fullPath := c.resolvePath(path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"name":     info.Name(),
		"path":     fullPath,
		"size":     info.Size(),
		"is_dir":   info.IsDir(),
		"mod_time": info.ModTime(),
		"mode":     info.Mode().String(),
	}, nil
}

// copyFile copies a file.
func (c *Connector) copyFile(src, dst string) (map[string]interface{}, error) {
	srcPath := c.resolvePath(src)
	dstPath := c.resolvePath(dst)

	source, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer source.Close()

	// Create destination directory if needed
	if c.config.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return nil, err
		}
	}

	destination, err := os.Create(dstPath)
	if err != nil {
		return nil, err
	}
	defer destination.Close()

	nBytes, err := io.Copy(destination, source)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"copied": true,
		"source": srcPath,
		"dest":   dstPath,
		"bytes":  nBytes,
	}, nil
}

// moveFile moves a file.
func (c *Connector) moveFile(src, dst string) (map[string]interface{}, error) {
	srcPath := c.resolvePath(src)
	dstPath := c.resolvePath(dst)

	// Create destination directory if needed
	if c.config.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return nil, err
		}
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"moved":  true,
		"source": srcPath,
		"dest":   dstPath,
	}, nil
}

// Helper functions for extracting parameters

func getParamString(params map[string]interface{}, key, defaultVal string) string {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getParamBool(params map[string]interface{}, key string, defaultVal bool) bool {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getParamInt(params map[string]interface{}, key string, defaultVal int) int {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return defaultVal
}

// bomReader wraps an io.Reader and strips a leading UTF-8 BOM (byte order mark)
// if present. This is common in CSV files exported from Excel on Windows.
type bomReader struct {
	r       io.Reader
	checked bool
	buf     []byte
}

func newBOMReader(r io.Reader) io.Reader {
	return &bomReader{r: r}
}

func (b *bomReader) Read(p []byte) (int, error) {
	if !b.checked {
		b.checked = true
		// Read up to 3 bytes to check for BOM
		bom := make([]byte, 3)
		n, err := io.ReadFull(b.r, bom)
		if n > 0 {
			// UTF-8 BOM: EF BB BF
			if n >= 3 && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
				// BOM detected and stripped
			} else {
				b.buf = bom[:n]
			}
		}
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return 0, err
		}
	}

	// Drain buffered bytes first
	if len(b.buf) > 0 {
		n := copy(p, b.buf)
		b.buf = b.buf[n:]
		if len(p) > n {
			m, err := b.r.Read(p[n:])
			return n + m, err
		}
		return n, nil
	}

	return b.r.Read(p)
}
