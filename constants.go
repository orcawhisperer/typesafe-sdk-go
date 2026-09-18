package typesafe

import "time"

// Environment variable names recognized by the TypeSafe Go SDK.
const (
	// APIKeyEnv is the environment variable for the TypeSafe API key (required if not passed to NewClient).
	APIKeyEnv = "TYPESAFE_API_KEY"

	// BaseURLEnv is the environment variable for overriding the API root URL.
	BaseURLEnv = "TYPESAFE_BASE_URL"

	// DefaultModelEnv is the environment variable for overriding the default model name.
	DefaultModelEnv = "TYPESAFE_DEFAULT_MODEL"

	// LogLevelEnv is the environment variable for configuring the SDK log verbosity.
	LogLevelEnv = "TYPESAFE_LOG_LEVEL"
)

// ENV groups the environment variable names recognized by the SDK, mirroring the JS/TS SDK's ENV object.
var ENV = struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	LogLevel     string
}{
	APIKey:       APIKeyEnv,
	BaseURL:      BaseURLEnv,
	DefaultModel: DefaultModelEnv,
	LogLevel:     LogLevelEnv,
}

// Default client settings.
const (
	// DefaultBaseURL is the default TypeSafe AI API root endpoint.
	DefaultBaseURL = "https://api.typesafe.ai"

	// DefaultModel is the default System One model alias used when a request omits Model.
	DefaultModel = "jev-latest"

	// DefaultTimeout is the default timeout per HTTP attempt (10 seconds).
	DefaultTimeout = 10 * time.Second

	// DefaultMaxRetries is the default number of retry attempts after the initial request (2 retries = up to 3 total attempts).
	DefaultMaxRetries = 2

	// DefaultBackoffInitial is the initial exponential backoff delay (500ms).
	DefaultBackoffInitial = 500 * time.Millisecond

	// DefaultBackoffMax is the maximum exponential backoff delay (5s).
	DefaultBackoffMax = 5000 * time.Millisecond

	// DefaultBackoffJitter is the fraction of each backoff delay randomly subtracted (0.25 = up to 25% subtracted).
	DefaultBackoffJitter = 0.25

	// DefaultMaxRetryAfter is the maximum server-requested Retry-After delay honored before falling back to exponential backoff (60s).
	DefaultMaxRetryAfter = 60 * time.Second
)

// Known TypeSafe model names and aliases.
const (
	// ModelJevLatest points to the most recent stable, official release of Jev.
	ModelJevLatest = "jev-latest"

	// ModelJevPreview points to the most recent release of Jev, including preview builds when available.
	ModelJevPreview = "jev-preview"

	// ModelJev1_13_0 is the pinned versioned ID for Jev 1.13.0.
	ModelJev1_13_0 = "jev-1.13.0"
)
