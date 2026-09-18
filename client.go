package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// TypeSafeClient is an alias for Client matching the Python and JS/TS SDK class name.
type TypeSafeClient = Client

// Client is a thread-safe HTTP client for the TypeSafe AI System One API.
type Client struct {
	apiKey         string
	baseURL        string
	defaultModel   string
	logLevel       LogLevel
	logger         Logger
	retry          RetryPolicy
	timeout        time.Duration
	defaultHeaders map[string]string
	httpClient     *http.Client
	Models         *ModelsService
	requestCount   atomic.Uint64
}

// NewClient creates a new TypeSafe AI Client.
// Explicit ClientOption arguments take precedence over environment variables (TYPESAFE_API_KEY,
// TYPESAFE_BASE_URL, TYPESAFE_DEFAULT_MODEL, TYPESAFE_LOG_LEVEL), which in turn take precedence over SDK defaults.
// Empty or whitespace-only environment variable values are ignored.
//
// Returns a *TypeSafeError if the API key is missing, if both HTTPClient and Transport are supplied,
// or if timeout/retry/logLevel settings are invalid.
func NewClient(opts ...ClientOption) (*Client, error) {
	cfg := ClientConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.HTTPClient != nil && cfg.Transport != nil {
		return nil, NewTypeSafeError("Cannot specify both `HTTPClient` and `Transport` options; they are mutually exclusive.")
	}

	apiKey := fromCodeOrEnv(cfg.APIKey, APIKeyEnv)
	if apiKey == "" {
		return nil, NewTypeSafeError(
			fmt.Sprintf("No API key was provided. Pass `WithAPIKey(...)` to NewClient or set the %s environment variable.", APIKeyEnv),
		)
	}

	rawBaseURL := fromCodeOrEnv(cfg.BaseURL, BaseURLEnv)
	if rawBaseURL == "" {
		rawBaseURL = DefaultBaseURL
	}
	baseURL := strings.TrimRight(rawBaseURL, "/")

	defaultModel := fromCodeOrEnv(cfg.DefaultModel, DefaultModelEnv)
	if defaultModel == "" {
		defaultModel = DefaultModel
	}

	var logLevel LogLevel
	if cfg.logLevelSet {
		parsed, err := ParseLogLevel(string(cfg.LogLevel), "the `LogLevel` option")
		if err != nil {
			return nil, err
		}
		logLevel = parsed
	} else if envLvl := readEnvNonEmpty(LogLevelEnv); envLvl != "" {
		parsed, err := ParseLogLevel(envLvl, LogLevelEnv)
		if err != nil {
			return nil, err
		}
		logLevel = parsed
	} else {
		logLevel = DefaultLogLevel
	}

	timeout := DefaultTimeout
	if cfg.timeoutSet {
		if cfg.Timeout <= 0 {
			return nil, NewTypeSafeError(fmt.Sprintf("`timeout` must be a positive duration, got %v.", cfg.Timeout))
		}
		timeout = cfg.Timeout
	} else if cfg.HTTPClient != nil && cfg.HTTPClient.Timeout > 0 {
		timeout = cfg.HTTPClient.Timeout
	}

	retryPolicy := DefaultRetryPolicy()
	if cfg.Retry != nil {
		if err := cfg.Retry.Validate(); err != nil {
			return nil, err
		}
		retryPolicy = cfg.Retry.Clone()
	}

	defHeaders := make(map[string]string, len(cfg.DefaultHeaders))
	for k, v := range cfg.DefaultHeaders {
		defHeaders[k] = v
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
		if cfg.Transport != nil {
			httpClient.Transport = cfg.Transport
		}
	}

	c := &Client{
		apiKey:         apiKey,
		baseURL:        baseURL,
		defaultModel:   defaultModel,
		logLevel:       logLevel,
		logger:         WithLogLevelFilter(cfg.Logger, logLevel),
		retry:          retryPolicy,
		timeout:        timeout,
		defaultHeaders: defHeaders,
		httpClient:     httpClient,
	}
	c.Models = &ModelsService{client: c}
	return c, nil
}

// MustNewClient is like NewClient but panics if configuration is invalid or the API key is missing.
func MustNewClient(opts ...ClientOption) *Client {
	c, err := NewClient(opts...)
	if err != nil {
		panic(err)
	}
	return c
}

// BaseURL returns the configured API root URL with trailing slashes removed.
func (c *Client) BaseURL() string { return c.baseURL }

// DefaultModel returns the default model used when a request omits Model.
func (c *Client) DefaultModel() string { return c.defaultModel }

