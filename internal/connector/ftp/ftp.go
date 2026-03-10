// Package ftp provides an FTP/SFTP connector for reading and writing remote files.
package ftp

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	goftp "github.com/jlaffaye/ftp"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/matutetandil/mycel/internal/connector"
)

// Config holds configuration for an FTP/SFTP connector.
type Config struct {
	// Host is the FTP/SFTP server hostname or IP address.
	Host string

	// Port is the server port (default: 21 for FTP, 22 for SFTP).
	Port int

	// Username is the authentication username.
	Username string

	// Password is the authentication password.
	Password string

	// Protocol is either "ftp" or "sftp" (default: "ftp").
	Protocol string

	// BasePath is the remote base directory for all operations.
	BasePath string

	// KeyFile is the path to an SSH private key file (SFTP only).
	KeyFile string

	// Passive enables FTP passive mode (default: true).
	Passive bool

	// Timeout is the connection timeout.
	Timeout time.Duration

	// TLS enables explicit TLS for FTP (FTPS).
	TLS bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Protocol: "ftp",
		Passive:  true,
		Timeout:  30 * time.Second,
	}
}

// FileInfo represents metadata about a remote file.
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

// remoteClient abstracts FTP vs SFTP operations behind a common interface.
type remoteClient interface {
	List(path string) ([]FileInfo, error)
	Get(path string) ([]byte, error)
	Put(path string, content []byte) error
	Mkdir(path string) error
	Remove(path string) error
	Close() error
}

// Connector implements the connector.Connector, connector.Reader, and
// connector.Writer interfaces for FTP and SFTP servers.
type Connector struct {
	name   string
	config *Config
	client remoteClient

	mu sync.RWMutex
}

// New creates a new FTP/SFTP connector.
func New(name string, config *Config) *Connector {
	if config.Protocol == "" {
		config.Protocol = "ftp"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Port == 0 {
		if config.Protocol == "sftp" {
			config.Port = 22
		} else {
			config.Port = 21
		}
	}

	return &Connector{
		name:   name,
		config: config,
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "ftp"
}

// Connect establishes the connection to the FTP/SFTP server.
func (c *Connector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var client remoteClient
	var err error

	switch c.config.Protocol {
	case "sftp":
		client, err = newSFTPClient(c.config)
	case "ftp":
		client, err = newFTPClient(c.config)
	default:
		return fmt.Errorf("unsupported protocol: %s (use \"ftp\" or \"sftp\")", c.config.Protocol)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to %s://%s:%d: %w",
			c.config.Protocol, c.config.Host, c.config.Port, err)
	}

	c.client = client
	return nil
}

// Close terminates the connection gracefully.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

// Health checks if the connector is healthy by listing the base path.
func (c *Connector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("ftp client not connected")
	}

	listPath := "/"
	if c.config.BasePath != "" {
		listPath = c.config.BasePath
	}

	_, err := c.client.List(listPath)
	return err
}

// Read reads from the remote FTP/SFTP server.
//
// Operations:
//   - "LIST": returns directory listing as rows [{name, size, mod_time, is_dir}]
//   - "GET" or "": downloads file content
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("ftp client not connected")
	}

	remotePath := c.resolvePath(query.Target)
	operation := strings.ToUpper(query.Operation)

	var rows []map[string]interface{}
	var err error

	switch operation {
	case "LIST":
		rows, err = c.listDirectory(remotePath)
	case "GET", "":
		rows, err = c.downloadFile(remotePath, query.Params)
	default:
		return nil, fmt.Errorf("unknown read operation: %s", query.Operation)
	}

	if err != nil {
		return nil, err
	}

	return &connector.Result{Rows: rows}, nil
}

// Write writes to the remote FTP/SFTP server.
//
// Operations:
//   - "PUT", "UPLOAD", or "": uploads content to the target path
//   - "MKDIR": creates a remote directory
//   - "DELETE": removes a remote file
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, fmt.Errorf("ftp client not connected")
	}

	remotePath := c.resolvePath(data.Target)
	operation := strings.ToUpper(data.Operation)

	var metadata map[string]interface{}
	var err error

	switch operation {
	case "PUT", "UPLOAD", "":
		metadata, err = c.uploadFile(remotePath, data.Payload)
	case "MKDIR":
		metadata, err = c.makeDirectory(remotePath)
	case "DELETE":
		metadata, err = c.removeFile(remotePath)
	default:
		return nil, fmt.Errorf("unknown write operation: %s", data.Operation)
	}

	if err != nil {
		return nil, err
	}

	return &connector.Result{
		Affected: 1,
		Metadata: metadata,
	}, nil
}

