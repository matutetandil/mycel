package email

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Sending an email.
//
// An email is often the only thing a customer sees of a flow — an order
// confirmation, a password reset — and the address it goes to comes from data,
// so it is wrong sometimes. What the server said about each address is the
// whole answer, and a send nobody accepted must not read as a send.

// fakeSMTP speaks enough of the protocol for a client to talk to it, and lets
// a test decide which addresses it will take.
type fakeSMTP struct {
	listener net.Listener
	refuse   map[string]bool
	failData bool

	mu       sync.Mutex
	from     string
	accepted []string
	body     strings.Builder
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &fakeSMTP{listener: listener, refuse: map[string]bool{}}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.serve(conn)
		}
	}()
	return server
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	write := func(line string) { fmt.Fprintf(conn, "%s\r\n", line) }

	write("220 fake.smtp.test ESMTP")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))

		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			write("250-fake.smtp.test")
			write("250 SIZE 35882577")
		case strings.HasPrefix(command, "MAIL FROM"):
			s.mu.Lock()
			s.from = addressIn(line)
			s.mu.Unlock()
			write("250 2.1.0 Ok")
		case strings.HasPrefix(command, "RCPT TO"):
			address := addressIn(line)
			if s.refuse[address] {
				// What a server says about an address that is not there.
				write("550 5.1.1 <" + address + ">: Recipient address rejected: User unknown")
				continue
			}
			s.mu.Lock()
			s.accepted = append(s.accepted, address)
			s.mu.Unlock()
			write("250 2.1.5 Ok")
		case strings.HasPrefix(command, "DATA"):
			if s.failData {
				write("554 5.5.1 Error: no valid recipients")
				continue
			}
			write("354 End data with <CR><LF>.<CR><LF>")
			for {
				body, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(body) == "." {
					break
				}
				s.mu.Lock()
				s.body.WriteString(body)
				s.mu.Unlock()
			}
			write("250 2.0.0 Ok: queued as 1234")
		case strings.HasPrefix(command, "RSET"):
			write("250 2.0.0 Ok")
		case strings.HasPrefix(command, "NOOP"):
			write("250 2.0.0 Ok")
		case strings.HasPrefix(command, "QUIT"):
			write("221 2.0.0 Bye")
			return
		default:
			write("500 5.5.2 Error: command not recognized")
		}
	}
}

func addressIn(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start < 0 || end < start {
		return strings.TrimSpace(line)
	}
	return line[start+1 : end]
}

func (s *fakeSMTP) connector(t *testing.T) *SMTPConnector {
	t.Helper()
	address := s.listener.Addr().(*net.TCPAddr)
	return NewSMTPConnector("mail", &Config{
		Driver: "smtp",
		From:   "orders@example.test",
		SMTP: &SMTPConfig{
			Host:    "127.0.0.1",
			Port:    address.Port,
			TLS:     "none",
			Timeout: 5 * time.Second,
		},
	})
}

func (s *fakeSMTP) sentBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

