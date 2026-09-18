package typesafe

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Answer is implemented by all parsed TypeSafe answer types (NoulResponse, ChoiceResponse[string], ScoreResponse, UnknownAnswer).
type Answer interface {
	// AnswerType returns the answer discriminator ("noul", "choice", "score", or an unrecognized type string).
	AnswerType() string
}

// NoulResponse represents the model's answer to a Noul (yes/no) question.
type NoulResponse struct {
	// Type is always "noul".
	Type string `json:"type"`

	// Noul is the probability of a "yes" answer on a scale from 0.0 (no) to 1.0 (yes).
	Noul float64 `json:"noul"`
}

// AnswerType returns "noul".
func (NoulResponse) AnswerType() string { return "noul" }

// NoulAnswer is an alias for NoulResponse matching the Python SDK naming.
type NoulAnswer = NoulResponse

// ChoiceResponse represents the model's answer to a Choice question, parameterized by the label type T (~string).
type ChoiceResponse[T ~string] struct {
	// Type is always "choice".
	Type string `json:"type"`

	// Choice is the highest-probability option label.
	Choice T `json:"choice"`

	// Confidence reports how certain the model is in the selected label, in [0.0, 1.0].
	Confidence float64 `json:"confidence"`

	// Probabilities maps every option label defined in criteria to its probability (summing to 1.0).
	Probabilities map[T]float64 `json:"probabilities"`
}

// AnswerType returns "choice".
func (ChoiceResponse[T]) AnswerType() string { return "choice" }

// ChoiceAnswer is an alias for ChoiceResponse[string] matching the Python SDK naming.
type ChoiceAnswer = ChoiceResponse[string]

// ScoreResponse represents the model's answer to a Score question.
type ScoreResponse struct {
	// Type is always "score".
	Type string `json:"type"`

	// Score is the probability-weighted expected score across the rubric levels (may land between integer levels).
	Score float64 `json:"score"`

	// Confidence reports how certain the model is in the score, in [0.0, 1.0].
	Confidence float64 `json:"confidence"`

	// Legend maps each stringified level index ("0", "1", ...) back to its description from criteria.
	Legend map[string]Entry `json:"legend"`

	// Probabilities maps each stringified level index ("0", "1", ...) to its probability (summing to 1.0).
	Probabilities map[string]float64 `json:"probabilities"`
}

// AnswerType returns "score".
func (ScoreResponse) AnswerType() string { return "score" }

// ScoreAnswer is an alias for ScoreResponse matching the Python SDK naming.
type ScoreAnswer = ScoreResponse

// UnknownAnswer preserves unrecognized future answer kinds when the API introduces a new primitive type.
type UnknownAnswer struct {
	Type string         `json:"type"`
	Raw  map[string]any `json:"raw"`
}

// AnswerType returns the unrecognized type string.
func (u UnknownAnswer) AnswerType() string { return u.Type }

// SystemOneResponse holds the evaluation results from POST /v1/systemone, providing both
// the polymorphic Answers map (JS style) and pre-partitioned Nouls, Choices, and Scores maps (Python style).
type SystemOneResponse struct {
	// Model is the versioned model ID that performed the evaluation (e.g., "jev-1.13.0").
	Model string `json:"model"`

	// Usage contains token consumption statistics for the request.
	Usage Usage `json:"usage"`

	// Answers contains all parsed Answer objects keyed by question name.
	Answers map[string]Answer `json:"answers"`

	// Nouls contains all "noul" answers keyed by question name.
	Nouls map[string]NoulResponse `json:"-"`

	// Choices contains all "choice" answers keyed by question name.
	Choices map[string]ChoiceResponse[string] `json:"-"`

	// Scores contains all "score" answers keyed by question name.
	Scores map[string]ScoreResponse `json:"-"`

	// RawAnswers retains the raw JSON representation of each answer for forward compatibility.
	RawAnswers map[string]json.RawMessage `json:"-"`

	// RequestID is the value of the "x-typesafe-request-id" response header, or "" if absent.
	RequestID string `json:"-"`

	// RawHTTPResponse is the underlying *http.Response with a re-readable Body buffer.
	RawHTTPResponse *http.Response `json:"-"`
}

// SystemOneResult is an alias for SystemOneResponse matching the JS SDK naming.
type SystemOneResult = SystemOneResponse

// GetChoice retrieves a strongly-typed ChoiceResponse[T] from resp for the given question name,
// converting the string label and probability keys to the caller's custom ~string enum type T.
func GetChoice[T ~string](resp *SystemOneResponse, name string) (ChoiceResponse[T], bool) {
	var zero ChoiceResponse[T]
	if resp == nil {
		return zero, false
	}
	raw, ok := resp.Choices[name]
	if !ok {
		return zero, false
	}
	probs := make(map[T]float64, len(raw.Probabilities))
	for k, v := range raw.Probabilities {
		probs[T(k)] = v
	}
	return ChoiceResponse[T]{
		Type:          raw.Type,
		Choice:        T(raw.Choice),
		Confidence:    raw.Confidence,
		Probabilities: probs,
	}, true
}

// GetScore retrieves a ScoreResponse from resp for the given question name.
func GetScore(resp *SystemOneResponse, name string) (ScoreResponse, bool) {
	if resp == nil {
		return ScoreResponse{}, false
	}
	s, ok := resp.Scores[name]
	return s, ok
}

// GetNoul retrieves a NoulResponse from resp for the given question name.
func GetNoul(resp *SystemOneResponse, name string) (NoulResponse, bool) {
	if resp == nil {
		return NoulResponse{}, false
	}
	n, ok := resp.Nouls[name]
	return n, ok
}

