package platform

import (
	"errors"
	"net/http"
)

// ClientError is an error that is safe to forward to API clients. It carries
// an HTTP status code and a message that reveals no internal system state.
//
// The internal field holds the underlying error for logging — it is never
// serialized or sent to clients. This explicit boundary means internal errors
// (database failures, unexpected nil values, dependency errors) cannot leak
// through API responses by accident.
type ClientError struct {
	HTTPStatus int
	Message    string
	internal   error
}

func (e *ClientError) Error() string { return e.Message }

// Unwrap exposes the internal error for errors.Is and errors.As traversal.
// The internal error is for logging only — never forward it to clients.
func (e *ClientError) Unwrap() error { return e.internal }

// Sentinel errors for common HTTP failure cases.
// Use Wrap to attach an internal error for logging context.
var (
	ErrUnauthenticated = &ClientError{HTTPStatus: http.StatusUnauthorized, Message: "authentication required"}
	ErrForbidden       = &ClientError{HTTPStatus: http.StatusForbidden, Message: "forbidden"}
	ErrNotFound        = &ClientError{HTTPStatus: http.StatusNotFound, Message: "not found"}
	ErrConflict        = &ClientError{HTTPStatus: http.StatusConflict, Message: "conflict"}
	ErrBadRequest      = &ClientError{HTTPStatus: http.StatusBadRequest, Message: "bad request"}
	ErrTooManyRequests = &ClientError{HTTPStatus: http.StatusTooManyRequests, Message: "rate limit exceeded"}
)

// Wrap creates a ClientError that pairs a client-safe response with an internal
// error for logging. The safe message comes from the sentinel; the internal
// error carries full context.
//
//	return platform.Wrap(platform.ErrNotFound, fmt.Errorf("customer %d: %w", id, err))
func Wrap(safe *ClientError, internal error) *ClientError {
	return &ClientError{
		HTTPStatus: safe.HTTPStatus,
		Message:    safe.Message,
		internal:   internal,
	}
}

// Internal creates a 500 ClientError wrapping an unexpected internal error.
// The client sees only "an unexpected error occurred".
func Internal(err error) *ClientError {
	return &ClientError{
		HTTPStatus: http.StatusInternalServerError,
		Message:    "an unexpected error occurred",
		internal:   err,
	}
}

// NotFound creates a 404 ClientError with a specific not-found message.
// Use this when the resource type is safe to name in a response.
func NotFound(resource string) *ClientError {
	return &ClientError{
		HTTPStatus: http.StatusNotFound,
		Message:    resource + " not found",
	}
}

// AsClientError extracts a ClientError from the error chain.
// Returns the ClientError and true if one is present; nil and false otherwise.
func AsClientError(err error) (*ClientError, bool) {
	var ce *ClientError
	return ce, errors.As(err, &ce)
}
