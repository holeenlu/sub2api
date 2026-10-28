package host

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ServeHTTP owns and joins the listener goroutine. Bind failures return to the
// composition root so its cleanup executes. On cancellation it stops HTTP
// admission, waits within drainTimeout, then closes remaining HTTP connections.
// Hijacked WS connections are NOT owned by net/http: their registered owner must
// separately drain/cancel/join them before dependencies and usage workers stop.
func ServeHTTP(ctx context.Context, server *http.Server, drainTimeout time.Duration) error {
	if drainTimeout <= 0 {
		return errors.New("HTTP drain timeout must be positive")
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	err := server.Shutdown(drainCtx)
	if err != nil {
		err = errors.Join(err, server.Close())
	}
	serveErr := <-done
	if !errors.Is(serveErr, http.ErrServerClosed) {
		err = errors.Join(err, serveErr)
	}
	return err
}
