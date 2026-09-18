package typesafe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxRawBodyInMessage = 200

// TypeSafeError is the base error type for all TypeSafe SDK failures.
// Every specific SDK error embeds or unwraps to *TypeSafeError so callers can use
// errors.As(err, &tsErr) to catch any SDK-originated error.
type TypeSafeError struct {
	Message string
	Cause   error
}

// NewTypeSafeError constructs a new base *TypeSafeError.
func NewTypeSafeError(message string, cause ...error) *TypeSafeError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &TypeSafeError{Message: message, Cause: c}
}

func (e *TypeSafeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *TypeSafeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// APIError represents an unsuccessful HTTP response returned by the TypeSafe API.
// Specific status codes (400, 401, 403, 404, 422, 429, 5xx) embed *APIError and also unwrap to it.
type APIError struct {
	TypeSafeError

	// Status is the HTTP response status code (e.g., 401, 422, 429, 529).
	Status int

	// Headers are the HTTP response headers.
	Headers http.Header

	// Body is the parsed JSON body (map[string]any or []any), plain response string, or nil if empty.
	Body any

	// RawBody is the raw byte slice of the response body.
	RawBody []byte

	// Endpoint is the HTTP method and URL (without credentials, query parameters, or fragment).
	Endpoint string

	// RequestID is the value of the "x-typesafe-request-id" response header, or "" if absent.
	RequestID string
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return &e.TypeSafeError
}

// NewAPIError constructs an *APIError with a formatted diagnostic message.
func NewAPIError(status int, body any, rawBody []byte, headers http.Header, endpoint string, message string) *APIError {
	if message == "" {
		message = describeAPIError(status, body, rawBody)
	}
	reqID := requestIDFromHeader(headers)
	return &APIError{
		TypeSafeError: TypeSafeError{Message: message},
		Status:        status,
		Headers:       headers,
		Body:          body,
		RawBody:       rawBody,
		Endpoint:      endpoint,
		RequestID:     reqID,
	}
}

func describeAPIError(status int, body any, rawBody []byte) string {
	if detail := extractErrorMessage(body); detail != "" {
		return fmt.Sprintf("%d %s", status, detail)
	}
	if body == nil && len(rawBody) == 0 {
		return fmt.Sprintf("%d status code (no body)", status)
	}
	var raw string
	if s, ok := body.(string); ok {
		raw = s
	} else if len(rawBody) > 0 {
		raw = string(rawBody)
	} else {
		b, _ := json.Marshal(body)
		raw = string(b)
	}
	runes := []rune(raw)
	if len(runes) > maxRawBodyInMessage {
		raw = string(runes[:maxRawBodyInMessage]) + "…"
	}
	return fmt.Sprintf("%d %s", status, raw)
}

func extractErrorMessage(body any) string {
	switch v := body.(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["error"].(string); ok && s != "" {
			return s
		}
		if errObj, ok := v["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && msg != "" {
				return msg
			}
		}
		if s, ok := v["message"].(string); ok && s != "" {
			return s
		}
		if s, ok := v["detail"].(string); ok && s != "" {
			return s
		}
		if detailObj, ok := v["detail"].(map[string]any); ok {
			if msg, ok := detailObj["message"].(string); ok && msg != "" {
				return msg
			}
		}
		if detailList, ok := v["detail"].([]any); ok {
			if desc := describeValidationErrors(detailList); desc != "" {
				return desc
			}
		}
	}
	return ""
}

