package typesafe

import (
	"net/http"
	"time"
)

// ClientConfig holds configuration parameters for constructing a TypeSafe Client.
// Callers can configure a Client via functional ClientOption helpers (WithAPIKey, WithBaseURL, etc.)
// or by passing WithConfig(ClientConfig{...}).
type ClientConfig struct {
	// APIKey is the TypeSafe API key. Falls back to the TYPESAFE_API_KEY environment variable.
	APIKey string

	// BaseURL is the API root URL. Falls back to TYPESAFE_BASE_URL, then DefaultBaseURL ("https://api.typesafe.ai").
	BaseURL string

	// DefaultModel is the model name used when a request omits Model. Falls back to TYPESAFE_DEFAULT_MODEL, then DefaultModel ("jev-latest").
	DefaultModel string

	// Timeout is the per-attempt HTTP round-trip timeout. Defaults to DefaultTimeout (10s).
	Timeout time.Duration
	timeoutSet bool

	// Retry configures the client-wide RetryPolicy.
	Retry *RetryPolicy

	// DefaultHeaders specifies additional headers sent with every request.
	DefaultHeaders map[string]string

	// HTTPClient allows supplying a custom *http.Client. Mutually exclusive with Transport.
	HTTPClient *http.Client

	// Transport allows supplying a custom http.RoundTripper. Mutually exclusive with HTTPClient.
	Transport http.RoundTripper

	// Logger provides a custom Logger implementation.
	Logger Logger

	// LogLevel sets the log verbosity ("debug", "info", "warn", "error", "off"). Falls back to TYPESAFE_LOG_LEVEL, then "warn".
	LogLevel LogLevel
	logLevelSet bool
}

// ClientOption configures a Client during NewClient initialization.
type ClientOption func(*ClientConfig)

// WithConfig applies an entire ClientConfig struct to the client builder.
func WithConfig(cfg ClientConfig) ClientOption {
	return func(c *ClientConfig) {
		*c = cfg
		if cfg.Timeout != 0 {
			c.timeoutSet = true
		}
		if cfg.LogLevel != "" {
			c.logLevelSet = true
		}
	}
}

// WithAPIKey sets the TypeSafe API key (`Authorization: Bearer <key>`).
func WithAPIKey(apiKey string) ClientOption {
	return func(c *ClientConfig) {
		c.APIKey = apiKey
	}
}

// WithBaseURL sets the TypeSafe API root URL (trailing slashes are automatically stripped).
func WithBaseURL(baseURL string) ClientOption {
	return func(c *ClientConfig) {
		c.BaseURL = baseURL
	}
}

// WithDefaultModel sets the default model used when SystemOneRequest.Model is omitted.
func WithDefaultModel(model string) ClientOption {
	return func(c *ClientConfig) {
		c.DefaultModel = model
	}
}

// WithTimeout sets the per-attempt timeout for HTTP operations.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *ClientConfig) {
		c.Timeout = timeout
		c.timeoutSet = true
	}
}

// WithRetryPolicy sets the client-level RetryPolicy. Pass RetryPolicy{MaxRetries: 0} or WithMaxRetries(0) to disable retries.
func WithRetryPolicy(policy RetryPolicy) ClientOption {
	return func(c *ClientConfig) {
		cp := policy.Clone()
		c.Retry = &cp
	}
}

// WithMaxRetries is a convenience option to set the maximum retry count on the client's retry policy.
func WithMaxRetries(maxRetries int) ClientOption {
	return func(c *ClientConfig) {
		if c.Retry == nil {
			def := DefaultRetryPolicy()
			c.Retry = &def
		}
		c.Retry.MaxRetries = maxRetries
	}
}

// WithDefaultHeaders configures additional HTTP headers sent with every request.
func WithDefaultHeaders(headers map[string]string) ClientOption {
	return func(c *ClientConfig) {
		if c.DefaultHeaders == nil {
			c.DefaultHeaders = make(map[string]string, len(headers))
		}
		for k, v := range headers {
			c.DefaultHeaders[k] = v
		}
	}
}

// WithHTTPClient sets a custom *http.Client. Mutually exclusive with WithTransport.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *ClientConfig) {
		c.HTTPClient = httpClient
	}
}

// WithTransport sets a custom http.RoundTripper. Mutually exclusive with WithHTTPClient.
func WithTransport(transport http.RoundTripper) ClientOption {
	return func(c *ClientConfig) {
		c.Transport = transport
	}
}

// WithLogger sets a custom Logger implementation.
func WithLogger(logger Logger) ClientOption {
	return func(c *ClientConfig) {
		c.Logger = logger
	}
}

// WithLogLevel sets the SDK log filter level ("debug", "info", "warn", "error", "off").
func WithLogLevel(level LogLevel) ClientOption {
	return func(c *ClientConfig) {
		c.LogLevel = level
		c.logLevelSet = true
	}
}

// ---------------------------------------------------------------------------
// Per-Call Request Options
// ---------------------------------------------------------------------------

// RequestOptions holds per-request overrides for a single API call.
type RequestOptions struct {
	// Timeout overrides the per-attempt timeout for this call only.
	Timeout    time.Duration
	timeoutSet bool

	// Retry overrides the client-level RetryPolicy for this call only.
	Retry *RetryPolicy

	// ExtraHeaders merges additional request headers for this call only (cannot override protected protocol headers).
	ExtraHeaders map[string]string

	// ExtraBody shallow-merges additional top-level JSON fields over the request body for this call only.
	ExtraBody map[string]any

	// Model overrides the model for this call only.
	Model string
}

// RequestOption configures per-call overrides on SystemOne, Ask, or Models.List calls.
type RequestOption func(*RequestOptions)

// WithRequestTimeout overrides the per-attempt timeout for a single API call.
func WithRequestTimeout(timeout time.Duration) RequestOption {
	return func(o *RequestOptions) {
		o.Timeout = timeout
		o.timeoutSet = true
	}
}

// WithRequestRetryPolicy overrides the RetryPolicy for a single API call.
func WithRequestRetryPolicy(policy RetryPolicy) RequestOption {
	return func(o *RequestOptions) {
		cp := policy.Clone()
		o.Retry = &cp
	}
}

// WithExtraHeaders sets additional HTTP headers for a single API call.
func WithExtraHeaders(headers map[string]string) RequestOption {
	return func(o *RequestOptions) {
		if o.ExtraHeaders == nil {
			o.ExtraHeaders = make(map[string]string, len(headers))
		}
		for k, v := range headers {
			o.ExtraHeaders[k] = v
		}
	}
}

// WithExtraBody sets additional top-level request body fields for a single SystemOne call.
func WithExtraBody(extra map[string]any) RequestOption {
	return func(o *RequestOptions) {
		if o.ExtraBody == nil {
			o.ExtraBody = make(map[string]any, len(extra))
		}
		for k, v := range extra {
			o.ExtraBody[k] = v
		}
	}
}

// WithRequestModel overrides the model for a single SystemOne or Ask call.
func WithRequestModel(model string) RequestOption {
	return func(o *RequestOptions) {
		o.Model = model
	}
}