// LogLevel returns the configured log filter level.
func (c *Client) LogLevel() LogLevel { return c.logLevel }

// Logger returns the configured Logger filtered to LogLevel.
func (c *Client) Logger() Logger { return c.logger }

// RetryPolicy returns a copy of the client's configured RetryPolicy.
func (c *Client) RetryPolicy() RetryPolicy { return c.retry.Clone() }

// Timeout returns the client's per-attempt timeout duration.
func (c *Client) Timeout() time.Duration { return c.timeout }

// DefaultHeaders returns a copy of the client's configured default headers.
func (c *Client) DefaultHeaders() map[string]string {
	out := make(map[string]string, len(c.defaultHeaders))
	for k, v := range c.defaultHeaders {
		out[k] = v
	}
	return out
}

// Close releases idle network connections on the underlying HTTP transport.
func (c *Client) Close() error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

// SystemOne evaluates state against a map of named Questions (`POST /v1/systemone`).
// Validates questions client-side before transmitting and automatically retries transient errors
// according to the active RetryPolicy.
func (c *Client) SystemOne(ctx context.Context, req SystemOneRequest, opts ...RequestOption) (*SystemOneResponse, error) {
	if req.State == nil {
		return nil, NewTypeSafeError("`state` is required and cannot be nil.")
	}
	if err := ValidateQuestions(req.Questions); err != nil {
		return nil, err
	}

	reqOpts := resolveRequestOptions(opts...)
	if reqOpts.Model != "" {
		req.Model = reqOpts.Model
	}

	payloadBytes, err := req.MarshalPayload(c.defaultModel, reqOpts.ExtraBody)
	if err != nil {
		return nil, NewTypeSafeError("Failed to marshal SystemOneRequest payload.", err)
	}

	rawBytes, httpResp, err := c.doRequest(ctx, http.MethodPost, "/v1/systemone", payloadBytes, opts...)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s %s/v1/systemone", http.MethodPost, sanitizeEndpointURL(c.baseURL))
	parsedResp, err := parseAndValidateSystemOneResponse(rawBytes, httpResp.StatusCode, httpResp.Header, endpoint, c.logger)
	if err != nil {
		return nil, err
	}
	parsedResp.RawHTTPResponse = httpResp
	return parsedResp, nil
}

// SystemOneWithResponse calls SystemOne and returns a WithResponse[*SystemOneResponse] wrapper,
// mirroring the JS/TS SDK's `client.systemOne(req).withResponse()`.
func (c *Client) SystemOneWithResponse(ctx context.Context, req SystemOneRequest, opts ...RequestOption) (*WithResponse[*SystemOneResponse], error) {
	resp, err := c.SystemOne(ctx, req, opts...)
	if err != nil {
		return nil, err
	}
	return &WithResponse[*SystemOneResponse]{
		Data:      resp,
		Response:  resp.RawHTTPResponse,
		RequestID: resp.RequestID,
	}, nil
}

// Ask is a concise helper method matching the Python SDK's `client.system_one(state, questions, ...)`
// positional call style.
func (c *Client) Ask(ctx context.Context, state Entry, questions Questions, opts ...RequestOption) (*SystemOneResponse, error) {
	return c.SystemOne(ctx, SystemOneRequest{
		State:     state,
		Questions: questions,
	}, opts...)
}

// ---------------------------------------------------------------------------
// Internal Transport, Retries, Timeout, and Logging
// ---------------------------------------------------------------------------

var runtimeHeaderValue = fmt.Sprintf("go/%s (%s; %s)", strings.TrimPrefix(runtime.Version(), "go"), runtime.GOOS, runtime.GOARCH)

