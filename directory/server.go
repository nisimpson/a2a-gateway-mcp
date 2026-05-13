package directory

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// shutdownTimeout is the maximum time to wait for in-flight requests to complete
// during graceful shutdown.
const shutdownTimeout = 30 * time.Second

// ListenAndServe starts the directory as a standalone HTTP server.
// It blocks until the context is cancelled, then performs graceful shutdown
// with a 30-second timeout for in-flight requests.
func (d *Directory) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: d,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		// Wait for ListenAndServe to return after shutdown.
		serverErr := <-errCh
		if shutdownErr != nil {
			return shutdownErr
		}
		// ErrServerClosed is expected during graceful shutdown.
		if errors.Is(serverErr, http.ErrServerClosed) {
			return nil
		}
		return serverErr
	case err := <-errCh:
		// Server failed to start (e.g., port already in use).
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