func TestAnEmailReachesItsRecipients(t *testing.T) {
	server := newFakeSMTP(t)
	c := server.connector(t)

	result, err := c.Send(context.Background(), &Email{
		To:       []Recipient{{Email: "someone@example.test"}},
		CC:       []Recipient{{Email: "copy@example.test"}},
		BCC:      []Recipient{{Email: "hidden@example.test"}},
		Subject:  "Your order",
		TextBody: "It is on its way",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success {
		t.Fatalf("result = %+v", result)
	}

	server.mu.Lock()
	accepted := append([]string(nil), server.accepted...)
	from := server.from
	server.mu.Unlock()

	if from != "orders@example.test" {
		t.Errorf("sent from %q, want the connector's address", from)
	}
	// Everybody the message is for, including the blind copies — which is the
	// point of them: they are addressed at the protocol and left out of the
	// headers.
	if len(accepted) != 3 {
		t.Errorf("the server was given %v, want all three", accepted)
	}

	body := server.sentBody()
	if !strings.Contains(body, "Subject: Your order") {
		t.Errorf("the subject is not in the message:\n%s", body)
	}
	if strings.Contains(body, "hidden@example.test") {
		t.Errorf("a blind copy was named in the headers, where every recipient can read it:\n%s", body)
	}
	if !strings.Contains(body, "copy@example.test") {
		t.Errorf("a visible copy was not named in the headers:\n%s", body)
	}
}

func TestAnAddressTheServerRefused(t *testing.T) {
	// One bad address out of several: the rest still receive it, and which
	// one failed is on the result — that is what a flow logs or stores so
	// somebody can correct the record.
	server := newFakeSMTP(t)
	server.refuse["gone@example.test"] = true
	c := server.connector(t)

	result, err := c.Send(context.Background(), &Email{
		To:       []Recipient{{Email: "someone@example.test"}, {Email: "gone@example.test"}},
		Subject:  "Your order",
		TextBody: "It is on its way",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success {
		t.Errorf("a message one recipient took was reported as failed: %+v", result)
	}

	var refused *RecipientResult
	for i := range result.Recipients {
		if !result.Recipients[i].Success {
			refused = &result.Recipients[i]
		}
	}
	if refused == nil {
		t.Fatalf("nothing says an address was refused: %+v", result.Recipients)
	}
	if refused.Email != "gone@example.test" {
		t.Errorf("the wrong address was recorded as refused: %+v", refused)
	}
	// What the server said, so somebody reading it can tell "user unknown"
	// from "mailbox full".
	if !strings.Contains(refused.Error, "550") && !strings.Contains(refused.Error, "unknown") {
		t.Errorf("the reason was not kept: %q", refused.Error)
	}
}

func TestAnEmailNobodyAccepted(t *testing.T) {
	// Every address refused. The send used to carry on to DATA and come back
	// a success — it relied on the server objecting, and a server that
	// accepts the message and drops it looked the same as one that delivered
	// it.
	server := newFakeSMTP(t)
	server.refuse["gone@example.test"] = true
	server.refuse["also-gone@example.test"] = true
	c := server.connector(t)

	result, err := c.Send(context.Background(), &Email{
		To:       []Recipient{{Email: "gone@example.test"}, {Email: "also-gone@example.test"}},
		Subject:  "Your order",
		TextBody: "It is on its way",
	})

	if err == nil {
		t.Fatal("an email nobody accepted was reported as sent")
	}
	if result.Success {
		t.Errorf("result = %+v", result)
	}
	// The message names the addresses, because the flow that sent it knows
	// them only as data.
	if !strings.Contains(result.Error, "gone@example.test") {
		t.Errorf("the error does not say which addresses were refused: %q", result.Error)
	}
	if len(result.Recipients) != 2 {
		t.Errorf("recipients = %+v", result.Recipients)
	}
	// Nothing was written to the server after the refusals.
	if body := server.sentBody(); body != "" {
		t.Errorf("a message was sent anyway:\n%s", body)
	}
}

func TestAServerThatRefusesTheMessageItself(t *testing.T) {
	// Over a size limit, caught by a content filter: the addresses were fine
	// and the message was not.
	server := newFakeSMTP(t)
	server.failData = true
	c := server.connector(t)

	result, err := c.Send(context.Background(), &Email{
		To:       []Recipient{{Email: "someone@example.test"}},
		Subject:  "Your order",
		TextBody: "It is on its way",
	})
	if err == nil {
		t.Fatal("a message the server refused was reported as sent")
	}
	if result.Success {
		t.Errorf("result = %+v", result)
	}
}

func TestAnEmailWithNobodyToSendFrom(t *testing.T) {
	server := newFakeSMTP(t)
	address := server.listener.Addr().(*net.TCPAddr)

	c := NewSMTPConnector("mail", &Config{
		Driver: "smtp",
		SMTP:   &SMTPConfig{Host: "127.0.0.1", Port: address.Port, TLS: "none", Timeout: 5 * time.Second},
	})

	if _, err := c.Send(context.Background(), &Email{
		To:      []Recipient{{Email: "someone@example.test"}},
		Subject: "Your order",
	}); err == nil {
		t.Error("an email was sent from no address at all")
	}
}

func TestAServerThatIsNotThere(t *testing.T) {
	c := NewSMTPConnector("mail", &Config{
		Driver: "smtp", From: "orders@example.test",
		SMTP: &SMTPConfig{Host: "127.0.0.1", Port: 1, TLS: "none", Timeout: time.Second},
	})

	result, err := c.Send(context.Background(), &Email{
		To:      []Recipient{{Email: "someone@example.test"}},
		Subject: "Your order",
	})
	if err == nil {
		t.Fatal("an email was sent to a server nobody is running")
	}
	if result.Success {
		t.Errorf("result = %+v", result)
	}
}

func TestAWriteBecomesTheEmail(t *testing.T) {
	// The path a flow takes.
	server := newFakeSMTP(t)
	c := server.connector(t)

	result, err := c.Write(context.Background(), &connector.Data{
		Target: "someone@example.test",
		Payload: map[string]interface{}{
			"subject": "Your order",
			"body":    "It is on its way",
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}

	server.mu.Lock()
	accepted := append([]string(nil), server.accepted...)
	server.mu.Unlock()
	if len(accepted) != 1 || accepted[0] != "someone@example.test" {
		t.Errorf("sent to %v, want the flow's target", accepted)
	}

	// And a refusal fails the flow rather than being reported as sent.
	server.refuse["gone@example.test"] = true
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  "gone@example.test",
		Payload: map[string]interface{}{"subject": "Your order"},
	}); err == nil {
		t.Error("a flow was told an email went out that nobody accepted")
	}
}

func TestTheConnectionIsReused(t *testing.T) {
	// Opening a connection per email is a handshake and an authentication
	// each time, which is what the pool exists to avoid — and a pooled
	// connection that went stale must be replaced rather than used.
	server := newFakeSMTP(t)
	c := server.connector(t)

	for i := 0; i < 3; i++ {
		if _, err := c.Send(context.Background(), &Email{
			To:       []Recipient{{Email: "someone@example.test"}},
			Subject:  fmt.Sprintf("Message %d", i),
			TextBody: "hello",
		}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	server.mu.Lock()
	accepted := len(server.accepted)
	server.mu.Unlock()
	if accepted != 3 {
		t.Errorf("the server took %d messages, want three", accepted)
	}

	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}
