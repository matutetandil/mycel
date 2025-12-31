package memory

import (
	"context"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector/cache/types"
)

func TestMemoryCache_Basic(t *testing.T) {
	config := &types.Config{
		MaxItems: 100,
		Eviction: "lru",
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	// Test Set and Get
	key := "test_key"
	value := []byte("test_value")

	if err := cache.Set(ctx, key, value, 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, found, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("Expected key to be found")
	}
	if string(got) != string(value) {
		t.Errorf("Got %q, want %q", got, value)
	}
}

func TestMemoryCache_TTL(t *testing.T) {
	config := &types.Config{
		MaxItems: 100,
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	key := "ttl_key"
	value := []byte("ttl_value")
	ttl := 100 * time.Millisecond

	if err := cache.Set(ctx, key, value, ttl); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should exist immediately
	_, found, _ := cache.Get(ctx, key)
	if !found {
		t.Fatal("Key should exist immediately after set")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should not exist after TTL
	_, found, _ = cache.Get(ctx, key)
	if found {
		t.Fatal("Key should not exist after TTL expiry")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	config := &types.Config{
		MaxItems: 100,
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	key := "delete_key"
	value := []byte("delete_value")

	if err := cache.Set(ctx, key, value, 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify key exists
	_, found, _ := cache.Get(ctx, key)
	if !found {
		t.Fatal("Key should exist after set")
	}

	// Delete the key
	if err := cache.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify key is deleted
	_, found, _ = cache.Get(ctx, key)
	if found {
		t.Fatal("Key should not exist after delete")
	}
}

func TestMemoryCache_DeletePattern(t *testing.T) {
	config := &types.Config{
		MaxItems: 100,
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	// Set multiple keys with a pattern
	keys := []string{"users:1", "users:2", "users:3", "products:1", "products:2"}
	for _, key := range keys {
		if err := cache.Set(ctx, key, []byte("value"), 0); err != nil {
			t.Fatalf("Set failed for %s: %v", key, err)
		}
	}

	// Delete all users:* keys
	if err := cache.DeletePattern(ctx, "users:*"); err != nil {
		t.Fatalf("DeletePattern failed: %v", err)
	}

	// Verify users keys are deleted
	for _, key := range []string{"users:1", "users:2", "users:3"} {
		_, found, _ := cache.Get(ctx, key)
		if found {
			t.Errorf("Key %s should be deleted", key)
		}
	}

	// Verify products keys still exist
	for _, key := range []string{"products:1", "products:2"} {
		_, found, _ := cache.Get(ctx, key)
		if !found {
			t.Errorf("Key %s should still exist", key)
		}
	}
}

func TestMemoryCache_Exists(t *testing.T) {
	config := &types.Config{
		MaxItems: 100,
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	key := "exists_key"

	// Should not exist before set
	exists, err := cache.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Fatal("Key should not exist before set")
	}

	// Set the key
	if err := cache.Set(ctx, key, []byte("value"), 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should exist after set
	exists, err = cache.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatal("Key should exist after set")
	}
}

func TestMemoryCache_Prefix(t *testing.T) {
	config := &types.Config{
		MaxItems: 100,
		Prefix:   "myapp",
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	key := "user:1"
	value := []byte("test_value")

	if err := cache.Set(ctx, key, value, 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should be able to get with the same key
	got, found, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("Expected key to be found")
	}
	if string(got) != string(value) {
		t.Errorf("Got %q, want %q", got, value)
	}

	// Verify the internal key has the prefix
	internalKeys := cache.Keys()
	foundPrefixed := false
	for _, k := range internalKeys {
		if k == "myapp:user:1" {
			foundPrefixed = true
			break
		}
	}
	if !foundPrefixed {
		t.Errorf("Expected internal key with prefix 'myapp:', got keys: %v", internalKeys)
	}
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	config := &types.Config{
		MaxItems: 3,
		Eviction: "lru",
	}

	cache := New("test_cache", config)

	ctx := context.Background()
	if err := cache.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer cache.Close(ctx)

	// Set 3 keys (fill the cache)
	for i := 1; i <= 3; i++ {
		key := string(rune('a' + i - 1))
		if err := cache.Set(ctx, key, []byte("value"), 0); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
	}

	// Access key "a" to make it recently used
	cache.Get(ctx, "a")

	// Add a 4th key, should evict the least recently used (b)
	if err := cache.Set(ctx, "d", []byte("value"), 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify "a" still exists (was recently used)
	_, found, _ := cache.Get(ctx, "a")
	if !found {
		t.Fatal("Key 'a' should still exist (recently used)")
	}

	// Verify "d" exists (just added)
	_, found, _ = cache.Get(ctx, "d")
	if !found {
		t.Fatal("Key 'd' should exist")
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		key     string
		want    bool
	}{
		// Exact match
		{"users:1", "users:1", true},
		{"users:1", "users:2", false},

		// Suffix wildcard
		{"users:*", "users:1", true},
		{"users:*", "users:123", true},
		{"users:*", "products:1", false},

		// Prefix wildcard
		{"*:active", "users:active", true},
		{"*:active", "products:active", true},
		{"*:active", "users:inactive", false},

		// Middle wildcard
		{"users:*:profile", "users:1:profile", true},
		{"users:*:profile", "users:123:profile", true},
		{"users:*:profile", "users:1:settings", false},

		// Multiple wildcards
		{"*:*:data", "users:1:data", true},
		{"*:*:data", "a:b:data", true},
		{"*:*:data", "users:1:info", false},

		// All wildcard
		{"*", "anything", true},
		{"*", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.key, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.key)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.key, got, tt.want)
			}
		})
	}
}
