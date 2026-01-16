// Package dataloader provides automatic batching for N+1 query prevention.
// It wraps github.com/graph-gophers/dataloader/v7 with Mycel-specific functionality.
package dataloader

import (
	"context"
	"sync"
	"time"

	"github.com/graph-gophers/dataloader/v7"
)

// contextKey is a type for context keys in this package.
type contextKey string

// LoadersKey is the context key for the loader collection.
const LoadersKey contextKey = "dataloaders"

// BatchFunc is a function that loads multiple items by their keys.
// It receives a slice of keys and should return results in the same order.
type BatchFunc[K comparable, V any] func(ctx context.Context, keys []K) ([]V, []error)

// Loader wraps dataloader.Loader with convenience methods.
type Loader[K comparable, V any] struct {
	loader *dataloader.Loader[K, V]
}

// LoaderConfig configures a data loader.
type LoaderConfig struct {
	// BatchSize is the maximum number of keys to batch together (default: 100)
	BatchSize int
	// Wait is the duration to wait before dispatching a batch (default: 1ms)
	Wait time.Duration
	// Cache enables caching of results (default: true)
	Cache bool
}

// DefaultConfig returns default loader configuration.
func DefaultConfig() LoaderConfig {
	return LoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
		Cache:     true,
	}
}

// NewLoader creates a new data loader with the given batch function.
func NewLoader[K comparable, V any](batchFn BatchFunc[K, V], config ...LoaderConfig) *Loader[K, V] {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	// Create batch function wrapper
	batchFunc := func(ctx context.Context, keys []K) []*dataloader.Result[V] {
		results, errors := batchFn(ctx, keys)
		dlResults := make([]*dataloader.Result[V], len(keys))

		for i := range keys {
			var value V
			var err error

			if i < len(results) {
				value = results[i]
			}
			if i < len(errors) {
				err = errors[i]
			}

			dlResults[i] = &dataloader.Result[V]{Data: value, Error: err}
		}

		return dlResults
	}

	// Configure options
	var opts []dataloader.Option[K, V]

	if cfg.BatchSize > 0 {
		opts = append(opts, dataloader.WithBatchCapacity[K, V](cfg.BatchSize))
	}
	if cfg.Wait > 0 {
		opts = append(opts, dataloader.WithWait[K, V](cfg.Wait))
	}
	if !cfg.Cache {
		opts = append(opts, dataloader.WithCache[K, V](&dataloader.NoCache[K, V]{}))
	}

	return &Loader[K, V]{
		loader: dataloader.NewBatchedLoader(batchFunc, opts...),
	}
}

// Load loads a single item by key.
func (l *Loader[K, V]) Load(ctx context.Context, key K) (V, error) {
	thunk := l.loader.Load(ctx, key)
	return thunk()
}

// LoadMany loads multiple items by keys.
func (l *Loader[K, V]) LoadMany(ctx context.Context, keys []K) ([]V, []error) {
	thunk := l.loader.LoadMany(ctx, keys)
	results, errs := thunk()
	return results, errs
}

// Prime primes the cache with a value. This is useful for seeding the cache
// with data loaded by other means.
func (l *Loader[K, V]) Prime(ctx context.Context, key K, value V) {
	l.loader.Prime(ctx, key, value)
}

// Clear removes a key from the cache.
func (l *Loader[K, V]) Clear(ctx context.Context, key K) {
	l.loader.Clear(ctx, key)
}

// ClearAll clears all cached values.
func (l *Loader[K, V]) ClearAll() {
	l.loader.ClearAll()
}

// LoaderCollection manages multiple data loaders for a request.
// Each request should have its own collection to ensure proper batching.
type LoaderCollection struct {
	mu      sync.RWMutex
	loaders map[string]interface{}
}

// NewLoaderCollection creates a new loader collection.
func NewLoaderCollection() *LoaderCollection {
	return &LoaderCollection{
		loaders: make(map[string]interface{}),
	}
}

// Get retrieves a loader by name.
func (c *LoaderCollection) Get(name string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	loader, ok := c.loaders[name]
	return loader, ok
}

// Set stores a loader by name.
func (c *LoaderCollection) Set(name string, loader interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaders[name] = loader
}

// GetOrCreate retrieves a loader or creates it if it doesn't exist.
func GetOrCreate[K comparable, V any](c *LoaderCollection, name string, batchFn BatchFunc[K, V], config ...LoaderConfig) *Loader[K, V] {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.loaders[name]; ok {
		if loader, ok := existing.(*Loader[K, V]); ok {
			return loader
		}
	}

	loader := NewLoader(batchFn, config...)
	c.loaders[name] = loader
	return loader
}

