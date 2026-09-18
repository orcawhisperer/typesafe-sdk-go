package typesafe

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Question is implemented by all TypeSafe question types (NoulQuestion, ChoiceQuestion[T], ScoreQuestion, RawQuestion).
type Question interface {
	// QuestionType returns the question primitive identifier ("noul", "choice", or "score").
	QuestionType() string
	toWire() any
	validate(name string) error
}

// Questions maps user-chosen question names to Question definitions.
// Answers are returned under the exact same names in SystemOneResponse.
type Questions map[string]Question

// NoulCriteria holds optional descriptions of the yes ("true") and no ("false") outcomes for a NoulQuestion.
type NoulCriteria struct {
	// True describes what a yes (probability near 1) means; nil leaves it undescribed.
	True Entry `json:"true,omitempty"`

	// False describes what a no (probability near 0) means; nil leaves it undescribed.
	False Entry `json:"false,omitempty"`

	// IncludeNulls forces explicit `"true": null` or `"false": null` in the JSON payload when set.
	IncludeNulls bool `json:"-"`
}

func (c NoulCriteria) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 2)
	if c.True != nil || c.IncludeNulls {
		m["true"] = c.True
	}
	if c.False != nil || c.IncludeNulls {
		m["false"] = c.False
	}
	return json.Marshal(m)
}

// NoulQuestion represents a yes/no question that returns the probability (0 to 1) that the answer is yes.
type NoulQuestion struct {
	// Instructions is the yes/no question to evaluate (string, JSON object, array, or nil).
	Instructions Entry

	// Criteria provides optional descriptions of the true and false outcomes.
	Criteria *NoulCriteria

	// ExtraFields allows arbitrary extra fields on the question object for forward compatibility.
	ExtraFields map[string]any
}

// QuestionType returns "noul".
func (NoulQuestion) QuestionType() string { return "noul" }

func (q NoulQuestion) toWire() any {
	m := map[string]any{
		"type":         "noul",
		"instructions": q.Instructions,
	}
	if q.Criteria != nil {
		m["criteria"] = q.Criteria
	}
	for k, v := range q.ExtraFields {
		m[k] = v
	}
	return m
}

func (q NoulQuestion) validate(_ string) error {
	return nil
}

func (q NoulQuestion) MarshalJSON() ([]byte, error) {
	return json.Marshal(q.toWire())
}

// Noul constructs a NoulQuestion with optional NoulCriteria.
// Mirrors both Python's Noul(instructions=..., criteria=...) and JS's noul(instructions, criteria).
func Noul(instructions Entry, criteria ...NoulCriteria) NoulQuestion {
	var crit *NoulCriteria
	if len(criteria) > 0 {
		c := criteria[0]
		crit = &c
	}
	return NoulQuestion{
		Instructions: instructions,
		Criteria:     crit,
	}
}

// ChoiceCriteria maps string-like option labels to descriptions (or nil when an option needs no extra detail).
type ChoiceCriteria[T ~string] map[T]Description

// ChoiceQuestion represents a question that selects one option from a defined set of named alternatives.
// Parameterized by T ~string so callers can use either plain strings or strongly-typed Go string enums.
type ChoiceQuestion[T ~string] struct {
	// Instructions describes what the model should decide (string, JSON object, array, or nil).
	Instructions Entry

	// Criteria maps each option label to its rubric description (or nil for undescribed labels).
	Criteria map[T]Description

	// ExtraFields allows arbitrary extra fields on the question object for forward compatibility.
	ExtraFields map[string]any
}

// QuestionType returns "choice".
func (ChoiceQuestion[T]) QuestionType() string { return "choice" }

func (q ChoiceQuestion[T]) toWire() any {
	crit := make(map[string]any, len(q.Criteria))
	for k, v := range q.Criteria {
		crit[string(k)] = v
	}
	m := map[string]any{
		"type":         "choice",
		"instructions": q.Instructions,
		"criteria":     crit,
	}
	for k, v := range q.ExtraFields {
		m[k] = v
	}
	return m
}

func (q ChoiceQuestion[T]) validate(name string) error {
	if q.Criteria == nil {
		return NewTypeSafeError(fmt.Sprintf("Choice question %q must have a non-nil criteria map.", name))
	}
	return nil
}