func (c *Client) doRequest(
	ctx context.Context,
	method string,
	path string,
	bodyBytes []byte,
	opts ...RequestOption,
) ([]byte, *http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	reqOpts := resolveRequestOptions(opts...)
	timeout := c.timeout
	if reqOpts.timeoutSet {
		if reqOpts.Timeout <= 0 {
			return nil, nil, NewTypeSafeError(fmt.Sprintf("`timeout` must be a positive duration, got %v.", reqOpts.Timeout))
		}
		timeout = reqOpts.Timeout
	}

	retryPolicy := c.retry
	if reqOpts.Retry != nil {
		if err := reqOpts.Retry.Validate(); err != nil {
			return nil, nil, err
		}
		retryPolicy = reqOpts.Retry.Clone()
	}

	reqIDNum := c.requestCount.Add(1)
	tag := fmt.Sprintf("#%d %s %s", reqIDNum, method, path)
	fullURL := c.baseURL + path
	endpoint := fmt.Sprintf("%s %s", method, sanitizeEndpointURL(fullURL))

	// User-supplied headers go first so they cannot clobber protected protocol headers.
	mergedUserHeaders := mergeHeaderMaps(c.defaultHeaders, reqOpts.ExtraHeaders)
	protectedHeaders := map[string]*string{
		"Authorization":          strPtr("Bearer " + c.apiKey),
		"Accept":                 strPtr("application/json"),
		"User-Agent":             strPtr("typesafe-sdk-go/" + Version),
		"X-TypeSafe-SDK":         strPtr("typesafe-sdk-go/" + Version),
		"X-TypeSafe-Runtime":     strPtr(runtimeHeaderValue),
		"X-TypeSafe-Retry-Count": nil, // Explicitly clear any user-supplied retry count
	}
	if bodyBytes != nil {
		protectedHeaders["Content-Type"] = strPtr("application/json")
	} else {
		protectedHeaders["Content-Type"] = nil
	}
	baseHeaders := applyProtectedHeaders(mergedUserHeaders, protectedHeaders)

	callStart := time.Now()

	for attempt := 0; ; attempt++ {
		retriesLeft := retryPolicy.MaxRetries - attempt
		attemptHeaders := cloneStringMap(baseHeaders)
		if attempt > 0 {
			attemptHeaders["X-TypeSafe-Retry-Count"] = strconv.Itoa(attempt)
		}

		c.logger.Debug(fmt.Sprintf("%s -> %s", tag, fullURL), map[string]any{
			"headers": RedactHeaders(attemptHeaders),
			"body":    string(bodyBytes),
		})

		attemptStart := time.Now()
		rawRespBytes, httpResp, err := c.executeSingleAttempt(ctx, tag, method, fullURL, attemptHeaders, bodyBytes, timeout)
		if err != nil {
			var abortErr *APIUserAbortError
			if errors.As(err, &abortErr) || retriesLeft <= 0 {
				return nil, nil, err
			}
			if !isErrorRetryable(err, retryPolicy) {
				return nil, nil, err
			}
			delay := RetryDelay(attempt, nil, retryPolicy, nil)
			if exceedsTotalBudget(callStart, delay, retryPolicy.TotalTimeout) {
				return nil, nil, err
			}
			c.logger.Info(fmt.Sprintf("%s retrying in %dms (retry %d/%d) after %s",
				tag, delay.Milliseconds(), attempt+1, attempt+retriesLeft, err.Error()))
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				c.logger.Info(fmt.Sprintf("%s aborted by caller while waiting to retry", tag))
				return nil, nil, sleepErr
			}
			continue
		}

		reqID := requestIDFromHeader(httpResp.Header)
		reqIDSuffix := ""
		if reqID != "" {
			reqIDSuffix = fmt.Sprintf(" (request %s)", reqID)
		}
		c.logger.Info(fmt.Sprintf("%s <- %d in %dms%s",
			tag, httpResp.StatusCode, time.Since(attemptStart).Milliseconds(), reqIDSuffix))

		if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			c.logger.Debug(fmt.Sprintf("%s <- body", tag), string(rawRespBytes))
			return rawRespBytes, httpResp, nil
		}

		parsedErrBody := parseBodyLenient(rawRespBytes, httpResp.Header.Get("Content-Type"))
		c.logger.Debug(fmt.Sprintf("%s <- error body", tag), parsedErrBody)
		apiErr := APIErrorFromResponse(httpResp.StatusCode, parsedErrBody, rawRespBytes, httpResp.Header, endpoint)

		shouldRetryStatus := retryPolicy.IsRetryableStatus(httpResp.StatusCode) ||
			(retryPolicy.Predicate != nil && retryPolicy.Predicate(apiErr))
		if retriesLeft <= 0 || !shouldRetryStatus {
			return nil, nil, apiErr
		}

		delay := RetryDelay(attempt, httpResp.Header, retryPolicy, nil)
		if exceedsTotalBudget(callStart, delay, retryPolicy.TotalTimeout) {
			return nil, nil, apiErr
		}
		c.logger.Info(fmt.Sprintf("%s retrying in %dms (retry %d/%d) after %d",
			tag, delay.Milliseconds(), attempt+1, attempt+retriesLeft, httpResp.StatusCode))
		if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
			c.logger.Info(fmt.Sprintf("%s aborted by caller while waiting to retry", tag))
			return nil, nil, sleepErr
		}
	}
}