func describeValidationErrors(errs []any) string {
	var parts []string
	for _, item := range errs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		msg, ok := m["msg"].(string)
		if !ok || msg == "" {
			continue
		}
		var locParts []string
		if locSlice, ok := m["loc"].([]any); ok {
			for _, x := range locSlice {
				s := fmt.Sprint(x)
				if s != "body" && s != "" {
					locParts = append(locParts, s)
				}
			}
		}
		if len(locParts) > 0 {
			parts = append(parts, fmt.Sprintf("%s: %s", strings.Join(locParts, "."), msg))
		} else {
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}

// BadRequestError represents HTTP 400 Bad Request.
type BadRequestError struct{ *APIError }

func (e *BadRequestError) Unwrap() error { return e.APIError }

// AuthenticationError represents HTTP 401 Unauthorized.
type AuthenticationError struct{ *APIError }

func (e *AuthenticationError) Unwrap() error { return e.APIError }

// PermissionDeniedError represents HTTP 403 Forbidden.
type PermissionDeniedError struct{ *APIError }

func (e *PermissionDeniedError) Unwrap() error { return e.APIError }

// NotFoundError represents HTTP 404 Not Found.
type NotFoundError struct{ *APIError }

func (e *NotFoundError) Unwrap() error { return e.APIError }

// UnprocessableEntityError represents HTTP 422 Unprocessable Entity (request validation failure).
type UnprocessableEntityError struct{ *APIError }

func (e *UnprocessableEntityError) Unwrap() error { return e.APIError }

// RateLimitError represents HTTP 429 Too Many Requests.
type RateLimitError struct {
	*APIError
	// RetryAfterMs is the server's requested wait in milliseconds, or nil if unavailable/invalid.
	RetryAfterMs *float64
	// RetryAfter is the server's requested wait as a time.Duration (0 if unavailable).
	RetryAfter time.Duration
}

func (e *RateLimitError) Unwrap() error { return e.APIError }

// InternalServerError represents HTTP 5xx responses (including 529 Overloaded).
type InternalServerError struct{ *APIError }

func (e *InternalServerError) Unwrap() error { return e.APIError }

// APIResponseValidationError represents a 2xx HTTP response whose body was missing or had structurally invalid required fields.
type APIResponseValidationError struct {
	*APIError
	// FieldPath is the dotted path to the offending field (e.g., "answers.tone.confidence").
	FieldPath string
}

func (e *APIResponseValidationError) Unwrap() error { return e.APIError }

// NewAPIResponseValidationError creates an *APIResponseValidationError for a specific dotted field path.
func NewAPIResponseValidationError(status int, body any, rawBody []byte, headers http.Header, endpoint, fieldPath, detail string) *APIResponseValidationError {
	msg := fmt.Sprintf("Response validation failed at %q: %s", fieldPath, detail)
	if fieldPath == "" {
		msg = fmt.Sprintf("Response validation failed: %s", detail)
	}
	base := NewAPIError(status, body, rawBody, headers, endpoint, msg)
	return &APIResponseValidationError{
		APIError:  base,
		FieldPath: fieldPath,
	}
}

// APIErrorFromResponse constructs the status-specific error type (*BadRequestError, *AuthenticationError,
// *PermissionDeniedError, *NotFoundError, *UnprocessableEntityError, *RateLimitError, *InternalServerError, or *APIError).
func APIErrorFromResponse(status int, body any, rawBody []byte, headers http.Header, endpoint string) error {
	base := NewAPIError(status, body, rawBody, headers, endpoint, "")
	switch {
	case status == http.StatusBadRequest:
		return &BadRequestError{APIError: base}
	case status == http.StatusUnauthorized:
		return &AuthenticationError{APIError: base}
	case status == http.StatusForbidden:
		return &PermissionDeniedError{APIError: base}
	case status == http.StatusNotFound:
		return &NotFoundError{APIError: base}
	case status == http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{APIError: base}
	case status == http.StatusTooManyRequests:
		retryMs := ParseRetryAfter(headers, time.Now())
		var dur time.Duration
		if retryMs != nil {
			dur = time.Duration(*retryMs * float64(time.Millisecond))
		}
		return &RateLimitError{
			APIError:     base,
			RetryAfterMs: retryMs,
			RetryAfter:   dur,
		}
	case status >= 500:
		return &InternalServerError{APIError: base}
	default:
		return base
	}
}

// APIConnectionError represents a network failure where no complete HTTP response was received
// (DNS failure, TLS error, refused connection, mid-stream body interruption).
type APIConnectionError struct {
	TypeSafeError
}

// NewAPIConnectionError creates an *APIConnectionError.
func NewAPIConnectionError(message string, cause error) *APIConnectionError {
	if message == "" {
		if cause != nil {
			message = fmt.Sprintf("Connection error: %v", cause)
		} else {
			message = "Connection error."
		}
	}
	return &APIConnectionError{
		TypeSafeError: TypeSafeError{Message: message, Cause: cause},
	}
}

func (e *APIConnectionError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.Cause != nil {
		return []error{&e.TypeSafeError, e.Cause}
	}
	return []error{&e.TypeSafeError}
}

// APITimeoutError represents a request attempt that exceeded its configured per-attempt timeout.
// APITimeoutError embeds *APIConnectionError (matching Python & JS SDK hierarchy).
type APITimeoutError struct {
	*APIConnectionError
	// TimeoutDuration is the configured per-attempt timeout duration.
	TimeoutDuration time.Duration
	// TimeoutMs is the configured per-attempt timeout in milliseconds.
	TimeoutMs int64
}

// NewAPITimeoutError creates an *APITimeoutError for the given timeout duration.
func NewAPITimeoutError(timeout time.Duration, cause error) *APITimeoutError {
	ms := timeout.Milliseconds()
	msg := fmt.Sprintf("Request timed out after %dms.", ms)
	connErr := &APIConnectionError{
		TypeSafeError: TypeSafeError{Message: msg, Cause: cause},
	}
	return &APITimeoutError{
		APIConnectionError: connErr,
		TimeoutDuration:    timeout,
		TimeoutMs:          ms,
	}
}

func (e *APITimeoutError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.APIConnectionError != nil && e.APIConnectionError.Cause != nil {
		return []error{e.APIConnectionError, &e.APIConnectionError.TypeSafeError, e.APIConnectionError.Cause}
	}
	return []error{e.APIConnectionError, &e.APIConnectionError.TypeSafeError}
}

// Timeout implements net.Error-compatible timeout detection.
func (e *APITimeoutError) Timeout() bool { return true }

// APIUserAbortError is returned when the caller cancels the request via context.Context
// ( either during an HTTP round-trip or while waiting in a retry backoff delay).
type APIUserAbortError struct {
	TypeSafeError
}

// NewAPIUserAbortError creates an *APIUserAbortError wrapping the caller's context error.
func NewAPIUserAbortError(message string, cause error) *APIUserAbortError {
	if message == "" {
		message = "Request was aborted."
	}
	return &APIUserAbortError{
		TypeSafeError: TypeSafeError{Message: message, Cause: cause},
	}
}

func (e *APIUserAbortError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.Cause != nil {
		return []error{&e.TypeSafeError, e.Cause}
	}
	return []error{&e.TypeSafeError}
}

// Type aliases matching the Python SDK exception names for developer convenience.
type (
	TypeSafeAPIError                       = APIError
	TypeSafeBadRequestError                = BadRequestError
	TypeSafeAuthenticationError            = AuthenticationError
	TypeSafePermissionDeniedError          = PermissionDeniedError
	TypeSafeNotFoundError                  = NotFoundError
	TypeSafeUnprocessableEntityError       = UnprocessableEntityError
	TypeSafeRateLimitError                 = RateLimitError
	TypeSafeInternalServerError            = InternalServerError
	TypeSafeAPIConnectionError             = APIConnectionError
	TypeSafeAPITimeoutError                = APITimeoutError
	TypeSafeAPIResponseValidationError     = APIResponseValidationError
)
