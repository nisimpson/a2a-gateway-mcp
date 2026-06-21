package tool

import "errors"

var (
	// ErrStreamTimeout is returned when a stream times out after receiving events.
	ErrStreamTimeout = errors.New("stream timeout")

	// ErrStreamConnectionTimeout is returned when a stream times out before receiving any events.
	ErrStreamConnectionTimeout = errors.New("stream connection timeout")
)
