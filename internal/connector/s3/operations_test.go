package s3

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Objects in a bucket, against a real S3-compatible server.
//
// The operations a flow reaches through a step rather than a write — copy,
// move, delete, and the signed links a service hands a browser — had nothing.
// A signed link that does not work is a download button that does not; a move
// that copies without deleting is a bucket that quietly doubles.

func liveBucket(t *testing.T) *Connector {
	t.Helper()

	endpoint := os.Getenv("MYCEL_TEST_S3_ENDPOINT")
	if endpoint == "" {
		if !reachable("127.0.0.1:39000") {
			t.Skip("no S3-compatible server at 127.0.0.1:39000 (the integration stack publishes MinIO)")
		}
		endpoint = "http://127.0.0.1:39000"
	}

	c := New("objects", &Config{
		Bucket:       "test-bucket",
		Region:       "us-east-1",
		Endpoint:     endpoint,
		AccessKey:    "minioadmin",
		SecretKey:    "minioadmin",
		UsePathStyle: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Skipf("the bucket is not answering: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func reachable(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func objectKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("tests/%d.json", time.Now().UnixNano())
}

func TestAnObjectComesBackTheWayItWasWritten(t *testing.T) {
	c := liveBucket(t)
	ctx := context.Background()
	key := objectKey(t)

	if _, err := c.Write(ctx, &connector.Data{
		Target: key,
		Params: map[string]interface{}{
			"content": map[string]interface{}{"sku": "WIDGET-1", "on_hand": 10},
		},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	t.Cleanup(func() { _, _ = c.Call(ctx, "delete", map[string]interface{}{"key": key}) })

	rows, err := c.Read(ctx, connector.Query{Target: key})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows.Rows) != 1 || rows.Rows[0]["sku"] != "WIDGET-1" {
		t.Errorf("rows = %v, want what was written", rows)
	}
}

func TestAskingWhetherAnObjectIsThere(t *testing.T) {
	// A flow branching on whether a file has arrived yet, which is the
	// ordinary reason to reach for this.
	c := liveBucket(t)
	ctx := context.Background()
	key := objectKey(t)

	answer, err := c.Call(ctx, "exists", map[string]interface{}{"key": key})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if present(answer) {
		t.Error("an object nobody wrote is reported as there")
	}

	if _, err := c.Write(ctx, &connector.Data{
		Target: key,
		Params: map[string]interface{}{"content": map[string]interface{}{"sku": "WIDGET-1"}},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	t.Cleanup(func() { _, _ = c.Call(ctx, "delete", map[string]interface{}{"key": key}) })

	answer, err = c.Call(ctx, "exists", map[string]interface{}{"key": key})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !present(answer) {
		t.Errorf("an object that was written is reported as missing: %v", answer)
	}

	// And what the server knows about it without fetching it, which is how a
	// flow checks a size before deciding to read.
	head, err := c.Call(ctx, "head", map[string]interface{}{"key": key})
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	described, ok := head.(map[string]interface{})
	if !ok || described["size"] == nil {
		t.Errorf("head = %v, want the size at least", head)
	}
}

func present(answer interface{}) bool {
	switch v := answer.(type) {
	case bool:
		return v
	case map[string]interface{}:
		if exists, ok := v["exists"].(bool); ok {
			return exists
		}
	}
	return false
}

func TestCopyingLeavesBothAndMovingLeavesOne(t *testing.T) {
	// The difference is the whole reason both exist: a move that copies
	// without deleting is a bucket that quietly doubles.
	c := liveBucket(t)
	ctx := context.Background()
	original := objectKey(t)
	copied := original + ".copy"
	moved := original + ".moved"

	if _, err := c.Write(ctx, &connector.Data{
		Target: original,
		Params: map[string]interface{}{"content": map[string]interface{}{"sku": "WIDGET-1"}},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	t.Cleanup(func() {
		for _, key := range []string{original, copied, moved} {
			_, _ = c.Call(ctx, "delete", map[string]interface{}{"key": key})
		}
	})

	if _, err := c.Call(ctx, "copy", map[string]interface{}{
		"source": original, "destination": copied,
	}); err != nil {
		t.Fatalf("copy: %v", err)
	}

	for _, key := range []string{original, copied} {
		answer, err := c.Call(ctx, "exists", map[string]interface{}{"key": key})
		if err != nil {
			t.Fatalf("exists: %v", err)
		}
		if !present(answer) {
			t.Errorf("%s is not there after the copy", key)
		}
	}

	if _, err := c.Call(ctx, "move", map[string]interface{}{
		"source": copied, "destination": moved,
	}); err != nil {
		t.Fatalf("move: %v", err)
	}

	answer, err := c.Call(ctx, "exists", map[string]interface{}{"key": copied})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if present(answer) {
		t.Error("the object a move took from is still there, so the bucket doubled")
	}
	answer, err = c.Call(ctx, "exists", map[string]interface{}{"key": moved})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !present(answer) {
		t.Error("the object a move was supposed to leave is not there")
	}
}

func TestDeletingAnObjectTakesIt(t *testing.T) {
	c := liveBucket(t)
	ctx := context.Background()
	key := objectKey(t)

	if _, err := c.Write(ctx, &connector.Data{
		Target: key,
		Params: map[string]interface{}{"content": map[string]interface{}{"sku": "WIDGET-1"}},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := c.Call(ctx, "delete", map[string]interface{}{"key": key}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	answer, err := c.Call(ctx, "exists", map[string]interface{}{"key": key})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if present(answer) {
		t.Error("the object is still there after being deleted")
	}
}

func TestASignedLinkWorksWithoutCredentials(t *testing.T) {
	// This is what a service hands a browser so that a download does not go
	// through it. A link that does not work is a download button that does
	// not, and nothing here would have said so.
	c := liveBucket(t)
	ctx := context.Background()
	key := objectKey(t)

	if _, err := c.Write(ctx, &connector.Data{
		Target: key,
		Params: map[string]interface{}{"content": map[string]interface{}{"sku": "WIDGET-1"}},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	t.Cleanup(func() { _, _ = c.Call(ctx, "delete", map[string]interface{}{"key": key}) })

	answer, err := c.Call(ctx, "presign_get", map[string]interface{}{
		"key": key, "expires": "5m",
	})
	if err != nil {
		t.Fatalf("presign_get: %v", err)
	}

	link := linkOf(answer)
	if link == "" {
		t.Fatalf("answer = %v, want a link", answer)
	}
	if !strings.Contains(link, key) {
		t.Errorf("the link does not name the object: %s", link)
	}

	// The point of signing it: it works with no credentials at all.
	resp, err := http.Get(link)
	if err != nil {
		t.Fatalf("following the link: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the signed link answered %d, so a browser handed it downloads nothing", resp.StatusCode)
	}
}

func TestASignedLinkCanBeGivenOutToUploadWith(t *testing.T) {
	// The other direction: a browser uploads straight to the bucket rather
	// than through the service.
	c := liveBucket(t)
	ctx := context.Background()
	key := objectKey(t)

	answer, err := c.Call(ctx, "presign_put", map[string]interface{}{
		"key": key, "expires": "5m",
	})
	if err != nil {
		t.Fatalf("presign_put: %v", err)
	}

	link := linkOf(answer)
	if link == "" {
		t.Fatalf("answer = %v, want a link", answer)
	}

	request, err := http.NewRequest(http.MethodPut, link, strings.NewReader(`{"sku":"WIDGET-1"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("uploading: %v", err)
	}
	defer resp.Body.Close()
	t.Cleanup(func() { _, _ = c.Call(ctx, "delete", map[string]interface{}{"key": key}) })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the upload answered %d", resp.StatusCode)
	}

	// And what was uploaded is what the bucket holds.
	rows, err := c.Read(ctx, connector.Query{Target: key})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows.Rows) != 1 || rows.Rows[0]["sku"] != "WIDGET-1" {
		t.Errorf("rows = %v", rows)
	}
}

func linkOf(answer interface{}) string {
	switch v := answer.(type) {
	case string:
		return v
	case map[string]interface{}:
		for _, name := range []string{"url", "link", "presigned_url"} {
			if link, ok := v[name].(string); ok {
				return link
			}
		}
	}
	return ""
}

func TestAnOperationNobodyImplementsIsRefused(t *testing.T) {
	c := liveBucket(t)

	if _, err := c.Call(context.Background(), "teleport", map[string]interface{}{"key": "x"}); err == nil {
		t.Error("an operation nothing implements was accepted")
	}
}

func TestListingAPrefixAnswersWithWhatIsUnderIt(t *testing.T) {
	// How a flow walks a folder of files somebody dropped in a bucket.
	c := liveBucket(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("tests/listing-%d/", time.Now().UnixNano())

	for _, name := range []string{"one.json", "two.json"} {
		if _, err := c.Write(ctx, &connector.Data{
			Target: prefix + name,
			Params: map[string]interface{}{"content": map[string]interface{}{"name": name}},
		}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		t.Cleanup(func() {
			_, _ = c.Call(ctx, "delete", map[string]interface{}{"key": prefix + name})
		})
	}

	rows, err := c.Read(ctx, connector.Query{Target: prefix})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows.Rows) != 2 {
		t.Errorf("%d objects, want both under the prefix", len(rows.Rows))
	}
}
