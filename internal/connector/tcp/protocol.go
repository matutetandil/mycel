// Package tcp provides a TCP connector for Mycel.
package tcp

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	// MaxMessageSize is the maximum allowed message size (16MB)
	MaxMessageSize = 16 * 1024 * 1024

	// HeaderSize is the size of the length prefix (4 bytes)
	HeaderSize = 4
)

// Framer handles length-prefixed message framing over a connection.
type Framer struct {
	conn  net.Conn
	codec Codec
}

// NewFramer creates a new Framer with the given connection and codec.
func NewFramer(conn net.Conn, codec Codec) *Framer {
	return &Framer{
		conn:  conn,
		codec: codec,
	}
}

// SetReadDeadline sets the read deadline on the underlying connection.
func (f *Framer) SetReadDeadline(t time.Time) error {
	return f.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection.
func (f *Framer) SetWriteDeadline(t time.Time) error {
	return f.conn.SetWriteDeadline(t)
}

// ReadRaw reads a length-prefixed message and returns the raw bytes.
func (f *Framer) ReadRaw() ([]byte, error) {
	// Read the 4-byte length header
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(f.conn, header); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse length (big-endian)
	length := binary.BigEndian.Uint32(header)

	// Validate message size
	if length > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", length, MaxMessageSize)
	}

	if length == 0 {
		return []byte{}, nil
	}

	// Read the payload
	payload := make([]byte, length)
	if _, err := io.ReadFull(f.conn, payload); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	return payload, nil
}

// WriteRaw writes raw bytes with a length prefix.
func (f *Framer) WriteRaw(data []byte) error {
	length := uint32(len(data))

	// Validate message size
	if length > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes (max %d)", length, MaxMessageSize)
	}

	// Write length header
	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(header, length)

	if _, err := f.conn.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write payload
	if length > 0 {
		if _, err := f.conn.Write(data); err != nil {
			return fmt.Errorf("failed to write payload: %w", err)
		}
	}

	return nil
}

// ReadMessage reads a length-prefixed message and decodes it using the codec.
func (f *Framer) ReadMessage(v interface{}) error {
	data, err := f.ReadRaw()
	if err != nil {
		return err
	}

	return f.codec.Decode(data, v)
}

// WriteMessage encodes a message using the codec and writes it with length prefix.
func (f *Framer) WriteMessage(v interface{}) error {
	data, err := f.codec.Encode(v)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	return f.WriteRaw(data)
}

// Close closes the underlying connection.
func (f *Framer) Close() error {
	return f.conn.Close()
}

// RemoteAddr returns the remote address of the connection.
func (f *Framer) RemoteAddr() net.Addr {
	return f.conn.RemoteAddr()
}

// LocalAddr returns the local address of the connection.
func (f *Framer) LocalAddr() net.Addr {
	return f.conn.LocalAddr()
}
