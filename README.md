# TypeSafe AI Go SDK (`typesafe-sdk-go`)

Idiomatic, strongly-typed Go client SDK for the [TypeSafe AI](https://typesafe.ai) System One API (`v0.6.0`), with **zero external dependencies** (pure Go 1.22+ standard library).

## Features

- **100% Protocol & Feature Parity** with `typesafe-sdk` (Python v0.6.0) and `@typesafe-ai/sdk` (TypeScript/JS v0.6.0).
- **Compile-Time Generic Type Safety**: Define `ChoiceQuestion[T]` using custom Go string enums (`type Department string`) and extract typed `ChoiceResponse[T]` via `DefineChoice` / `GetChoice[T]` without unsafe casting.
- **Dual Answer Access**: Access results via pre-partitioned maps (`resp.Nouls`, `resp.Choices`, `resp.Scores` — Python SDK style), polymorphic `resp.Answers` (JS SDK style), or bound generic handles (`deptQ.MustAnswer(resp)`).
- **Automatic Retries with Exponential Backoff & `Retry-After`**: Retries `408`, `429`, `500–599` (including `529 Overloaded`), `APITimeoutError`, and `APIConnectionError` with subtracted jitter (`0.25`) and `retry-after-ms` / `Retry-After` header support.
- **Full Error Hierarchy**: `TypeSafeError`, `APIError` (`BadRequestError` 400, `AuthenticationError` 401, `PermissionDeniedError` 403, `NotFoundError` 404, `UnprocessableEntityError` 422, `RateLimitError` 429, `InternalServerError` 5xx, `APIResponseValidationError`), `APIConnectionError`, `APITimeoutError`, and `APIUserAbortError`.
- **Built-in Architectural Pattern Helpers**: First-class support for **Confidence-Gated Routing** (`RouteChoice`, `RouteScore`, `RouteNoul`), **Composite Scoring** (`ComputeCompositeScore`), and **Speculative Fan-Out / Concurrent Batching** (`BindQuestions`, `BatchSystemOne`).

---

## Installation

```bash
go get github.com/orcawhisperer/typesafe-sdk-go
```

---

## Quickstart

Set `TYPESAFE_API_KEY` in your environment:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/orcawhisperer/typesafe-sdk-go"
)

type Tone string

const (
	ToneCalm       Tone = "calm"
	ToneFrustrated Tone = "frustrated"
	ToneAngry      Tone = "angry"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	billingQ := typesafe.DefineNoul("billing", "Is this ticket about billing?")
	toneQ := typesafe.DefineChoiceOptions("tone", "What is the customer's tone?",
		ToneCalm, ToneFrustrated, ToneAngry,
	)
	urgencyQ := typesafe.DefineScore("urgency", "How urgent is this ticket?",
		"can wait", "this week", "today",
	)

	resp, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State:     map[string]any{"document": "I was charged twice. Please fix this ASAP."},
		Questions: typesafe.MustBindQuestions(billingQ, toneQ, urgencyQ),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Billing probability:", billingQ.MustAnswer(resp).Noul)
	fmt.Println("Customer tone:", toneQ.MustAnswer(resp).Choice) // Typed as Tone!
	fmt.Println("Urgency score:", urgencyQ.MustAnswer(resp).Score)
}
```

See also the runnable examples under [`examples/`](examples/):

| Example | Demonstrates |
|---|---|
| [`examples/quickstart`](examples/quickstart/main.go) | Typed questions, `SystemOne`, bound answer extraction |
| [`examples/fanout_routing`](examples/fanout_routing/main.go) | Speculative fan-out + confidence-gated routing |
| [`examples/composite_score`](examples/composite_score/main.go) | Weighted composite scoring across score/noul answers |

---

## Environment Variables & Defaults

| Variable | Configures | Default |
|---|---|---|
| `TYPESAFE_API_KEY` | API key (required if not passed via `WithAPIKey`) | — |
| `TYPESAFE_BASE_URL` | API root URL | `https://api.typesafe.ai` |
| `TYPESAFE_DEFAULT_MODEL` | Default model | `jev-latest` |
| `TYPESAFE_LOG_LEVEL` | SDK logger verbosity (`debug`, `info`, `warn`, `error`, `off`) | `warn` |

