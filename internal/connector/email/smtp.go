package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// SMTPConnector sends emails via SMTP
type SMTPConnector struct {
	name   string
	config *Config

	// Connection pool
	pool    chan *smtpConn
	poolMu  sync.Mutex
}

type smtpConn struct {
	client    *smtp.Client
	createdAt time.Time
}

// NewSMTPConnector creates a new SMTP connector
func NewSMTPConnector(name string, config *Config) *SMTPConnector {
	if config.SMTP == nil {
		config.SMTP = DefaultSMTPConfig()
	}
	if config.SMTP.Timeout == 0 {
		config.SMTP.Timeout = 30 * time.Second
	}
	if config.SMTP.PoolSize == 0 {
		config.SMTP.PoolSize = 5
	}

	return &SMTPConnector{
		name:   name,
		config: config,
		pool:   make(chan *smtpConn, config.SMTP.PoolSize),
	}
}

// Name returns the connector name
func (c *SMTPConnector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *SMTPConnector) Type() string {
	return "email"
}

// Connect establishes the connection pool
func (c *SMTPConnector) Connect(ctx context.Context) error {
	// Pre-warm one connection to verify config
	conn, err := c.dial(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

	select {
	case c.pool <- &smtpConn{client: conn, createdAt: time.Now()}:
	default:
		conn.Close()
	}

	return nil
}

// Send sends an email
func (c *SMTPConnector) Send(ctx context.Context, email *Email) (*SendResult, error) {
	// Apply config-level template as default if no per-email template
	if email.Template == "" && c.config.Template != "" {
		email.Template = c.config.Template
	}
	// Render HTML template if specified
	if email.Template != "" {
		if err := email.RenderTemplate(nil); err != nil {
			return &SendResult{Success: false, Provider: "smtp", Error: err.Error()}, err
		}
	}

	// Get connection from pool
	conn, err := c.getConnection(ctx)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "smtp",
			Error:    err.Error(),
		}, err
	}
	defer c.returnConnection(conn)

	// Build message
	msg, err := c.buildMessage(email)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "smtp",
			Error:    err.Error(),
		}, err
	}

	// Get from address
	from := email.From
	if from == "" {
		from = c.config.From
	}
	if from == "" {
		return &SendResult{
			Success:  false,
			Provider: "smtp",
			Error:    "from address is required",
		}, fmt.Errorf("from address is required")
	}

	// Collect all recipients
	var recipients []string
	for _, r := range email.To {
		recipients = append(recipients, r.Email)
	}
	for _, r := range email.CC {
		recipients = append(recipients, r.Email)
	}
	for _, r := range email.BCC {
		recipients = append(recipients, r.Email)
	}

	// Send
	err = conn.client.Mail(from)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "smtp",
			Error:    fmt.Sprintf("MAIL FROM failed: %v", err),
		}, err
	}

	var recipientResults []RecipientResult
	for _, rcpt := range recipients {
		err := conn.client.Rcpt(rcpt)
		recipientResults = append(recipientResults, RecipientResult{
			Email:   rcpt,
			Success: err == nil,
			Error:   errorString(err),
		})
	}

	// Send data
	w, err := conn.client.Data()
	if err != nil {
		return &SendResult{
			Success:    false,
			Provider:   "smtp",
			Error:      fmt.Sprintf("DATA failed: %v", err),
			Recipients: recipientResults,
		}, err
	}

	_, err = w.Write(msg)
	if err != nil {
		w.Close()
		return &SendResult{
			Success:    false,
			Provider:   "smtp",
			Error:      fmt.Sprintf("write failed: %v", err),
			Recipients: recipientResults,
		}, err
	}

	err = w.Close()
	if err != nil {
		return &SendResult{
			Success:    false,
			Provider:   "smtp",
			Error:      fmt.Sprintf("close failed: %v", err),
			Recipients: recipientResults,
		}, err
	}

	// Reset for next message
	conn.client.Reset()

	return &SendResult{
		Success:    true,
		Provider:   "smtp",
		Recipients: recipientResults,
	}, nil
}

