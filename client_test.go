package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orcawhisperer/typesafe-sdk-go"
)

type Department string

const (
	DeptBilling   Department = "billing"
	DeptTechnical Department = "technical"
	DeptSales     Department = "sales"
)

func TestNewClient_ConfigAndEnvPrecedence(t *testing.T) {
	t.Setenv(typesafe.APIKeyEnv, "")
	t.Setenv(typesafe.BaseURLEnv, "")
	t.Setenv(typesafe.DefaultModelEnv, "")
	t.Setenv(typesafe.LogLevelEnv, "")

	// 1. Missing API key fails with *TypeSafeError
	_, err := typesafe.NewClient()
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
	var tsErr *typesafe.TypeSafeError
	if !errors.As(err, &tsErr) {
		t.Fatalf("expected *TypeSafeError, got %T: %v", err, err)
	}

	// 2. Environment variable resolution + trailing slash stripping
	// Note: test dummy token below is solely for unit test verification (not a real secret).
	t.Setenv(typesafe.APIKeyEnv, "  test-env-token-value-1234  ")
	t.Setenv(typesafe.BaseURLEnv, "https://api.typesafe.ai///")
	t.Setenv(typesafe.DefaultModelEnv, "jev-preview")
	t.Setenv(typesafe.LogLevelEnv, "info")

	c, err := typesafe.NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BaseURL() != "https://api.typesafe.ai" {
		t.Errorf("expected trailing slashes stripped, got %q", c.BaseURL())
	}
	if c.DefaultModel() != "jev-preview" {
		t.Errorf("expected default model 'jev-preview', got %q", c.DefaultModel())
	}
	if c.LogLevel() != typesafe.LogLevelInfo {
		t.Errorf("expected log level 'info', got %q", c.LogLevel())
	}

	// 3. Explicit options override environment variables
	c2, err := typesafe.NewClient(
		typesafe.WithAPIKey("explicit-token-9999"),
		typesafe.WithBaseURL("http://127.0.0.1:8080/"),
		typesafe.WithDefaultModel("jev-1.13.0"),
		typesafe.WithLogLevel(typesafe.LogLevelDebug),
		typesafe.WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c2.BaseURL() != "http://127.0.0.1:8080" {
		t.Errorf("got BaseURL %q", c2.BaseURL())
	}
	if c2.DefaultModel() != "jev-1.13.0" {
		t.Errorf("got DefaultModel %q", c2.DefaultModel())
	}
	if c2.Timeout() != 5*time.Second {
		t.Errorf("got Timeout %v", c2.Timeout())
	}

	// 4. Mutually exclusive HTTPClient and Transport
	_, err = typesafe.NewClient(
		typesafe.WithAPIKey("explicit-token-9999"),
		typesafe.WithHTTPClient(&http.Client{}),
		typesafe.WithTransport(http.DefaultTransport),
	)
	if err == nil {
		t.Fatal("expected error when both HTTPClient and Transport are set")
	}
}

func TestSystemOne_PrimitivesGenericsAndHeaders(t *testing.T) {
	// Bind test server to 127.0.0.1 via httptest.NewServer
	var capturedHeaders http.Header
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		capturedHeaders = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-typesafe-request-id", "req_test_abc123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"model": "jev-1.13.0",
			"answers": {
				"is_urgent": {
					"type": "noul",
					"noul": 0.92
				},
				"department": {
					"type": "choice",
					"choice": "technical",
					"probabilities": {
						"billing": 0.08,
						"technical": 0.85,
						"sales": 0.07
					},
					"confidence": 0.82
				},
				"frustration": {
					"type": "score",
					"score": 1.6,
					"legend": {
						"0": "Calm",
						"1": "Frustrated",
						"2": "Very angry"
					},
					"probabilities": {
						"0": 0.05,
						"1": 0.30,
						"2": 0.65
					},
					"confidence": 0.78
				}
			},
			"usage": {
				"input_tokens": 312,
				"output_tokens": 48
			}
		}`))
	}))
	defer srv.Close()

	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("test-key-abcdefgh1234"),
		typesafe.WithBaseURL(srv.URL),
		typesafe.WithDefaultHeaders(map[string]string{
			"X-Custom-Client": "my-app",
			"Authorization":   "Bearer attempt-to-clobber",
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Define strongly-typed bound questions
	urgentQ := typesafe.DefineNoul("is_urgent", "Does this convey urgency?", typesafe.NoulCriteria{
		True:  "Explicitly time-sensitive",
		False: "No urgency expressed",
	})
	deptQ := typesafe.DefineChoice("department", "Which team should handle this?", map[Department]typesafe.Description{
		DeptBilling:   "Payments, invoicing, refunds",
		DeptTechnical: "Bugs, outages, integrations",
		DeptSales:     "Pricing, upgrades, new accounts",
	})
	frustQ := typesafe.DefineScore("frustration", "How frustrated is the customer?", "Calm", "Frustrated", "Very angry")

	questions, err := typesafe.BindQuestions(urgentQ, deptQ, frustQ)
	if err != nil {
		t.Fatalf("BindQuestions failed: %v", err)
	}
	withResp, err := client.SystemOneWithResponse(
		context.Background(),
		typesafe.SystemOneRequest{
			State:     map[string]any{"document": "Help! My payouts have been failing for 3 days."},
			Questions: questions,
		},
		typesafe.WithExtraHeaders(map[string]string{"X-Request-Trace": "trace-42"}),
		typesafe.WithExtraBody(map[string]any{"beam_width": 4}),
	)
	if err != nil {
		t.Fatalf("SystemOneWithResponse failed: %v", err)
	}

	// Verify protected protocol headers could not be clobbered by DefaultHeaders
	if got := capturedHeaders.Get("Authorization"); got != "Bearer test-key-abcdefgh1234" {
		t.Errorf("Authorization header clobbered or wrong: %q", got)
	}
	if got := capturedHeaders.Get("User-Agent"); got != "typesafe-sdk-go/"+typesafe.Version {
		t.Errorf("unexpected User-Agent: %q", got)
	}
	if got := capturedHeaders.Get("X-TypeSafe-SDK"); got != "typesafe-sdk-go/"+typesafe.Version {
		t.Errorf("unexpected X-TypeSafe-SDK: %q", got)
	}
	if !strings.HasPrefix(capturedHeaders.Get("X-TypeSafe-Runtime"), "go/") {
		t.Errorf("unexpected X-TypeSafe-Runtime: %q", capturedHeaders.Get("X-TypeSafe-Runtime"))
	}
	if got := capturedHeaders.Get("X-Custom-Client"); got != "my-app" {
		t.Errorf("expected X-Custom-Client 'my-app', got %q", got)
	}
	if got := capturedHeaders.Get("X-Request-Trace"); got != "trace-42" {
		t.Errorf("expected X-Request-Trace 'trace-42', got %q", got)
	}

	// Verify ExtraBody shallow merge
	if got := capturedBody["beam_width"]; got != float64(4) {
		t.Errorf("expected beam_width=4 in body, got %v", got)
	}
	if got := capturedBody["model"]; got != "jev-latest" {
		t.Errorf("expected default model 'jev-latest', got %v", got)
	}

	resp := withResp.Data
	if withResp.RequestID != "req_test_abc123" || resp.RequestID != "req_test_abc123" {
		t.Errorf("expected RequestID 'req_test_abc123', got %q", resp.RequestID)
	}
	if resp.Model != "jev-1.13.0" {
		t.Errorf("expected Model 'jev-1.13.0', got %q", resp.Model)
	}
	if resp.Usage.InputTokensValue() != 312 || resp.Usage.OutputTokensValue() != 48 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}

	// Check Python-style pre-partitioned maps AND bound generic extractors
	if resp.Nouls["is_urgent"].Noul != 0.92 || urgentQ.MustAnswer(resp).Noul != 0.92 {
		t.Errorf("unexpected noul answer: %+v", resp.Nouls["is_urgent"])
	}
	deptAns := deptQ.MustAnswer(resp)
	if deptAns.Choice != DeptTechnical || deptAns.Confidence != 0.82 || deptAns.Probabilities[DeptTechnical] != 0.85 {
		t.Errorf("unexpected generic choice answer: %+v", deptAns)
	}
	frustAns := frustQ.MustAnswer(resp)
	if frustAns.Score != 1.6 || frustAns.Confidence != 0.78 || frustAns.Legend["2"] != "Very angry" {
		t.Errorf("unexpected score answer: %+v", frustAns)
	}

	// Verify RawHTTPResponse.Body is still re-readable after SDK parsing
	reReadBytes, err := io.ReadAll(resp.RawHTTPResponse.Body)
	if err != nil || len(reReadBytes) == 0 {
		t.Errorf("expected RawHTTPResponse.Body to be re-readable, got len=%d err=%v", len(reReadBytes), err)
	}
}

func TestBindQuestions_NilAndDuplicateNames(t *testing.T) {
	q1 := typesafe.DefineNoul("billing", "Is this about billing?")
	q2 := typesafe.DefineScore("urgency", "How urgent?", "low", "high")

	_, err := typesafe.BindQuestions(q1, nil)
	if err == nil || !strings.Contains(err.Error(), "nil BoundQuestion") {
		t.Fatalf("expected nil BoundQuestion error, got %v", err)
	}

	dup := typesafe.DefineNoul("billing", "Duplicate name")
	_, err = typesafe.BindQuestions(q1, dup)
	if err == nil || !strings.Contains(err.Error(), "duplicate question name") {
		t.Fatalf("expected duplicate name error, got %v", err)
	}

	bound, err := typesafe.BindQuestions(q1, q2)
	if err != nil {
		t.Fatalf("unexpected BindQuestions error: %v", err)
	}
	if len(bound) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(bound))
	}
}

func TestValidateQuestions_ClientSideRejection(t *testing.T) {
	client := typesafe.MustNewClient(typesafe.WithAPIKey("test-key-123456789"))

	// 1. Empty questions map
	_, err := client.Ask(context.Background(), "hello", typesafe.Questions{})
	if err == nil || !strings.Contains(err.Error(), "At least one question is required") {
		t.Fatalf("expected empty questions error, got %v", err)
	}

	// 2. Score question with < 2 criteria
	_, err = client.Ask(context.Background(), "hello", typesafe.Questions{
		"bad_score": typesafe.ScoreSlice("Rate this", []typesafe.Entry{"only_one"}),
	})
	if err == nil || !strings.Contains(err.Error(), "at least two scores are required") {
		t.Fatalf("expected <2 score criteria error, got %v", err)
	}

	// 3. RawQuestion score with non-list criteria
	_, err = client.Ask(context.Background(), "hello", typesafe.Questions{
		"raw_score": typesafe.RawQuestion{
			"type":         "score",
			"instructions": "Rate this",
			"criteria":     map[string]any{"0": "low", "1": "high"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not a list") {
		t.Fatalf("expected non-list score criteria error, got %v", err)
	}

	// 4. Choice question with empty criteria map
	_, err = client.Ask(context.Background(), "hello", typesafe.Questions{
		"bad_choice": typesafe.Choice("Pick one", map[Department]typesafe.Description{}),
	})
	if err == nil || !strings.Contains(err.Error(), "at least one option") {
		t.Fatalf("expected empty choice criteria error, got %v", err)
	}
}

func TestRetries_BackoffRetryAfterAndErrors(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			if got := r.Header.Get("X-TypeSafe-Retry-Count"); got != "" {
				t.Errorf("attempt 1 should not have X-TypeSafe-Retry-Count, got %q", got)
			}
			w.Header().Set("retry-after-ms", "15")
			w.Header().Set("x-typesafe-request-id", "req_429_1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "Rate limit exceeded"}`))
			return
		}
		if n == 2 {
			if got := r.Header.Get("X-TypeSafe-Retry-Count"); got != "1" {
				t.Errorf("attempt 2 expected X-TypeSafe-Retry-Count='1', got %q", got)
			}
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(529) // 529 Overloaded
			_, _ = w.Write([]byte(`{"message": "TypeSafe is overloaded"}`))
			return
		}
		if got := r.Header.Get("X-TypeSafe-Retry-Count"); got != "2" {
			t.Errorf("attempt 3 expected X-TypeSafe-Retry-Count='2', got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"model": "jev-1.13.0",
			"answers": {"ok": {"type": "noul", "noul": 0.99}},
			"usage": {"input_tokens": 10, "output_tokens": 2}
		}`))
	}))
	defer srv.Close()

	client := typesafe.MustNewClient(
		typesafe.WithAPIKey("test-key-123456789"),
		typesafe.WithBaseURL(srv.URL),
	)

	resp, err := client.Ask(context.Background(), "state", typesafe.Questions{
		"ok": typesafe.Noul("Is it ok?"),
	})
	if err != nil {
		t.Fatalf("expected retry to succeed on 3rd attempt, got err: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
	if resp.Nouls["ok"].Noul != 0.99 {
		t.Errorf("unexpected noul: %v", resp.Nouls["ok"].Noul)
	}
}

func TestErrorHierarchyAndStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		check  func(error) bool
	}{
		{400, `{"error":"bad input"}`, func(err error) bool { var e *typesafe.BadRequestError; return errors.As(err, &e) }},
		{401, `{"error":{"message":"invalid api key"}}`, func(err error) bool { var e *typesafe.AuthenticationError; return errors.As(err, &e) }},
		{403, `{"detail":"forbidden"}`, func(err error) bool { var e *typesafe.PermissionDeniedError; return errors.As(err, &e) }},
		{404, `{"message":"not found"}`, func(err error) bool { var e *typesafe.NotFoundError; return errors.As(err, &e) }},
		{422, `{"detail":[{"loc":["body","questions","q1"],"msg":"field required"}]}`, func(err error) bool {
			var e *typesafe.UnprocessableEntityError
			return errors.As(err, &e) && strings.Contains(e.Error(), "questions.q1: field required")
		}},
		{429, `{"error":"rate limited"}`, func(err error) bool {
			var e *typesafe.RateLimitError
			return errors.As(err, &e) && e.RetryAfterMs != nil && *e.RetryAfterMs == 2000
		}},
		{529, `{"error":"overloaded"}`, func(err error) bool { var e *typesafe.InternalServerError; return errors.As(err, &e) }},
	}

	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.status == 429 {
				w.Header().Set("Retry-After", "2")
			}
			w.Header().Set("x-typesafe-request-id", "req_err_1")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		client := typesafe.MustNewClient(
			typesafe.WithAPIKey("test-key-123456789"),
			typesafe.WithBaseURL(srv.URL),
			typesafe.WithMaxRetries(0),
		)
		_, err := client.Ask(context.Background(), "state", typesafe.Questions{"q": typesafe.Noul("?")})
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: expected error, got nil", tc.status)
		}
		if !tc.check(err) {
			t.Errorf("status %d: error check failed for %T (%v)", tc.status, err, err)
		}
		var apiErr *typesafe.APIError
		if !errors.As(err, &apiErr) || apiErr.RequestID != "req_err_1" {
			t.Errorf("status %d: expected errors.As(*APIError) with RequestID 'req_err_1', got %+v", tc.status, apiErr)
		}
	}
}

func TestTimeoutAndCallerAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 1. Per-attempt timeout -> *APITimeoutError (which also unwraps to *APIConnectionError)
	client := typesafe.MustNewClient(
		typesafe.WithAPIKey("test-key-123456789"),
		typesafe.WithBaseURL(srv.URL),
		typesafe.WithTimeout(30*time.Millisecond),
		typesafe.WithMaxRetries(0),
	)
	_, err := client.Ask(context.Background(), "state", typesafe.Questions{"q": typesafe.Noul("?")})
	var timeoutErr *typesafe.APITimeoutError
	var connErr *typesafe.APIConnectionError
	if !errors.As(err, &timeoutErr) || !errors.As(err, &connErr) {
		t.Fatalf("expected *APITimeoutError and *APIConnectionError, got %T: %v", err, err)
	}

	// 2. Caller context cancellation -> *APIUserAbortError (no retries!)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	clientWithRetries := typesafe.MustNewClient(
		typesafe.WithAPIKey("test-key-123456789"),
		typesafe.WithBaseURL(srv.URL),
		typesafe.WithTimeout(2*time.Second),
		typesafe.WithMaxRetries(3),
	)
	_, err = clientWithRetries.Ask(ctx, "state", typesafe.Questions{"q": typesafe.Noul("?")})
	var abortErr *typesafe.APIUserAbortError
	if !errors.As(err, &abortErr) {
		t.Fatalf("expected *APIUserAbortError on context cancel, got %T: %v", err, err)
	}
}

func TestModelsList_AndValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("x-typesafe-request-id", "req_models_1")
			_, _ = w.Write([]byte(`{
				"models": [
					{"name": "jev-latest", "description": "Latest stable Jev", "release_date": "2026-09-14"}
				]
			}`))
			return
		}
		// Return a 200 OK with invalid confidence field to trigger APIResponseValidationError
		_, _ = w.Write([]byte(`{
			"model": "jev-1.13.0",
			"answers": {
				"tone": {
					"type": "choice",
					"choice": "calm",
					"confidence": "not-a-number",
					"probabilities": {"calm": 1.0}
				}
			},
			"usage": {"input_tokens": 5, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	client := typesafe.MustNewClient(
		typesafe.WithAPIKey("test-key-123456789"),
		typesafe.WithBaseURL(srv.URL),
	)

	modelsResp, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatalf("Models.List failed: %v", err)
	}
	if len(modelsResp.Models) != 1 || modelsResp.Models[0].Name != "jev-latest" {
		t.Errorf("unexpected models: %+v", modelsResp.Models)
	}

	_, err = client.Ask(context.Background(), "state", typesafe.Questions{
		"tone": typesafe.ChoiceOptions("Tone?", "calm", "angry"),
	})
	var valErr *typesafe.APIResponseValidationError
	if !errors.As(err, &valErr) || valErr.FieldPath != "answers.tone.confidence" {
		t.Fatalf("expected *APIResponseValidationError with FieldPath 'answers.tone.confidence', got %T (%+v)", err, valErr)
	}
}

func TestRedactHeaders_AndArchitecturalPatterns(t *testing.T) {
	redacted := typesafe.RedactHeaders(map[string]string{
		"Authorization": "Bearer ts_live_1234567890abcd",
		"X-Api-Key":     "short",
		"Cookie":        "session=xyz",
		"X-Auth-Token":  "my-token",
		"Accept":        "application/json",
	})
	if redacted["Authorization"] != "Bearer ***abcd" {
		t.Errorf("expected 'Bearer ***abcd', got %q", redacted["Authorization"])
	}
	if redacted["X-Api-Key"] != "***" {
		t.Errorf("expected '***' for short key, got %q", redacted["X-Api-Key"])
	}
	if redacted["Cookie"] != "***" || redacted["X-Auth-Token"] != "***" {
		t.Errorf("expected '***' for Cookie and X-Auth-Token, got %+v", redacted)
	}
	if redacted["Accept"] != "application/json" {
		t.Errorf("expected Accept untouched, got %q", redacted["Accept"])
	}

	// Test Confidence-Gated Routing & Composite Scoring
	resp := &typesafe.SystemOneResponse{
		Choices: map[string]typesafe.ChoiceResponse[string]{
			"dept": {Type: "choice", Choice: "billing", Confidence: 0.88},
		},
		Scores: map[string]typesafe.ScoreResponse{
			"severity": {Type: "score", Score: 1.5, Confidence: 0.90},
			"impact":   {Type: "score", Score: 2.0, Confidence: 0.80},
		},
		Nouls: map[string]typesafe.NoulResponse{
			"vip": {Type: "noul", Noul: 0.95},
		},
	}
	route := typesafe.RouteChoice(resp.Choices["dept"], 0.80, 0.50)
	if route.Action != typesafe.GateActionAct || route.Answer != "billing" {
		t.Errorf("unexpected route decision: %+v", route)
	}

	comp, err := typesafe.ComputeCompositeScore(resp, []typesafe.ScoreDimension{
		{Name: "severity", Weight: 0.5, MaxScore: 2.0}, // 1.5 / 2.0 = 0.75 * 0.5 = 0.375
		{Name: "impact", Weight: 0.3, MaxScore: 2.0},   // 2.0 / 2.0 = 1.00 * 0.3 = 0.300
		{Name: "vip", Weight: 0.2},                     // 0.95 * 0.2 = 0.190
	})
	if err != nil {
		t.Fatalf("ComputeCompositeScore failed: %v", err)
	}
	// Total = 0.375 + 0.300 + 0.190 = 0.865
	if comp.WeightedScore < 0.864 || comp.WeightedScore > 0.866 || comp.MinConfidence != 0.80 {
		t.Errorf("unexpected composite score: %+v", comp)
	}
}

func TestBatchSystemOne_OrderConcurrencyAndPartialFailure(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			State map[string]any `json:"state"`
		}
		_ = json.Unmarshal(raw, &payload)
		idx, _ := payload.State["idx"].(float64)

		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		time.Sleep(30 * time.Millisecond)

		mu.Lock()
		active--
		mu.Unlock()

		if int(idx) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"model": "jev-1.13.0",
			"answers": {"q": {"type": "noul", "noul": %.2f}},
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`, idx*0.1)))
	}))
	defer srv.Close()

	client := typesafe.MustNewClient(
		typesafe.WithAPIKey("test-key-123456789"),
		typesafe.WithBaseURL(srv.URL),
		typesafe.WithMaxRetries(0),
	)

	makeReq := func(idx int) typesafe.SystemOneRequest {
		return typesafe.SystemOneRequest{
			State:     map[string]any{"idx": idx},
			Questions: typesafe.Questions{"q": typesafe.Noul("?")},
		}
	}

	requests := []typesafe.SystemOneRequest{
		makeReq(0),
		makeReq(1),
		makeReq(2),
		makeReq(3),
	}

	results := client.BatchSystemOne(context.Background(), requests, 2)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	if maxActive > 2 {
		t.Errorf("expected concurrency capped at 2, saw maxActive=%d", maxActive)
	}

	if results[0].Err != nil || results[0].Response.Nouls["q"].Noul != 0.0 {
		t.Errorf("index 0: expected success with noul=0.0, got err=%v resp=%+v", results[0].Err, results[0].Response)
	}
	var badReq *typesafe.BadRequestError
	if !errors.As(results[1].Err, &badReq) {
		t.Errorf("index 1: expected *BadRequestError, got %v", results[1].Err)
	}
	if results[2].Err != nil || results[2].Response.Nouls["q"].Noul != 0.2 {
		t.Errorf("index 2: expected success with noul=0.2, got err=%v", results[2].Err)
	}
	if results[3].Err != nil || results[3].Response.Nouls["q"].Noul != 0.3 {
		t.Errorf("index 3: expected success with noul=0.3, got err=%v", results[3].Err)
	}
	for i, r := range results {
		if r.Index != i {
			t.Errorf("result[%d].Index = %d, want %d", i, r.Index, i)
		}
	}
}

func TestBatchSystemOne_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":1}},"usage":{}}`))
	}))
	defer srv.Close()

	client := typesafe.MustNewClient(
		typesafe.WithAPIKey("test-key-123456789"),
		typesafe.WithBaseURL(srv.URL),
		typesafe.WithTimeout(5*time.Second),
		typesafe.WithMaxRetries(0),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	requests := []typesafe.SystemOneRequest{
		{State: "a", Questions: typesafe.Questions{"q": typesafe.Noul("?")}},
		{State: "b", Questions: typesafe.Questions{"q": typesafe.Noul("?")}},
	}

	results := client.BatchSystemOne(ctx, requests, 2)
	for i, r := range results {
		var abortErr *typesafe.APIUserAbortError
		if !errors.As(r.Err, &abortErr) {
			t.Errorf("result[%d]: expected *APIUserAbortError, got %v", i, r.Err)
		}
	}
}
