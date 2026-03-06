package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type CapturedRequest struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	Timestamp string            `json:"timestamp"`
}

var (
	requests []CapturedRequest
	mu       sync.Mutex
)

func main() {
	mux := http.NewServeMux()

	// GET /requests - Return all captured requests (with optional path filter)
	mux.HandleFunc("GET /requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		pathFilter := r.URL.Query().Get("path")
		var result []CapturedRequest

		if pathFilter != "" {
			for _, req := range requests {
				if strings.HasPrefix(req.Path, pathFilter) {
					result = append(result, req)
				}
			}
		} else {
			result = requests
		}

		if result == nil {
			result = []CapturedRequest{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// DELETE /requests - Clear all captured requests
	mux.HandleFunc("DELETE /requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = nil
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cleared": true}`))
	})

	// GET /health - Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ok"}`))
	})

	// Catch-all: capture any other request
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		headers := make(map[string]string)
		for k, v := range r.Header {
			headers[k] = strings.Join(v, ", ")
		}

		captured := CapturedRequest{
			Method:    r.Method,
			Path:      r.URL.Path,
			Headers:   headers,
			Body:      string(body),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		mu.Lock()
		requests = append(requests, captured)
		mu.Unlock()

		log.Printf("Captured: %s %s (%d bytes)", r.Method, r.URL.Path, len(body))

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	})

	fmt.Println("Mock server listening on :8888")
	log.Fatal(http.ListenAndServe(":8888", mux))
}
