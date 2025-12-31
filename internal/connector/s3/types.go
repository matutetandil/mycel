// Package s3 provides an S3 connector for reading and writing objects.
package s3

import (
	"time"
)

// Config holds configuration for an S3 connector.
type Config struct {
	// Bucket is the S3 bucket name.
	Bucket string

	// Region is the AWS region.
	Region string

	// Endpoint is a custom S3-compatible endpoint (for MinIO, etc.).
	Endpoint string

	// AccessKey is the AWS access key ID.
	AccessKey string

	// SecretKey is the AWS secret access key.
	SecretKey string

	// SessionToken is an optional session token for temporary credentials.
	SessionToken string

	// Prefix is an optional prefix for all object keys.
	Prefix string

	// Format specifies the default file format (json, csv, text, binary).
	Format string

	// UsePathStyle enables path-style addressing (required for MinIO).
	UsePathStyle bool

	// Timeout is the timeout for S3 operations.
	Timeout time.Duration
}

// ObjectInfo represents metadata about an S3 object.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
	ContentType  string    `json:"content_type"`
	StorageClass string    `json:"storage_class"`
}

// ReadOptions configures how objects are read.
type ReadOptions struct {
	Format   string // json, csv, text, binary, lines
	Encoding string // utf-8, latin1, etc.
	Range    string // byte range (e.g., "bytes=0-1023")
}

// WriteOptions configures how objects are written.
type WriteOptions struct {
	Format       string            // json, csv, text, binary
	ContentType  string            // MIME type
	Metadata     map[string]string // Custom metadata
	StorageClass string            // STANDARD, REDUCED_REDUNDANCY, etc.
	ACL          string            // private, public-read, etc.
}
