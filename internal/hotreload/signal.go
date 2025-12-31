package hotreload

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// SignalHandler handles OS signals for hot reload.
type SignalHandler struct {
	logger  *slog.Logger
	watcher *Watcher
	signals chan os.Signal
	done    chan struct{}
}

// NewSignalHandler creates a new signal handler.
func NewSignalHandler(watcher *Watcher, logger *slog.Logger) *SignalHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SignalHandler{
		logger:  logger,
		watcher: watcher,
		signals: make(chan os.Signal, 1),
		done:    make(chan struct{}),
	}
}

// Start starts listening for SIGHUP signals.
func (h *SignalHandler) Start(ctx context.Context) {
	signal.Notify(h.signals, syscall.SIGHUP)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.done:
				return
			case sig := <-h.signals:
				if sig == syscall.SIGHUP {
					h.logger.Info("received SIGHUP, reloading configuration")
					if err := h.watcher.TriggerReload(ctx); err != nil {
						h.logger.Error("reload triggered by SIGHUP failed", "error", err)
					}
				}
			}
		}
	}()

	h.logger.Debug("signal handler started, listening for SIGHUP")
}

// Stop stops the signal handler.
func (h *SignalHandler) Stop() {
	signal.Stop(h.signals)
	close(h.done)
}
