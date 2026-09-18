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
		Questions: typesafe.BindQuestions(billingQ, toneQ, urgencyQ),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Billing probability:", billingQ.MustAnswer(resp).Noul)
	fmt.Println("Customer tone:", toneQ.MustAnswer(resp).Choice) // Typed as Tone!
	fmt.Println("Urgency score:", urgencyQ.MustAnswer(resp).Score)
}
```

---

## Environment Variables & Defaults

| Variable | Configures | Default |
|---|---|---|
| `TYPESAFE_API_KEY` | API key (required if not passed via `WithAPIKey`) | — |
| `TYPESAFE_BASE_URL` | API root URL | `https://api.typesafe.ai` |
| `TYPESAFE_DEFAULT_MODEL` | Default model | `jev-latest` |
| `TYPESAFE_LOG_LEVEL` | SDK logger verbosity (`debug`, `info`, `warn`, `error`, `off`) | `warn` |