Explicit `ClientOption` values always override environment variables.

---

## Client Configuration

```go
client, err := typesafe.NewClient(
	typesafe.WithAPIKey("ts_..."),
	typesafe.WithBaseURL("https://api.typesafe.ai"),
	typesafe.WithDefaultModel(typesafe.ModelJevLatest),
	typesafe.WithTimeout(30*time.Second),
	typesafe.WithMaxRetries(3),
	typesafe.WithLogLevel(typesafe.LogLevelInfo),
	typesafe.WithDefaultHeaders(map[string]string{"X-App": "my-service"}),
)
```

Per-request overrides:

```go
resp, err := client.SystemOne(ctx, req,
	typesafe.WithRequestModel("jev-1.13.0"),
	typesafe.WithRequestTimeout(60*time.Second),
	typesafe.WithExtraBody(map[string]any{"beam_width": 4}),
)
```

---

## Binding Questions

Use `BindQuestions` to assemble typed question handles into a `Questions` map. It returns an error for nil items or duplicate names:

```go
questions, err := typesafe.BindQuestions(billingQ, toneQ, urgencyQ)
if err != nil {
	log.Fatal(err)
}
```

`MustBindQuestions` panics on binding errors (convenient in examples and init code).

### Answer extraction

- `deptQ.Answer(resp)` returns `(ChoiceResponse[T], bool)` — preferred when a missing answer is unexpected.
- `deptQ.MustAnswer(resp)` returns the zero value when the answer is missing (no panic).

---

## Error Handling

All SDK errors unwrap to `*TypeSafeError`. HTTP failures expose typed status errors:

```go
resp, err := client.SystemOne(ctx, req)
if err != nil {
	var rateLimit *typesafe.RateLimitError
	if errors.As(err, &rateLimit) {
		time.Sleep(rateLimit.RetryAfter)
		// retry...
	}
	var auth *typesafe.AuthenticationError
	if errors.As(err, &auth) {
		log.Fatalf("invalid API key (request %s)", auth.RequestID)
	}
	var valErr *typesafe.APIResponseValidationError
	if errors.As(err, &valErr) {
		log.Fatalf("bad response at %s: %v", valErr.FieldPath, valErr)
	}
	log.Fatal(err)
}
```

Context cancellation returns `*APIUserAbortError`. Per-attempt timeouts return `*APITimeoutError`.

---

## Retry Policy

Default policy: 2 retries, 500ms initial backoff (capped at 5s), 25% subtractive jitter, honors `retry-after-ms` / `Retry-After` up to 60s.

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 5
policy.TotalTimeout = 2 * time.Minute

client, err := typesafe.NewClient(
	typesafe.WithAPIKey("ts_..."),
	typesafe.WithRetryPolicy(policy),
)
```

Disable retries with `typesafe.WithMaxRetries(0)`.

> **Note:** `LogLevelDebug` logs full request bodies including `state` content. Use `warn` or higher in production when state may contain sensitive data.

---

## Models API

```go
models, err := client.Models.List(ctx)
if err != nil {
	log.Fatal(err)
}
for _, m := range models.Models {
	fmt.Println(m.Name, m.Description)
}
```

---

## Architectural Patterns

### Confidence-gated routing

```go
decision := typesafe.RouteChoice(toneQ.MustAnswer(resp), 0.80, 0.50)
switch decision.Action {
case typesafe.GateActionAct:
	// auto-act on decision.Answer
case typesafe.GateActionReview:
	// route to human review
case typesafe.GateActionAbstain:
	// fall back
}
```

Also available: `RouteScore`, `RouteNoul`.

### Composite scoring

```go
composite, err := typesafe.ComputeCompositeScore(resp, []typesafe.ScoreDimension{
	{Name: "severity", Weight: 0.5, MaxScore: 2.0},
	{Name: "is_security", Weight: 0.5},
})
```

### Concurrent batch evaluation

```go
results := client.BatchSystemOne(ctx, []typesafe.SystemOneRequest{req1, req2, req3}, 8)
for _, r := range results {
	if r.Err != nil {
		log.Printf("item %d failed: %v", r.Index, r.Err)
		continue
	}
	// use r.Response
}
```

Results preserve input order. Partial failures do not abort the batch.
