package typesafe

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy configures automatic retry behavior for transient HTTP and network failures.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts after the initial request (default: 2; 0 disables retries).
	MaxRetries int

	// BackoffInitial is the base delay before the first retry (default: 500ms).
	BackoffInitial time.Duration

	// BackoffMax is the maximum exponential backoff delay (default: 5s).
	BackoffMax time.Duration

	// BackoffJitter is the fraction [0, 1] of each backoff delay randomly subtracted (default: 0.25).
	BackoffJitter float64

	// HTTPStatuses is the set of HTTP status codes eligible for retry (default: 408, 429, and 500..599).
	HTTPStatuses map[int]bool

	// RespectRetryAfter determines whether to honor "retry-after-ms" and "Retry-After" response headers (default: true).
	RespectRetryAfter bool

	// MaxRetryAfter is the maximum server-requested Retry-After delay honored before falling back to exponential backoff (default: 60s).
	MaxRetryAfter time.Duration

	// APIConnectionError determines whether to retry *APIConnectionError failures (default: true).
	APIConnectionError bool

	// APITimeoutError determines whether to retry *APITimeoutError failures (default: true).
	APITimeoutError bool

	// TotalTimeout is an optional total time budget across the initial attempt and all retry delays (from Python SDK RetryPolicy.timeout).
	// A value <= 0 disables the total budget limit.
	TotalTimeout time.Duration

	// Predicate is an optional custom callback that can return true for any error to trigger a retry in addition to standard rules.
	Predicate func(err error) bool
}

// DefaultHTTPStatuses returns a fresh copy of the default retryable HTTP status set (408, 429, 500..599).
func DefaultHTTPStatuses() map[int]bool {
	m := make(map[int]bool, 102)
	m[http.StatusRequestTimeout] = true  // 408
	m[http.StatusTooManyRequests] = true // 429
	for code := 500; code < 600; code++ {
		m[code] = true // Includes 500, 502, 503, 504, 529 Overloaded, etc.
	}
	return m
}

// DefaultRetryPolicy returns the default TypeSafe SDK RetryPolicy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:         DefaultMaxRetries,
		BackoffInitial:     DefaultBackoffInitial,
		BackoffMax:         DefaultBackoffMax,
		BackoffJitter:      DefaultBackoffJitter,
		HTTPStatuses:       DefaultHTTPStatuses(),
		RespectRetryAfter:  true,
		MaxRetryAfter:      DefaultMaxRetryAfter,
		APIConnectionError: true,
		APITimeoutError:    true,
	}
}

// Validate checks that all fields of RetryPolicy are within valid ranges, returning a *TypeSafeError if invalid.
func (p RetryPolicy) Validate() error {
	if p.MaxRetries < 0 {
		return NewTypeSafeError(fmt.Sprintf("`retry.maxRetries` must be a non-negative integer, got %d.", p.MaxRetries))
	}
	if p.BackoffInitial < 0 {
		return NewTypeSafeError(fmt.Sprintf("`retry.backoffInitial` must be a non-negative duration, got %v.", p.BackoffInitial))
	}
	if p.BackoffMax < 0 {
		return NewTypeSafeError(fmt.Sprintf("`retry.backoffMax` must be a non-negative duration, got %v.", p.BackoffMax))
	}
	if math.IsNaN(p.BackoffJitter) || math.IsInf(p.BackoffJitter, 0) || p.BackoffJitter < 0 || p.BackoffJitter > 1 {
		return NewTypeSafeError(fmt.Sprintf("`retry.backoffJitter` must be between 0 and 1, got %v.", p.BackoffJitter))
	}
	if p.MaxRetryAfter < 0 {
		return NewTypeSafeError(fmt.Sprintf("`retry.maxRetryAfter` must be a non-negative duration, got %v.", p.MaxRetryAfter))
	}
	for status := range p.HTTPStatuses {
		if status < 100 || status > 999 {
			return NewTypeSafeError(fmt.Sprintf("`retry.httpStatuses` must contain HTTP status codes, got %d.", status))
		}
	}
	return nil
}

// Clone returns a deep copy of the RetryPolicy with an isolated HTTPStatuses map.
func (p RetryPolicy) Clone() RetryPolicy {
	cp := p
	if p.HTTPStatuses != nil {
		cp.HTTPStatuses = make(map[int]bool, len(p.HTTPStatuses))
		for k, v := range p.HTTPStatuses {
			cp.HTTPStatuses[k] = v
		}
	} else {
		cp.HTTPStatuses = DefaultHTTPStatuses()
	}
	return cp
}

// IsRetryableStatus checks whether the given HTTP status code is retryable under the policy.
func (p RetryPolicy) IsRetryableStatus(status int) bool {
	if p.HTTPStatuses == nil {
		return DefaultHTTPStatuses()[status]
	}
	return p.HTTPStatuses[status]
}

// ParseRetryAfter parses "retry-after-ms" or "Retry-After" headers into milliseconds,
// preferring "retry-after-ms" (matching both Python and JS SDK behavior).
// Returns nil when neither header contains a valid non-negative delay.
func ParseRetryAfter(headers http.Header, now time.Time) *float64 {
	if headers == nil {
		return nil
	}
	if rawMs := strings.TrimSpace(headers.Get("retry-after-ms")); rawMs != "" {
		if ms, err := strconv.ParseFloat(rawMs, 64); err == nil && !math.IsNaN(ms) && !math.IsInf(ms, 0) && ms >= 0 {
			return &ms
		}
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
		if seconds >= 0 {
			ms := seconds * 1000.0
			return &ms
		}
		return nil
	}
	if parsedDate, err := http.ParseTime(raw); err == nil {
		diffMs := float64(parsedDate.Sub(now).Milliseconds())
		if diffMs < 0 {
			diffMs = 0
		}
		return &diffMs
	}
	return nil
}

// RetryDelay calculates the delay duration for a 0-based retry attempt.
// If RespectRetryAfter is enabled and the server provides a valid Retry-After delay <= MaxRetryAfter,
// that server delay is returned. Otherwise, capped exponential backoff with subtracted jitter is used:
//
//	exponential = min(BackoffInitial * 2^attempt, BackoffMax)
//	delayMs     = round(exponentialMs * (1 - rand() * BackoffJitter))
func RetryDelay(attempt int, headers http.Header, policy RetryPolicy, randFloat func() float64) time.Duration {
	if randFloat == nil {
		randFloat = rand.Float64
	}
	if policy.RespectRetryAfter && headers != nil {
		if retryAfterMs := ParseRetryAfter(headers, time.Now()); retryAfterMs != nil {
			maxMs := float64(policy.MaxRetryAfter.Milliseconds())
			if *retryAfterMs <= maxMs {
				return time.Duration(math.Round(*retryAfterMs)) * time.Millisecond
			}
		}
	}
	initialMs := float64(policy.BackoffInitial.Milliseconds())
	maxMs := float64(policy.BackoffMax.Milliseconds())
	expMs := math.Min(initialMs*math.Pow(2, float64(attempt)), maxMs)
	jitteredMs := math.Round(expMs * (1.0 - randFloat()*policy.BackoffJitter))
	if jitteredMs < 0 {
		jitteredMs = 0
	}
	return time.Duration(jitteredMs) * time.Millisecond
}

// sleepWithContext waits for duration d or returns immediately with *APIUserAbortError if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return NewAPIUserAbortError("", err)
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return NewAPIUserAbortError("", ctx.Err())
	case <-timer.C:
		return nil
	}
}
