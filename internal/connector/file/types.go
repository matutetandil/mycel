// Package file provides a file system connector for reading and writing files.
package file

import (
	"time"
)

// Config holds configuration for a file connector.
type Config struct {
	// BasePath is the base directory for file operations.
	// All paths are relative to this directory.
	BasePath string

	// Format specifies the default file format (json, csv, excel, text, binary).
	Format string

	// Watch enables file watching for changes.
	Watch bool

	// WatchInterval is the interval for polling file changes.
	WatchInterval time.Duration

	// Permissions sets the default file permissions for new files.
	Permissions uint32

	// CreateDirs automatically creates directories if they don't exist.
	CreateDirs bool

	// CSV holds default CSV/TSV options applied when reading/writing CSV files.
	// These can be overridden per-operation via params.
	CSV CSVOptions
}

// FileInfo represents metadata about a file.
type FileInfo struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
	Mode    string    `json:"mode"`
}

// CSVOptions configures CSV/TSV reading and writing behavior.
type CSVOptions struct {
	// Delimiter is the field separator character (default: comma).
	// Common values: "," (CSV), "\t" (TSV), ";" (European CSV), "|" (pipe-delimited).
	Delimiter rune

	// Comment is the character that marks lines as comments (e.g., '#').
	// Lines starting with this character are skipped during reading.
	Comment rune

	// SkipRows is the number of rows to skip before reading data.
	// Useful for files with metadata rows before the header.
	SkipRows int

	// NoHeader indicates there is no header row. Columns will be named
	// "column_1", "column_2", etc. unless Columns is specified.
	NoHeader bool

	// Columns specifies explicit column names. When set, these override
	// the header row (or provide names when NoHeader is true).
	Columns []string

	// TrimSpace removes leading/trailing whitespace from field values.
	TrimSpace bool
}

// ReadOptions configures how files are read.
type ReadOptions struct {
	Format   string // json, csv, excel, text, binary, lines
	Encoding string // utf-8, latin1, etc.
	Offset   int64  // Start reading from this byte offset
	Limit    int64  // Maximum bytes to read (0 = unlimited)
}

// WriteOptions configures how files are written.
type WriteOptions struct {
	Format   string // json, csv, excel, text, binary
	Append   bool   // Append to existing file instead of overwriting
	Encoding string // utf-8, latin1, etc.
}