func (q ChoiceQuestion[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(q.toWire())
}

// Choice constructs a ChoiceQuestion[T] from instructions and a map of option labels to descriptions.
// Type parameter T (~string) is inferred automatically from the criteria map keys.
func Choice[T ~string](instructions Entry, criteria map[T]Description) ChoiceQuestion[T] {
	return ChoiceQuestion[T]{
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// ChoiceOptions constructs a ChoiceQuestion[T] where every option label has a nil (undescribed) rubric entry.
// This eliminates `map[string]any{"a": nil, "b": nil}` boilerplate in Go when option names are self-descriptive.
func ChoiceOptions[T ~string](instructions Entry, options ...T) ChoiceQuestion[T] {
	crit := make(map[T]Description, len(options))
	for _, opt := range options {
		crit[opt] = nil
	}
	return ChoiceQuestion[T]{
		Instructions: instructions,
		Criteria:     crit,
	}
}

// ScoreQuestion represents a question that rates state along an ordered rubric of at least two levels.
type ScoreQuestion struct {
	// Instructions describes what the model should rate (string, JSON object, array, or nil).
	Instructions Entry

	// Criteria is an ordered slice of at least two level descriptions indexed from 0.
	Criteria []Entry

	// ExtraFields allows arbitrary extra fields on the question object for forward compatibility.
	ExtraFields map[string]any
}

// QuestionType returns "score".
func (ScoreQuestion) QuestionType() string { return "score" }

func (q ScoreQuestion) toWire() any {
	m := map[string]any{
		"type":         "score",
		"instructions": q.Instructions,
		"criteria":     q.Criteria,
	}
	for k, v := range q.ExtraFields {
		m[k] = v
	}
	return m
}

func (q ScoreQuestion) validate(name string) error {
	if q.Criteria == nil {
		return NewTypeSafeError(
			fmt.Sprintf("Score question %q has criteria that are not a list; score criteria must be a list of descriptions indexed by score from zero.", name),
		)
	}
	if len(q.Criteria) < 2 {
		return NewTypeSafeError(
			fmt.Sprintf("Score question %q has %d criteria; at least two scores are required.", name, len(q.Criteria)),
		)
	}
	return nil
}

func (q ScoreQuestion) MarshalJSON() ([]byte, error) {
	return json.Marshal(q.toWire())
}

// Score constructs a ScoreQuestion with compile-time enforcement of at least two rubric levels
// (first and second are required parameters, followed by any additional levels).
func Score(instructions Entry, first, second Entry, rest ...Entry) ScoreQuestion {
	criteria := make([]Entry, 0, 2+len(rest))
	criteria = append(criteria, first, second)
	criteria = append(criteria, rest...)
	return ScoreQuestion{
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// ScoreSlice constructs a ScoreQuestion from a slice of rubric level descriptions.
// Validation ensures at least 2 criteria before sending the request.
func ScoreSlice(instructions Entry, criteria []Entry) ScoreQuestion {
	return ScoreQuestion{
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// RawQuestion allows passing an arbitrary map[string]any as a question in a SystemOneRequest
// to support forward-compatible question fields or future question types before SDK modeling.
type RawQuestion map[string]any

// QuestionType returns the string value of the "type" key, or "" if absent.
func (q RawQuestion) QuestionType() string {
	if t, ok := q["type"].(string); ok {
		return t
	}
	return ""
}

func (q RawQuestion) toWire() any {
	return map[string]any(q)
}

func (q RawQuestion) validate(name string) error {
	if q == nil {
		return NewTypeSafeError(fmt.Sprintf("Question %q cannot be nil.", name))
	}
	qType := q.QuestionType()
	if qType == "score" {
		rawCrit, exists := q["criteria"]
		if !exists || rawCrit == nil {
			return NewTypeSafeError(
				fmt.Sprintf("Score question %q has criteria that are not a list; score criteria must be a list of descriptions indexed by score from zero.", name),
			)
		}
		rv := reflect.ValueOf(rawCrit)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return NewTypeSafeError(
				fmt.Sprintf("Score question %q has criteria that are not a list; score criteria must be a list of descriptions indexed by score from zero.", name),
			)
		}
		if rv.Len() < 2 {
			return NewTypeSafeError(
				fmt.Sprintf("Score question %q has %d criteria; at least two scores are required.", name, rv.Len()),
			)
		}
	}
	if qType == "choice" {
		rawCrit, exists := q["criteria"]
		if !exists || rawCrit == nil {
			return NewTypeSafeError("Choice criteria must be a map of labels to descriptions, not a list.")
		}
		rv := reflect.ValueOf(rawCrit)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			return NewTypeSafeError("Choice criteria must be a map of labels to descriptions, not a list.")
		}
	}
	return nil
}

// ValidateQuestions enforces client-side validation rules matching both the Python and JS SDKs:
// 1. At least one question must be provided.
// 2. Every score question must have a list/slice of at least 2 criteria.
// 3. Choice criteria must be a map, not a list.
func ValidateQuestions(questions Questions) error {
	if len(questions) == 0 {
		return NewTypeSafeError("At least one question is required.")
	}
	for name, q := range questions {
		if q == nil {
			return NewTypeSafeError(fmt.Sprintf("Question %q cannot be nil.", name))
		}
		if err := q.validate(name); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Bound Generic Question Handles (Compile-Time Typed Answer Extraction)
// ---------------------------------------------------------------------------

// BoundQuestion is implemented by NamedChoice[T], NamedScore, and NamedNoul so they can be
// combined into a heterogeneous Questions map via BindQuestions(...).
type BoundQuestion interface {
	QuestionName() string
	QuestionValue() Question
}

// NamedChoice binds a question key name with a strongly-typed ChoiceQuestion[T],
// enabling compile-time typed extraction of ChoiceResponse[T] from a batch SystemOneResponse.
type NamedChoice[T ~string] struct {
	Name     string
	Question ChoiceQuestion[T]
}

// DefineChoice defines a named, generic ChoiceQuestion[T] handle.
func DefineChoice[T ~string](name string, instructions Entry, criteria map[T]Description) NamedChoice[T] {
	return NamedChoice[T]{
		Name:     name,
		Question: Choice(instructions, criteria),
	}
}

// DefineChoiceOptions defines a named, generic ChoiceQuestion[T] handle with undescribed options.
func DefineChoiceOptions[T ~string](name string, instructions Entry, options ...T) NamedChoice[T] {
	return NamedChoice[T]{
		Name:     name,
		Question: ChoiceOptions(instructions, options...),
	}
}

func (n NamedChoice[T]) QuestionName() string    { return n.Name }
func (n NamedChoice[T]) QuestionValue() Question { return n.Question }

// Answer extracts the typed ChoiceResponse[T] from resp.
func (n NamedChoice[T]) Answer(resp *SystemOneResponse) (ChoiceResponse[T], bool) {
	return GetChoice[T](resp, n.Name)
}

// MustAnswer extracts the typed ChoiceResponse[T] from resp, or returns the zero value if missing.
func (n NamedChoice[T]) MustAnswer(resp *SystemOneResponse) ChoiceResponse[T] {
	ans, _ := GetChoice[T](resp, n.Name)
	return ans
}

// NamedScore binds a question key name with a ScoreQuestion.
type NamedScore struct {
	Name     string
	Question ScoreQuestion
}

// DefineScore defines a named ScoreQuestion handle requiring at least two rubric levels.
func DefineScore(name string, instructions Entry, first, second Entry, rest ...Entry) NamedScore {
	return NamedScore{
		Name:     name,
		Question: Score(instructions, first, second, rest...),
	}
}

func (n NamedScore) QuestionName() string    { return n.Name }
func (n NamedScore) QuestionValue() Question { return n.Question }

// Answer extracts the ScoreResponse from resp.
func (n NamedScore) Answer(resp *SystemOneResponse) (ScoreResponse, bool) {
	return GetScore(resp, n.Name)
}

// MustAnswer extracts the ScoreResponse from resp, or returns the zero value if missing.
func (n NamedScore) MustAnswer(resp *SystemOneResponse) ScoreResponse {
	ans, _ := GetScore(resp, n.Name)
	return ans
}

// NamedNoul binds a question key name with a NoulQuestion.
type NamedNoul struct {
	Name     string
	Question NoulQuestion
}

// DefineNoul defines a named NoulQuestion handle.
func DefineNoul(name string, instructions Entry, criteria ...NoulCriteria) NamedNoul {
	return NamedNoul{
		Name:     name,
		Question: Noul(instructions, criteria...),
	}
}

func (n NamedNoul) QuestionName() string    { return n.Name }
func (n NamedNoul) QuestionValue() Question { return n.Question }

// Answer extracts the NoulResponse from resp.
func (n NamedNoul) Answer(resp *SystemOneResponse) (NoulResponse, bool) {
	return GetNoul(resp, n.Name)
}

// MustAnswer extracts the NoulResponse from resp, or returns the zero value if missing.
func (n NamedNoul) MustAnswer(resp *SystemOneResponse) NoulResponse {
	ans, _ := GetNoul(resp, n.Name)
	return ans
}

// BindQuestions assembles one or more BoundQuestion handles (NamedChoice[T], NamedScore, NamedNoul)
// into a heterogeneous Questions map ready for SystemOneRequest.
func BindQuestions(items ...BoundQuestion) Questions {
	q := make(Questions, len(items))
	for _, item := range items {
		if item != nil {
			q[item.QuestionName()] = item.QuestionValue()
		}
	}
	return q
}
