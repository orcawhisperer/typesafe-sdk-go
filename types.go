package typesafe

import (
	"encoding/json"
	"net/http"
)

// JSONValue represents any JSON-compatible value (string, number, bool, map, slice, struct, or nil).
type JSONValue = any

// Entry represents text (string), a structured JSON object (map or struct), a JSON array (slice), or nil.
// Used for state, question instructions, choice option descriptions, score rubric entries, and noul criteria.
type Entry = any

// Description is an alias for Entry used in criterion descriptions; nil leaves a label undescribed.
type Description = Entry

// Usage reports token consumption for a System One evaluation request.
type Usage struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
}

// InputTokensValue returns the input token count, or 0 if nil.
func (u Usage) InputTokensValue() int {
	if u.InputTokens == nil {
		return 0
	}
	return *u.InputTokens
}

// OutputTokensValue returns the output token count, or 0 if nil.
func (u Usage) OutputTokensValue() int {
	if u.OutputTokens == nil {
		return 0
	}
	return *u.OutputTokens
}

// ModelCard describes an available model or model alias returned by GET /v1/models.
type ModelCard struct {
	// Name is the model ID or alias, as accepted by the request's Model field (e.g., "jev-latest").
	Name string `json:"name"`

	// Description explains what the model is for.
	Description string `json:"description"`

	// ReleaseDate indicates when the model or alias was released.
	ReleaseDate string `json:"release_date"`
}

// ModelMetadata is an alias for ModelCard, matching the Python SDK's naming convention.
type ModelMetadata = ModelCard

// WithResponse wraps a parsed response value alongside its raw HTTP response and TypeSafe request ID,
// mirroring the JS/TS SDK's WithResponse<T> interface.
type WithResponse[T any] struct {
	// Data is the parsed response payload.
	Data T

	// Response is the underlying *http.Response with a re-readable Body buffer.
	Response *http.Response

	// RequestID is the value of the "x-typesafe-request-id" response header, or "" if absent.
	RequestID string
}

// SystemOneRequest represents the parameters for evaluating state against named questions via POST /v1/systemone.
type SystemOneRequest struct {
	// State is the text, JSON object (map/struct), or array (slice) to evaluate. Required (cannot be nil).
	State Entry

	// Questions is a non-empty map of question identifiers to Question definitions (Noul, Choice, Score, or RawQuestion).
	Questions Questions

	// Model is an optional model override (e.g., "jev-latest" or "jev-1.13.0").
	// When empty, the client's DefaultModel is used.
	Model string

	// ExtraBody holds optional top-level JSON fields shallow-merged over the request payload
	// after state, model, and questions are set (last-write-wins), providing forward compatibility.
	ExtraBody map[string]any
}

// MarshalPayload serializes the SystemOneRequest into a JSON byte slice using the resolved defaultModel
// and merging any request-level and call-level ExtraBody maps.
func (r SystemOneRequest) MarshalPayload(defaultModel string, callExtraBody map[string]any) ([]byte, error) {
	model := r.Model
	if model == "" {
		model = defaultModel
	}
	qMap := make(map[string]any, len(r.Questions))
	for name, q := range r.Questions {
		if q == nil {
			continue
		}
		qMap[name] = q.toWire()
	}

	payload := map[string]any{
		"state":     r.State,
		"model":     model,
		"questions": qMap,
	}
	for k, v := range r.ExtraBody {
		payload[k] = v
	}
	for k, v := range callExtraBody {
		payload[k] = v
	}
	return json.Marshal(payload)
}
