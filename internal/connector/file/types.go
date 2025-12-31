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

	// Format specifies the default file format (json, csv, text, binary).
	Format string

	// Watch enables file watching for changes.
	Watch bool

	// WatchInterval is the interval for polling file changes.
	WatchInterval time.Duration

	// Permissions sets the default file permissions for new files.
	Permissions uint32

	// CreateDirs automatically creates directories if they don't exist.
	CreateDirs bool
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

// ReadOptions configures how files are read.
type ReadOptions struct {
	Format   string // json, csv, text, binary, lines
	Encoding string // utf-8, latin1, etc.
	Offset   int64  // Start reading from this byte offset
	Limit    int64  // Maximum bytes to read (0 = unlimited)
}

// WriteOptions configures how files are written.
type WriteOptions struct {
	Format   string // json, csv, text, binary
	Append   bool   // Append to existing file instead of overwriting
	Encoding string // utf-8, latin1, etc.
}