// resolvePath joins the base path with the given target path.
func (c *Connector) resolvePath(target string) string {
	if c.config.BasePath == "" {
		return path.Clean("/" + target)
	}
	return path.Join(c.config.BasePath, target)
}

// listDirectory lists files at the given remote path.
func (c *Connector) listDirectory(remotePath string) ([]map[string]interface{}, error) {
	entries, err := c.client.List(remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %s: %w", remotePath, err)
	}

	rows := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, map[string]interface{}{
			"name":     entry.Name,
			"size":     entry.Size,
			"mod_time": entry.ModTime.Format(time.RFC3339),
			"is_dir":   entry.IsDir,
		})
	}
	return rows, nil
}

// downloadFile downloads a file from the remote server and parses its content.
func (c *Connector) downloadFile(remotePath string, params map[string]interface{}) ([]map[string]interface{}, error) {
	data, err := c.client.Get(remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", remotePath, err)
	}

	format := getFormat(remotePath, params)
	return c.parseContent(data, format, remotePath)
}

// uploadFile uploads content to a remote path.
func (c *Connector) uploadFile(remotePath string, payload map[string]interface{}) (map[string]interface{}, error) {
	content, ok := payload["_content"]
	if !ok {
		// Fall back to serializing the entire payload as JSON
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize payload: %w", err)
		}
		if err := c.client.Put(remotePath, data); err != nil {
			return nil, fmt.Errorf("failed to upload to %s: %w", remotePath, err)
		}
		return map[string]interface{}{
			"path":     remotePath,
			"size":     len(data),
			"uploaded": true,
		}, nil
	}

	var data []byte
	switch v := content.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize content: %w", err)
		}
	}

	if err := c.client.Put(remotePath, data); err != nil {
		return nil, fmt.Errorf("failed to upload to %s: %w", remotePath, err)
	}

	return map[string]interface{}{
		"path":     remotePath,
		"size":     len(data),
		"uploaded": true,
	}, nil
}

// makeDirectory creates a remote directory.
func (c *Connector) makeDirectory(remotePath string) (map[string]interface{}, error) {
	if err := c.client.Mkdir(remotePath); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", remotePath, err)
	}

	return map[string]interface{}{
		"path":    remotePath,
		"created": true,
	}, nil
}

// removeFile removes a remote file.
func (c *Connector) removeFile(remotePath string) (map[string]interface{}, error) {
	if err := c.client.Remove(remotePath); err != nil {
		return nil, fmt.Errorf("failed to remove %s: %w", remotePath, err)
	}

	return map[string]interface{}{
		"path":    remotePath,
		"deleted": true,
	}, nil
}

// parseContent parses downloaded file content based on format.
func (c *Connector) parseContent(data []byte, format, remotePath string) ([]map[string]interface{}, error) {
	_, filename := path.Split(remotePath)

	switch format {
	case "json":
		var result interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		switch v := result.(type) {
		case []interface{}:
			rows := make([]map[string]interface{}, 0, len(v))
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					rows = append(rows, m)
				} else {
					rows = append(rows, map[string]interface{}{"value": item})
				}
			}
			return rows, nil
		case map[string]interface{}:
			return []map[string]interface{}{v}, nil
		default:
			return []map[string]interface{}{{"value": v}}, nil
		}

	case "csv":
		reader := csv.NewReader(bytes.NewReader(data))
		records, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("failed to parse CSV: %w", err)
		}
		if len(records) == 0 {
			return []map[string]interface{}{}, nil
		}
		headers := records[0]
		rows := make([]map[string]interface{}, 0, len(records)-1)
		for _, record := range records[1:] {
			row := make(map[string]interface{})
			for j, value := range record {
				if j < len(headers) {
					row[headers[j]] = value
				}
			}
			rows = append(rows, row)
		}
		return rows, nil

	case "text":
		return []map[string]interface{}{
			{
				"_path":    remotePath,
				"_name":    filename,
				"_content": string(data),
				"_size":    len(data),
			},
		}, nil

	default:
		// Binary or unknown: return raw content metadata
		return []map[string]interface{}{
			{
				"_path":    remotePath,
				"_name":    filename,
				"_content": data,
				"_size":    len(data),
			},
		}, nil
	}
}