// WithLoaders adds a loader collection to the context.
func WithLoaders(ctx context.Context, loaders *LoaderCollection) context.Context {
	return context.WithValue(ctx, LoadersKey, loaders)
}

// GetLoaders retrieves the loader collection from context.
func GetLoaders(ctx context.Context) *LoaderCollection {
	if loaders, ok := ctx.Value(LoadersKey).(*LoaderCollection); ok {
		return loaders
	}
	return nil
}

// GetOrCreateFromContext retrieves or creates a loader from the context.
func GetOrCreateFromContext[K comparable, V any](ctx context.Context, name string, batchFn BatchFunc[K, V], config ...LoaderConfig) *Loader[K, V] {
	loaders := GetLoaders(ctx)
	if loaders == nil {
		// No collection in context, create standalone loader
		return NewLoader(batchFn, config...)
	}
	return GetOrCreate(loaders, name, batchFn, config...)
}

// SQLBatchLoader is a helper for creating batch loaders for SQL queries.
type SQLBatchLoader struct {
	Query    string // SQL query with IN clause placeholder, e.g., "SELECT * FROM users WHERE id IN (?)"
	KeyField string // Field name for the key in results, e.g., "id"
}

// CreateBatchFunc creates a batch function for SQL queries.
// The execFn should execute the query and return rows.
func (s *SQLBatchLoader) CreateBatchFunc(execFn func(ctx context.Context, query string, keys []interface{}) ([]map[string]interface{}, error)) BatchFunc[interface{}, map[string]interface{}] {
	return func(ctx context.Context, keys []interface{}) ([]map[string]interface{}, []error) {
		if len(keys) == 0 {
			return nil, nil
		}

		// Execute batch query
		rows, err := execFn(ctx, s.Query, keys)
		if err != nil {
			// Return error for all keys
			errors := make([]error, len(keys))
			for i := range keys {
				errors[i] = err
			}
			return nil, errors
		}

		// Index results by key
		resultsByKey := make(map[interface{}]map[string]interface{})
		for _, row := range rows {
			if key, ok := row[s.KeyField]; ok {
				resultsByKey[key] = row
			}
		}

		// Return results in key order
		results := make([]map[string]interface{}, len(keys))
		for i, key := range keys {
			results[i] = resultsByKey[key] // Will be nil if not found
		}

		return results, nil
	}
}

// SQLManyBatchLoader is a helper for loading multiple items by a foreign key.
// For example: loading all orders for multiple users in a single query.
type SQLManyBatchLoader struct {
	Query       string // SQL query with IN clause placeholder, e.g., "SELECT * FROM orders WHERE user_id IN (?)"
	ForeignKey  string // Field name for the foreign key in results, e.g., "user_id"
	OrderBy     string // Optional: ORDER BY clause to append
}

// CreateBatchFunc creates a batch function that returns slices of results per key.
func (s *SQLManyBatchLoader) CreateBatchFunc(execFn func(ctx context.Context, query string, keys []interface{}) ([]map[string]interface{}, error)) BatchFunc[interface{}, []map[string]interface{}] {
	return func(ctx context.Context, keys []interface{}) ([][]map[string]interface{}, []error) {
		if len(keys) == 0 {
			return nil, nil
		}

		// Execute batch query
		rows, err := execFn(ctx, s.Query, keys)
		if err != nil {
			// Return error for all keys
			errors := make([]error, len(keys))
			for i := range keys {
				errors[i] = err
			}
			return nil, errors
		}

		// Group results by foreign key
		resultsByKey := make(map[interface{}][]map[string]interface{})
		for _, row := range rows {
			if key, ok := row[s.ForeignKey]; ok {
				resultsByKey[key] = append(resultsByKey[key], row)
			}
		}

		// Return results in key order (empty slices for keys with no results)
		results := make([][]map[string]interface{}, len(keys))
		for i, key := range keys {
			if items, ok := resultsByKey[key]; ok {
				results[i] = items
			} else {
				results[i] = []map[string]interface{}{} // Empty slice, not nil
			}
		}

		return results, nil
	}
}

// LoaderKey generates a unique key for a loader based on table and operation.
func LoaderKey(table, operation string) string {
	return table + ":" + operation
}