// ListModelsResponse holds the response from GET /v1/models.
type ListModelsResponse struct {
	// Models is the slice of available models and aliases.
	Models []ModelCard `json:"models"`

	// RequestID is the value of the "x-typesafe-request-id" response header, or "" if absent.
	RequestID string `json:"-"`

	// RawHTTPResponse is the underlying *http.Response with a re-readable Body buffer.
	RawHTTPResponse *http.Response `json:"-"`
}

// parseAndValidateSystemOneResponse decodes the raw JSON response from POST /v1/systemone,
// validates all required fields (returning *APIResponseValidationError with dotted FieldPath on violation),
// and populates Answers, Nouls, Choices, Scores, and RawAnswers.
// Unknown answer kinds log a warning on logger and are preserved in Answers[name] as UnknownAnswer and RawAnswers[name].
func parseAndValidateSystemOneResponse(
	rawBytes []byte,
	status int,
	headers http.Header,
	endpoint string,
	logger Logger,
) (*SystemOneResponse, error) {
	var parsedAny any
	if err := json.Unmarshal(rawBytes, &parsedAny); err != nil {
		return nil, NewAPIResponseValidationError(status, string(rawBytes), rawBytes, headers, endpoint, "", "invalid JSON response body")
	}

	rootMap, ok := parsedAny.(map[string]any)
	if !ok {
		return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, "", "expected JSON object at root")
	}

	modelVal, hasModel := rootMap["model"]
	if !hasModel || modelVal == nil {
		return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, "model", "missing required field")
	}
	modelStr, ok := modelVal.(string)
	if !ok {
		return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, "model", "expected string")
	}

	answersVal, hasAnswers := rootMap["answers"]
	if !hasAnswers || answersVal == nil {
		return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, "answers", "missing required field")
	}
	answersMap, ok := answersVal.(map[string]any)
	if !ok {
		return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, "answers", "expected object mapping question names to answers")
	}

	var wire struct {
		Model   string                     `json:"model"`
		Usage   Usage                      `json:"usage"`
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if err := json.Unmarshal(rawBytes, &wire); err != nil {
		return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, "", err.Error())
	}

	resp := &SystemOneResponse{
		Model:      modelStr,
		Usage:      wire.Usage,
		Answers:    make(map[string]Answer, len(wire.Answers)),
		Nouls:      make(map[string]NoulResponse),
		Choices:    make(map[string]ChoiceResponse[string]),
		Scores:     make(map[string]ScoreResponse),
		RawAnswers: wire.Answers,
		RequestID:  requestIDFromHeader(headers),
	}

	for qName, rawAns := range wire.Answers {
		ansObj, ok := answersMap[qName].(map[string]any)
		if !ok {
			return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s", qName), "expected answer object")
		}
		typeVal, hasType := ansObj["type"]
		if !hasType || typeVal == nil {
			return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.type", qName), "missing required field")
		}
		typeStr, ok := typeVal.(string)
		if !ok {
			return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.type", qName), "expected string")
		}

		switch typeStr {
		case "noul":
			if _, ok := ansObj["noul"].(float64); !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.noul", qName), "expected number")
			}
			var nAns NoulResponse
			if err := json.Unmarshal(rawAns, &nAns); err != nil {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s", qName), err.Error())
			}
			resp.Answers[qName] = nAns
			resp.Nouls[qName] = nAns

		case "choice":
			if _, ok := ansObj["choice"].(string); !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.choice", qName), "expected string")
			}
			if _, ok := ansObj["confidence"].(float64); !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.confidence", qName), "expected number")
			}
			probsObj, ok := ansObj["probabilities"].(map[string]any)
			if !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.probabilities", qName), "expected map of option probabilities")
			}
			for optKey, pVal := range probsObj {
				if _, ok := pVal.(float64); !ok {
					return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.probabilities.%s", qName, optKey), "expected number")
				}
			}
			var cAns ChoiceResponse[string]
			if err := json.Unmarshal(rawAns, &cAns); err != nil {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s", qName), err.Error())
			}
			resp.Answers[qName] = cAns
			resp.Choices[qName] = cAns

		case "score":
			if _, ok := ansObj["score"].(float64); !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.score", qName), "expected number")
			}
			if _, ok := ansObj["confidence"].(float64); !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.confidence", qName), "expected number")
			}
			if _, ok := ansObj["legend"].(map[string]any); !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.legend", qName), "expected object")
			}
			probsObj, ok := ansObj["probabilities"].(map[string]any)
			if !ok {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.probabilities", qName), "expected map of level probabilities")
			}
			for lvlKey, pVal := range probsObj {
				if _, ok := pVal.(float64); !ok {
					return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s.probabilities.%s", qName, lvlKey), "expected number")
				}
			}
			var sAns ScoreResponse
			if err := json.Unmarshal(rawAns, &sAns); err != nil {
				return nil, NewAPIResponseValidationError(status, parsedAny, rawBytes, headers, endpoint, fmt.Sprintf("answers.%s", qName), err.Error())
			}
			resp.Answers[qName] = sAns
			resp.Scores[qName] = sAns

		default:
			if logger != nil {
				logger.Warn(fmt.Sprintf("Unrecognized answer kind %q for question %q; skipping typed extraction (inspect RawAnswers or RawHTTPResponse).", typeStr, qName))
			}
			resp.Answers[qName] = UnknownAnswer{
				Type: typeStr,
				Raw:  ansObj,
			}
		}
	}

	return resp, nil
}