// getFormat detects the file format from params or file extension.
func getFormat(remotePath string, params map[string]interface{}) string {
	if params != nil {
		if f, ok := params["format"].(string); ok && f != "" {
			return f
		}
	}
	ext := strings.ToLower(path.Ext(remotePath))
	switch ext {
	case ".json":
		return "json"
	case ".csv":
		return "csv"
	case ".txt", ".log", ".md":
		return "text"
	default:
		return "binary"
	}
}

// --------------------------------------------------------------------------
// FTP client implementation
// --------------------------------------------------------------------------

type ftpClient struct {
	conn *goftp.ServerConn
}

func newFTPClient(cfg *Config) (*ftpClient, error) {
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))

	var opts []goftp.DialOption
	opts = append(opts, goftp.DialWithTimeout(cfg.Timeout))

	if cfg.TLS {
		opts = append(opts, goftp.DialWithExplicitTLS(nil))
	}

	conn, err := goftp.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("ftp dial failed: %w", err)
	}

	if cfg.Username != "" {
		if err := conn.Login(cfg.Username, cfg.Password); err != nil {
			conn.Quit()
			return nil, fmt.Errorf("ftp login failed: %w", err)
		}
	}

	return &ftpClient{conn: conn}, nil
}

func (f *ftpClient) List(dirPath string) ([]FileInfo, error) {
	entries, err := f.conn.List(dirPath)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, e := range entries {
		// Skip . and .. entries
		if e.Name == "." || e.Name == ".." {
			continue
		}
		files = append(files, FileInfo{
			Name:    e.Name,
			Size:    int64(e.Size),
			ModTime: e.Time,
			IsDir:   e.Type == goftp.EntryTypeFolder,
		})
	}
	return files, nil
}

func (f *ftpClient) Get(filePath string) ([]byte, error) {
	resp, err := f.conn.Retr(filePath)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	return io.ReadAll(resp)
}

func (f *ftpClient) Put(filePath string, content []byte) error {
	return f.conn.Stor(filePath, bytes.NewReader(content))
}

func (f *ftpClient) Mkdir(dirPath string) error {
	return f.conn.MakeDir(dirPath)
}

func (f *ftpClient) Remove(filePath string) error {
	return f.conn.Delete(filePath)
}

func (f *ftpClient) Close() error {
	return f.conn.Quit()
}

// --------------------------------------------------------------------------
// SFTP client implementation
// --------------------------------------------------------------------------

type sftpClient struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

func newSFTPClient(cfg *Config) (*sftpClient, error) {
	var authMethods []ssh.AuthMethod

	// Key-based authentication
	if cfg.KeyFile != "" {
		key, err := os.ReadFile(cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %s: %w", cfg.KeyFile, err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// Password authentication
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("sftp requires either password or key_file authentication")
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         cfg.Timeout,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	sshConn, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("ssh dial failed: %w", err)
	}

	sc, err := sftp.NewClient(sshConn)
	if err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("sftp client creation failed: %w", err)
	}

	return &sftpClient{
		sshClient:  sshConn,
		sftpClient: sc,
	}, nil
}

func (s *sftpClient) List(dirPath string) ([]FileInfo, error) {
	entries, err := s.sftpClient.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, e := range entries {
		if e.Name() == "." || e.Name() == ".." {
			continue
		}
		files = append(files, FileInfo{
			Name:    e.Name(),
			Size:    e.Size(),
			ModTime: e.ModTime(),
			IsDir:   e.IsDir(),
		})
	}
	return files, nil
}

func (s *sftpClient) Get(filePath string) ([]byte, error) {
	f, err := s.sftpClient.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

func (s *sftpClient) Put(filePath string, content []byte) error {
	f, err := s.sftpClient.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(content)
	return err
}

func (s *sftpClient) Mkdir(dirPath string) error {
	return s.sftpClient.MkdirAll(dirPath)
}

func (s *sftpClient) Remove(filePath string) error {
	return s.sftpClient.Remove(filePath)
}

func (s *sftpClient) Close() error {
	if s.sftpClient != nil {
		s.sftpClient.Close()
	}
	if s.sshClient != nil {
		s.sshClient.Close()
	}
	return nil
}
