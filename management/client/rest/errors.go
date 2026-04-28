package rest

import (
	"errors"
	"net/http"
)

// APIError is the typed error returned by Client.NewRequest when
// the server responds with a non-2xx status. It carries both the
// HTTP status code (for typed checks like IsNotFound) and the
// server-supplied message (for human-readable surfacing).
//
// Callers can check the kind via errors.As:
//
//	var apiErr *rest.APIError
//	if errors.As(err, &apiErr) && apiErr.StatusCode == 404 { ... }
//
// or via the helpers below for the common cases.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return http.StatusText(e.StatusCode)
	}
	return e.Message
}

// IsNotFound reports whether err originated from a 404 response.
// Used by reconcilers (e.g. openzro-operator) to distinguish
// "resource genuinely missing → don't retry" from other errors.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsUnauthorized reports whether err originated from a 401 response.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}

// IsForbidden reports whether err originated from a 403 response.
func IsForbidden(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}

// IsConflict reports whether err originated from a 409 response.
func IsConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}
