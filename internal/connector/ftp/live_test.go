package ftp

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Against a real SFTP server.
//
// Connect and Close were not covered at all and everything else was covered
// only as far as its arguments: what a file server does with a transfer is not
// something a mock can be asked. The integration stack runs one, so this
// uploads a file, lists the directory, reads it back and deletes it — the four
// things every flow that touches a file server does.
func liveSFTP(t *testing.T) *Connector {
	t.Helper()

	address := os.Getenv("MYCEL_TEST_SFTP_ADDR")
	if address == "" {
		address = "127.0.0.1:32222"
	}
	if !reachable(address) {
		t.Skipf("no SFTP server at %s (the integration stack publishes one)", address)
	}

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("MYCEL_TEST_SFTP_ADDR: %v", err)
	}
	port, _ := strconv.Atoi(portText)

	c := New("files", &Config{
		Host:     host,
		Port:     port,
		Username: envOr("MYCEL_TEST_SFTP_USER", "testuser"),
		Password: envOr("MYCEL_TEST_SFTP_PASS", "testpass"),
		Protocol: "sftp",
		BasePath: envOr("MYCEL_TEST_SFTP_PATH", "/upload"),
	}, nil)

	if err := c.Connect(context.Background()); err != nil {
		t.Skipf("cannot reach the SFTP server at %s: %v", address, err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func reachable(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestAFileGoesUpAndComesBack(t *testing.T) {
	c := liveSFTP(t)
	ctx := context.Background()

	name := "mycel-live-" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36) + ".csv"
	content := "id,total\n1,99\n"

	// Up. The operation is the one the runtime sends when a flow does not name
	// one, which is the ordinary case.
	written, err := c.Write(ctx, &connector.Data{
		Target:    name,
		Operation: "INSERT",
		Payload:   map[string]interface{}{"_content": content},
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if written == nil {
		t.Fatal("nothing came back from an upload")
	}
	t.Cleanup(func() {
		_, _ = c.Write(ctx, &connector.Data{Target: name, Operation: "DELETE"})
	})

	// Listed.
	listing, err := c.Read(ctx, connector.Query{Target: ".", Operation: "LIST"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, row := range listing.Rows {
		if row["name"] == name {
			found = true
			if size, ok := row["size"].(int64); ok && size != int64(len(content)) {
				t.Errorf("the listing says %d bytes, the file is %d", size, len(content))
			}
		}
	}
	if !found {
		t.Errorf("the file was uploaded and the listing does not have it: %v", listing.Rows)
	}

	// And back, by the verb the runtime sends for a read.
	read, err := c.Read(ctx, connector.Query{Target: name, Operation: "SELECT"})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(read.Rows) != 1 {
		t.Fatalf("%d rows came back from a download", len(read.Rows))
	}

	// A .csv is parsed into rows on the way back — the connector reads the
	// format from the extension — so what comes back is the record, not the
	// bytes.
	got := read.Rows[0]
	if got["id"] != "1" || got["total"] != "99" {
		t.Errorf("the csv came back as %#v, want the record it holds", got)
	}

	// Gone.
	deleted, err := c.Write(ctx, &connector.Data{Target: name, Operation: "DELETE"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted == nil {
		t.Error("nothing came back from a delete")
	}

	after, err := c.Read(ctx, connector.Query{Target: ".", Operation: "LIST"})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, row := range after.Rows {
		if row["name"] == name {
			t.Errorf("the file is still there after being deleted")
		}
	}
}

// A file whose extension says nothing comes back as its bytes.
func TestAPlainFileComesBackAsItsContent(t *testing.T) {
	c := liveSFTP(t)
	ctx := context.Background()

	name := "mycel-live-" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36) + ".txt"
	content := "just some words\n"

	if _, err := c.Write(ctx, &connector.Data{
		Target:  name,
		Payload: map[string]interface{}{"_content": content},
	}); err != nil {
		t.Fatalf("upload: %v", err)
	}
	t.Cleanup(func() {
		_, _ = c.Write(ctx, &connector.Data{Target: name, Operation: "DELETE"})
	})

	read, err := c.Read(ctx, connector.Query{Target: name})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(read.Rows) != 1 {
		t.Fatalf("%d rows", len(read.Rows))
	}
	if got := read.Rows[0]["_content"]; got != content {
		t.Errorf("_content = %#v, want what was written", got)
	}
	if size, ok := read.Rows[0]["_size"].(int); !ok || size != len(content) {
		t.Errorf("_size = %#v, want %d", read.Rows[0]["_size"], len(content))
	}
}

// A directory made and removed, which is the other pair of operations.
func TestADirectoryIsMadeAndRemoved(t *testing.T) {
	c := liveSFTP(t)
	ctx := context.Background()

	name := "mycel-dir-" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

	if _, err := c.Write(ctx, &connector.Data{Target: name, Operation: "MKDIR"}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	listing, err := c.Read(ctx, connector.Query{Target: ".", Operation: "LIST"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, row := range listing.Rows {
		if row["name"] == name {
			found = true
			if isDir, ok := row["is_dir"].(bool); ok && !isDir {
				t.Error("the listing does not say it is a directory")
			}
		}
	}
	if !found {
		t.Errorf("the directory was created and the listing does not have it")
	}

	if _, err := c.Write(ctx, &connector.Data{Target: name, Operation: "DELETE"}); err != nil {
		t.Logf("removing the directory: %v", err)
	}
}

// The name of the thing being read is not always spelled the same way.
func TestAConnectorWithoutASessionSaysSo(t *testing.T) {
	c := New("files", &Config{Host: "127.0.0.1", Port: 1, Protocol: "sftp"}, nil)

	_, err := c.Read(context.Background(), connector.Query{Target: "x", Operation: "LIST"})
	if err == nil {
		t.Fatal("a read on a connector that never connected was accepted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "connect") {
		t.Errorf("the error reads %q; it should say it is not connected", err)
	}
}
