package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"
)

func TestClassifyError_Nil(t *testing.T) {
	if got := ClassifyError(nil); got != OutcomeSuccess {
		t.Errorf("ClassifyError(nil) = %v, want OutcomeSuccess", got)
	}
}

func TestClassifyError_ContextCanceled(t *testing.T) {
	if got := ClassifyError(context.Canceled); got != OutcomeContextCanceled {
		t.Errorf("ClassifyError(context.Canceled) = %v, want OutcomeContextCanceled", got)
	}
}

func TestClassifyError_WrappedContextCanceled(t *testing.T) {
	err := fmt.Errorf("request failed: %w", context.Canceled)
	if got := ClassifyError(err); got != OutcomeContextCanceled {
		t.Errorf("ClassifyError(wrapped canceled) = %v, want OutcomeContextCanceled", got)
	}
}

func TestClassifyError_DeadlineExceeded(t *testing.T) {
	if got := ClassifyError(context.DeadlineExceeded); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(context.DeadlineExceeded) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_WrappedDeadlineExceeded(t *testing.T) {
	err := fmt.Errorf("timeout: %w", context.DeadlineExceeded)
	if got := ClassifyError(err); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(wrapped deadline) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_NetOpError(t *testing.T) {
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("connection refused"),
	}
	if got := ClassifyError(err); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(net.OpError) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_WrappedNetOpError(t *testing.T) {
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("connection refused"),
	}
	err := fmt.Errorf("send failed: %w", opErr)
	if got := ClassifyError(err); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(wrapped net.OpError) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_TLSCertificateError(t *testing.T) {
	err := &tls.CertificateVerificationError{
		UnverifiedCertificates: []*x509.Certificate{},
		Err:                    errors.New("certificate expired"),
	}
	if got := ClassifyError(err); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(tls.CertificateVerificationError) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_EOF(t *testing.T) {
	if got := ClassifyError(io.EOF); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(io.EOF) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_UnexpectedEOF(t *testing.T) {
	if got := ClassifyError(io.ErrUnexpectedEOF); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(io.ErrUnexpectedEOF) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_DNSError(t *testing.T) {
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{
			Err:  "no such host",
			Name: "example.invalid",
		},
	}
	if got := ClassifyError(err); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(DNS error) = %v, want OutcomeConnectionError", got)
	}
}

func TestClassifyError_CatchAll(t *testing.T) {
	// An unknown error with no HTTP response should be classified as connection error.
	err := errors.New("some unknown transport error")
	if got := ClassifyError(err); got != OutcomeConnectionError {
		t.Errorf("ClassifyError(unknown error) = %v, want OutcomeConnectionError", got)
	}
}

// Feature: agent-health-checks, Property 10: Error classification correctness
// **Validates: Requirements HLTH-8.1, HLTH-8.2, HLTH-8.3, HLTH-8.4**

// categorizedError pairs a generated error with its expected classification outcome.
type categorizedError struct {
	err      error
	expected ConnectionOutcome
	category string
}

// genCategorizedError generates errors from all classification categories paired
// with their expected outcomes. This ensures the property test covers nil,
// context.Canceled (and wrapped), context.DeadlineExceeded, net.OpError variants,
// tls.CertificateVerificationError, io.EOF, io.ErrUnexpectedEOF, and custom errors.
func genCategorizedError() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		// Select a category randomly from the full set
		categories := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		idx := int(params.NextInt64()%int64(len(categories)))
		if idx < 0 {
			idx = -idx
		}

		var ce categorizedError

		switch categories[idx] {
		case 0:
			// nil → OutcomeSuccess
			ce = categorizedError{
				err:      nil,
				expected: OutcomeSuccess,
				category: "nil",
			}

		case 1:
			// context.Canceled → OutcomeContextCanceled
			ce = categorizedError{
				err:      context.Canceled,
				expected: OutcomeContextCanceled,
				category: "context.Canceled",
			}

		case 2:
			// Wrapped context.Canceled → OutcomeContextCanceled
			depth := int(params.NextInt64()%3) + 1
			if depth < 1 {
				depth = 1
			}
			err := fmt.Errorf("wrap-1: %w", context.Canceled)
			for i := 1; i < depth; i++ {
				err = fmt.Errorf("wrap-%d: %w", i+1, err)
			}
			ce = categorizedError{
				err:      err,
				expected: OutcomeContextCanceled,
				category: "wrapped context.Canceled",
			}

		case 3:
			// context.DeadlineExceeded → OutcomeConnectionError
			ce = categorizedError{
				err:      context.DeadlineExceeded,
				expected: OutcomeConnectionError,
				category: "context.DeadlineExceeded",
			}

		case 4:
			// net.OpError (connection refused) → OutcomeConnectionError
			ops := []string{"dial", "read", "write"}
			opIdx := int(params.NextInt64()%int64(len(ops)))
			if opIdx < 0 {
				opIdx = -opIdx
			}
			ce = categorizedError{
				err: &net.OpError{
					Op:  ops[opIdx],
					Net: "tcp",
					Err: errors.New("connection refused"),
				},
				expected: OutcomeConnectionError,
				category: "net.OpError",
			}

		case 5:
			// net.OpError with DNS error → OutcomeConnectionError
			ce = categorizedError{
				err: &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: &net.DNSError{
						Err:  "no such host",
						Name: "agent.invalid",
					},
				},
				expected: OutcomeConnectionError,
				category: "net.OpError/DNS",
			}

		case 6:
			// tls.CertificateVerificationError → OutcomeConnectionError
			ce = categorizedError{
				err: &tls.CertificateVerificationError{
					UnverifiedCertificates: []*x509.Certificate{},
					Err:                    errors.New("certificate has expired"),
				},
				expected: OutcomeConnectionError,
				category: "tls.CertificateVerificationError",
			}

		case 7:
			// io.EOF → OutcomeConnectionError
			ce = categorizedError{
				err:      io.EOF,
				expected: OutcomeConnectionError,
				category: "io.EOF",
			}

		case 8:
			// io.ErrUnexpectedEOF → OutcomeConnectionError
			ce = categorizedError{
				err:      io.ErrUnexpectedEOF,
				expected: OutcomeConnectionError,
				category: "io.ErrUnexpectedEOF",
			}

		case 9:
			// Custom/unknown error → OutcomeConnectionError (catch-all per HLTH-8.4)
			msgs := []string{
				"unknown transport failure",
				"something went wrong",
				"unexpected protocol error",
				"remote closed stream",
			}
			msgIdx := int(params.NextInt64()%int64(len(msgs)))
			if msgIdx < 0 {
				msgIdx = -msgIdx
			}
			ce = categorizedError{
				err:      errors.New(msgs[msgIdx]),
				expected: OutcomeConnectionError,
				category: "custom/unknown error",
			}

		case 10:
			// Wrapped net.OpError → OutcomeConnectionError
			opErr := &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("network unreachable"),
			}
			ce = categorizedError{
				err:      fmt.Errorf("request failed: %w", opErr),
				expected: OutcomeConnectionError,
				category: "wrapped net.OpError",
			}
		}

		return gopter.NewGenResult(ce, gopter.NoShrinker)
	}
}

func TestPropertyErrorClassification(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("ClassifyError returns correct outcome for all error categories", prop.ForAll(
		func(ce categorizedError) bool {
			got := ClassifyError(ce.err)
			return got == ce.expected
		},
		genCategorizedError(),
	))

	properties.TestingRun(t)
}