func (c *SMTPConnector) buildMessage(email *Email) ([]byte, error) {
	var msg strings.Builder

	// From
	from := email.From
	fromName := email.FromName
	if from == "" {
		from = c.config.From
		fromName = c.config.FromName
	}
	if fromName != "" {
		msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, from))
	} else {
		msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	}

	// To
	var toAddrs []string
	for _, r := range email.To {
		if r.Name != "" {
			toAddrs = append(toAddrs, fmt.Sprintf("%s <%s>", r.Name, r.Email))
		} else {
			toAddrs = append(toAddrs, r.Email)
		}
	}
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(toAddrs, ", ")))

	// CC
	if len(email.CC) > 0 {
		var ccAddrs []string
		for _, r := range email.CC {
			if r.Name != "" {
				ccAddrs = append(ccAddrs, fmt.Sprintf("%s <%s>", r.Name, r.Email))
			} else {
				ccAddrs = append(ccAddrs, r.Email)
			}
		}
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccAddrs, ", ")))
	}

	// Reply-To
	replyTo := email.ReplyTo
	if replyTo == "" {
		replyTo = c.config.ReplyTo
	}
	if replyTo != "" {
		msg.WriteString(fmt.Sprintf("Reply-To: %s\r\n", replyTo))
	}

	// Subject
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", email.Subject))

	// Custom headers
	for k, v := range email.Headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	// MIME
	msg.WriteString("MIME-Version: 1.0\r\n")

	// Content
	if email.HTMLBody != "" && email.TextBody != "" {
		// Multipart
		boundary := "----=_Part_0_" + fmt.Sprintf("%d", time.Now().UnixNano())
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		msg.WriteString("\r\n")

		// Text part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(email.TextBody)
		msg.WriteString("\r\n")

		// HTML part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(email.HTMLBody)
		msg.WriteString("\r\n")

		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else if email.HTMLBody != "" {
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(email.HTMLBody)
	} else {
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(email.TextBody)
	}

	return []byte(msg.String()), nil
}

func (c *SMTPConnector) dial(ctx context.Context) (*smtp.Client, error) {
	cfg := c.config.SMTP
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// Create dialer with timeout
	dialer := &net.Dialer{
		Timeout: cfg.Timeout,
	}

	var conn net.Conn
	var err error

	switch cfg.TLS {
	case "tls", "ssl":
		// Direct TLS (port 465)
		tlsConfig := &tls.Config{ServerName: cfg.Host}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	default:
		// Plain connection (will upgrade with STARTTLS if needed)
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}

	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create client failed: %w", err)
	}

	// STARTTLS if needed
	if cfg.TLS == "starttls" || cfg.TLS == "" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: cfg.Host}
			if err := client.StartTLS(tlsConfig); err != nil {
				client.Close()
				return nil, fmt.Errorf("STARTTLS failed: %w", err)
			}
		}
	}

	// Auth
	if cfg.Username != "" && cfg.Password != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			client.Close()
			return nil, fmt.Errorf("auth failed: %w", err)
		}
	}

	return client, nil
}

func (c *SMTPConnector) getConnection(ctx context.Context) (*smtpConn, error) {
	// Try to get from pool
	select {
	case conn := <-c.pool:
		// Check if connection is still valid
		if err := conn.client.Noop(); err != nil {
			conn.client.Close()
			// Create new connection
			newConn, err := c.dial(ctx)
			if err != nil {
				return nil, err
			}
			return &smtpConn{client: newConn, createdAt: time.Now()}, nil
		}
		return conn, nil
	default:
		// Pool empty, create new connection
		newConn, err := c.dial(ctx)
		if err != nil {
			return nil, err
		}
		return &smtpConn{client: newConn, createdAt: time.Now()}, nil
	}
}

func (c *SMTPConnector) returnConnection(conn *smtpConn) {
	// Don't return old connections
	if time.Since(conn.createdAt) > 5*time.Minute {
		conn.client.Close()
		return
	}

	select {
	case c.pool <- conn:
	default:
		conn.client.Close()
	}
}

// Health checks if SMTP server is reachable
func (c *SMTPConnector) Health(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Close closes all connections
func (c *SMTPConnector) Close(ctx context.Context) error {
	close(c.pool)
	for conn := range c.pool {
		conn.client.Close()
	}
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