func (c *Client) executeSingleAttempt(
	callerCtx context.Context,
	tag string,
	method string,
	fullURL string,
	headers map[string]string,
	bodyBytes []byte,
	timeout time.Duration,
) ([]byte, *http.Response, error) {
	if err := callerCtx.Err(); err != nil {
		return nil, nil, NewAPIUserAbortError("", err)
	}

	attemptCtx, cancelAttempt := context.WithTimeout(callerCtx, timeout)
	defer cancelAttempt()

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(attemptCtx, method, fullURL, bodyReader)
	if err != nil {
		return nil, nil, NewAPIConnectionError(fmt.Sprintf("Connection error: %v", err), err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	started := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, c.classifyAttemptError(callerCtx, attemptCtx, tag, started, timeout, err)
	}
	defer resp.Body.Close()

	// Buffer the entire response body under the attempt timeout before returning.
	rawBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, nil, c.classifyAttemptError(callerCtx, attemptCtx, tag, started, timeout, readErr)
	}

	// Replace resp.Body with a re-readable reader so callers inspecting RawHTTPResponse can read it again.
	resp.Body = io.NopCloser(bytes.NewReader(rawBytes))
	return rawBytes, resp, nil
}

func (c *Client) classifyAttemptError(
	callerCtx context.Context,
	attemptCtx context.Context,
	tag string,
	started time.Time,
	timeout time.Duration,
	err error,
) error {
	elapsedMs := time.Since(started).Milliseconds()
	// Check whether the caller's own context was cancelled first.
	if callerErr := callerCtx.Err(); callerErr != nil {
		c.logger.Info(fmt.Sprintf("%s aborted by caller after %dms", tag, elapsedMs))
		return NewAPIUserAbortError("", callerErr)
	}
	if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		c.logger.Info(fmt.Sprintf("%s timed out after %dms", tag, elapsedMs))
		return NewAPITimeoutError(timeout, err)
	}
	c.logger.Info(fmt.Sprintf("%s connection error after %dms", tag, elapsedMs), err)
	return NewAPIConnectionError(fmt.Sprintf("Connection error: %v", err), err)
}

func isErrorRetryable(err error, policy RetryPolicy) bool {
	if policy.Predicate != nil && policy.Predicate(err) {
		return true
	}
	var timeoutErr *APITimeoutError
	if errors.As(err, &timeoutErr) {
		return policy.APITimeoutError
	}
	var connErr *APIConnectionError
	if errors.As(err, &connErr) {
		return policy.APIConnectionError
	}
	return false
}

func exceedsTotalBudget(callStart time.Time, nextDelay time.Duration, totalBudget time.Duration) bool {
	if totalBudget <= 0 {
		return false
	}
	return time.Since(callStart)+nextDelay >= totalBudget
}

func resolveRequestOptions(opts ...RequestOption) RequestOptions {
	var o RequestOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	return o
}

func readEnvNonEmpty(name string) string {
	val := strings.TrimSpace(os.Getenv(name))
	return val
}

func fromCodeOrEnv(codeVal, envName string) string {
	if strings.TrimSpace(codeVal) != "" {
		return strings.TrimSpace(codeVal)
	}
	return readEnvNonEmpty(envName)
}

func strPtr(s string) *string { return &s }

func mergeHeaderMaps(sources ...map[string]string) map[string]string {
	type pair struct{ k, v string }
	lowerMap := make(map[string]pair)
	for _, src := range sources {
		for k, v := range src {
			lowerMap[strings.ToLower(k)] = pair{k: k, v: v}
		}
	}
	out := make(map[string]string, len(lowerMap))
	for _, p := range lowerMap {
		out[p.k] = p.v
	}
	return out
}

func applyProtectedHeaders(userHeaders map[string]string, protected map[string]*string) map[string]string {
	type pair struct{ k, v string }
	lowerMap := make(map[string]pair, len(userHeaders)+len(protected))
	for k, v := range userHeaders {
		lowerMap[strings.ToLower(k)] = pair{k: k, v: v}
	}
	for k, vPtr := range protected {
		low := strings.ToLower(k)
		if vPtr == nil {
			delete(lowerMap, low)
		} else {
			lowerMap[low] = pair{k: k, v: *vPtr}
		}
	}
	out := make(map[string]string, len(lowerMap))
	for _, p := range lowerMap {
		out[p.k] = p.v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func requestIDFromHeader(h http.Header) string {
	if h == nil {
		return ""
	}
	return strings.TrimSpace(h.Get("x-typesafe-request-id"))
}

func sanitizeEndpointURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func parseBodyLenient(rawBytes []byte, _ string) any {
	if len(rawBytes) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(rawBytes, &parsed); err == nil {
		return parsed
	}
	return string(rawBytes)
}
