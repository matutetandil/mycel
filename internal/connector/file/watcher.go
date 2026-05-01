package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matutetandil/mycel/internal/flow"
)

// pollLoop runs the file watcher on a ticker interval.
func (c *Connector) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scan(ctx)
		}
	}
}

// seedKnown walks the base path and populates the known map without firing events.
func (c *Connector) seedKnown() {
	basePath := c.config.BasePath
	if basePath == "" {
		return
	}

	filepath.WalkDir(basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(basePath, path)
		if err != nil {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		c.mu.Lock()
		c.known[relPath] = fileState{
			modTime: info.ModTime(),
			size:    info.Size(),
		}
		c.mu.Unlock()

		return nil
	})
}

// scan walks the base path and fires events for new or modified files.
func (c *Connector) scan(ctx context.Context) {
	basePath := c.config.BasePath
	if basePath == "" {
		return
	}

	filepath.WalkDir(basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip files we can't access
		}
		if d.IsDir() {
			return nil
		}

		// Check context before processing each file
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}

		relPath, err := filepath.Rel(basePath, path)
		if err != nil {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Determine event type
		c.mu.RLock()
		prev, exists := c.known[relPath]
		c.mu.RUnlock()

		var event string
		if !exists {
			event = "created"
		} else if info.ModTime() != prev.modTime || info.Size() != prev.size {
			event = "modified"
		} else {
			return nil // unchanged
		}

		// Update known state
		c.mu.Lock()
		c.known[relPath] = fileState{
			modTime: info.ModTime(),
			size:    info.Size(),
		}
		c.mu.Unlock()

		// Find matching handler
		handler := c.matchHandler(relPath)
		if handler == nil {
			return nil
		}

		// Read file content
		format := c.detectFormat(path)
		rows, readErr := c.readFile(path, format, nil)

		// Build input and dispatch
		input := buildWatchInput(relPath, info, event, rows, readErr)
		c.debugGate.Acquire()
		result, handlerErr := handler(ctx, input)
		c.debugGate.Release()
		// Fire deferred on_drop closure (no-op on success).
		flow.FireDropAspect(ctx, result)
		if handlerErr != nil {
			c.logger.Error("file watch handler error",
				"connector", c.name,
				"path", relPath,
				"event", event,
				"error", handlerErr,
			)
		}

		return nil
	})
}

// matchHandler finds the first registered handler whose glob pattern matches the file.
func (c *Connector) matchHandler(relPath string) HandlerFunc {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fileName := filepath.Base(relPath)

	for pattern, handler := range c.handlers {
		// Try matching against filename only (e.g., "*.csv")
		if matched, _ := filepath.Match(pattern, fileName); matched {
			return handler
		}
		// Try matching against full relative path (e.g., "reports/*.csv")
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return handler
		}
		// Handle ** prefix patterns by checking if the suffix matches
		if strings.HasPrefix(pattern, "**/") {
			suffix := pattern[3:]
			if matched, _ := filepath.Match(suffix, fileName); matched {
				return handler
			}
		}
	}

	return nil
}

// buildWatchInput constructs the input map for a file watch event handler.
func buildWatchInput(relPath string, info os.FileInfo, event string, rows []map[string]interface{}, readErr error) map[string]interface{} {
	input := map[string]interface{}{
		"_path":     relPath,
		"_name":     filepath.Base(relPath),
		"_size":     info.Size(),
		"_mod_time": info.ModTime().UTC().Format(time.RFC3339),
		"_event":    event,
	}

	if readErr != nil {
		input["_error"] = readErr.Error()
		return input
	}

	// Flatten single-row results into the input map; multi-row goes as "rows"
	if len(rows) == 1 {
		for k, v := range rows[0] {
			input[k] = v
		}
	} else if len(rows) > 0 {
		input["rows"] = rows
	}

	return input
}
