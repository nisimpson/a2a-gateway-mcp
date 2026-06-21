package health

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
)

// ConnectionOutcome represents the result of a connection attempt.
type ConnectionOutcome int

const (
	// OutcomeSuccess indicates an HTTP response was received (any status code).
	OutcomeSuccess ConnectionOutcome = iota
	// OutcomeConnectionError indicates a transport-level failure.
	OutcomeConnectionError
	// OutcomeContextCanceled indicates client-initiated cancellation.
	OutcomeContextCanceled
)

// ClassifyError determines whether an error represents a connection failure,
// a client cancellation, or a successful connection. If err is nil, returns
// OutcomeSuccess.
//
// Classification rules:
//   - nil → OutcomeSuccess
//   - context.Canceled (not DeadlineExceeded) → OutcomeContextCanceled
//   - context.DeadlineExceeded → OutcomeConnectionError
//   - net.OpError → OutcomeConnectionError
//   - *tls.CertificateVerificationError → OutcomeConnectionError
//   - io.EOF / io.UnexpectedEOF → OutcomeConnectionError
//   - Any other non-nil error → OutcomeConnectionError (catch-all)
func ClassifyError(err error) ConnectionOutcome {
	if err == nil {
		return OutcomeSuccess
	}

	// Check for context.Canceled first (before DeadlineExceeded, since
	// DeadlineExceeded is also a context error but has different semantics).
	// context.Canceled means the MCP client disconnected — not the agent's fault.
	if errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return OutcomeContextCanceled
	}

	// context.DeadlineExceeded means the request to the agent timed out.
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeConnectionError
	}

	// net.OpError covers TCP connection refused, DNS resolution failure,
	// and network unreachable errors.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return OutcomeConnectionError
	}

	// TLS handshake failures.
	var tlsCertErr *tls.CertificateVerificationError
	if errors.As(err, &tlsCertErr) {
		return OutcomeConnectionError
	}

	// net.Error covers additional network-level errors (e.g., timeout interface).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return OutcomeConnectionError
	}

	// EOF errors indicate connection reset or incomplete response.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return OutcomeConnectionError
	}

	// Catch-all: any other error with no HTTP response is treated as a
	// connection error per HLTH-8.4.
	return OutcomeConnectionError
}
