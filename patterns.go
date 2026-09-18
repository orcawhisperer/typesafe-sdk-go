package typesafe

import (
	"context"
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Architectural Pattern 1: Confidence-Gated Routing
// (https://docs.typesafe.ai/patterns/confidence-routing)
// ---------------------------------------------------------------------------

// GateAction indicates the routing decision based on confidence and probability thresholds.
type GateAction string

const (
	// GateActionAct means the model's confidence meets or exceeds the threshold and code can act automatically.
	GateActionAct GateAction = "act"

	// GateActionReview means the model's confidence is below the automatic action threshold and should be routed to human/LLM review.
	GateActionReview GateAction = "review"

	// GateActionAbstain means confidence is below the minimum review floor and the system should abstain or fall back.
	GateActionAbstain GateAction = "abstain"
)

// ConfidenceDecision holds the evaluated answer along with the confidence-gated routing decision.
type ConfidenceDecision[T any] struct {
	Answer     T
	Confidence float64
	Action     GateAction
}

// RouteChoice evaluates a ChoiceResponse[T] against an automatic action confidence threshold
// and an optional lower review threshold.
// If ans.Confidence >= actThreshold -> GateActionAct.
// If ans.Confidence >= reviewThreshold -> GateActionReview.
// Otherwise -> GateActionAbstain.
func RouteChoice[T ~string](ans ChoiceResponse[T], actThreshold float64, reviewThreshold ...float64) ConfidenceDecision[T] {
	revFloor := 0.0
	if len(reviewThreshold) > 0 {
		revFloor = reviewThreshold[0]
	}
	action := GateActionAbstain
	if ans.Confidence >= actThreshold {
		action = GateActionAct
	} else if ans.Confidence >= revFloor {
		action = GateActionReview
	}
	return ConfidenceDecision[T]{
		Answer:     ans.Choice,
		Confidence: ans.Confidence,
		Action:     action,
	}
}

// RouteScore evaluates a ScoreResponse against an automatic action confidence threshold
// and an optional lower review threshold.
func RouteScore(ans ScoreResponse, actThreshold float64, reviewThreshold ...float64) ConfidenceDecision[float64] {
	revFloor := 0.0
	if len(reviewThreshold) > 0 {
		revFloor = reviewThreshold[0]
	}
	action := GateActionAbstain
	if ans.Confidence >= actThreshold {
		action = GateActionAct
	} else if ans.Confidence >= revFloor {
		action = GateActionReview
	}
	return ConfidenceDecision[float64]{
		Answer:     ans.Score,
		Confidence: ans.Confidence,
		Action:     action,
	}
}

// RouteNoul evaluates a NoulResponse (yes/no probability in [0, 1]) against upper ("yes") and lower ("no")
// certainty thresholds, routing uncertain middle probabilities in (noMaxThreshold, yesMinThreshold) to review.
func RouteNoul(ans NoulResponse, yesMinThreshold, noMaxThreshold float64) ConfidenceDecision[bool] {
	switch {
	case ans.Noul >= yesMinThreshold:
		return ConfidenceDecision[bool]{Answer: true, Confidence: ans.Noul, Action: GateActionAct}
	case ans.Noul <= noMaxThreshold:
		return ConfidenceDecision[bool]{Answer: false, Confidence: 1.0 - ans.Noul, Action: GateActionAct}
	default:
		return ConfidenceDecision[bool]{Answer: ans.Noul >= 0.5, Confidence: ans.Noul, Action: GateActionReview}
	}
}

// ---------------------------------------------------------------------------
// Architectural Pattern 2: Composite Scoring
// (https://docs.typesafe.ai/patterns/composite-scoring)
// ---------------------------------------------------------------------------

// ScoreDimension defines a single atomic score question name, its weight, and optional max rubric index for normalization.
type ScoreDimension struct {
	// Name is the question key in SystemOneResponse.Scores (or SystemOneResponse.Nouls).
	Name string

	// Weight is the relative weight of this dimension in the composite score.
	Weight float64

	// MaxScore is the maximum score index for normalizing a ScoreResponse onto [0, 1] (e.g., float64(len(criteria)-1)).
	// If MaxScore <= 0, the raw Score value is used without [0, 1] normalization.
	MaxScore float64
}

// CompositeScoreResult holds the weighted composite score and minimum/weighted confidence across dimensions.
type CompositeScoreResult struct {
	// WeightedScore is the weighted average across all evaluated dimensions.
	WeightedScore float64

	// WeightedConfidence is the weight-averaged confidence across all Score dimensions.
	WeightedConfidence float64

	// MinConfidence is the lowest confidence across any participating Score dimension (useful for conservative gating).
	MinConfidence float64

	// Contributions maps each dimension Name to its normalized weighted contribution.
	Contributions map[string]float64
}

// ComputeCompositeScore combines multiple atomic Score (or Noul) answers from a SystemOneResponse
// using code-controlled weights.
func ComputeCompositeScore(resp *SystemOneResponse, dimensions []ScoreDimension) (*CompositeScoreResult, error) {
	if resp == nil {
		return nil, NewTypeSafeError("Cannot compute composite score on a nil SystemOneResponse.")
	}
	if len(dimensions) == 0 {
		return nil, NewTypeSafeError("At least one ScoreDimension is required for ComputeCompositeScore.")
	}

	var totalWeight, weightedSum, confWeightSum, weightedConfSum float64
	minConf := 1.0
	hasConf := false
	contributions := make(map[string]float64, len(dimensions))

	for _, dim := range dimensions {
		if dim.Weight < 0 {
			return nil, NewTypeSafeError(fmt.Sprintf("Dimension %q has negative weight %v.", dim.Name, dim.Weight))
		}
		var val float64
		if sAns, ok := resp.Scores[dim.Name]; ok {
			val = sAns.Score
			if dim.MaxScore > 0 {
				val = val / dim.MaxScore
			}
			weightedConfSum += sAns.Confidence * dim.Weight
			confWeightSum += dim.Weight
			if !hasConf || sAns.Confidence < minConf {
				minConf = sAns.Confidence
				hasConf = true
			}
		} else if nAns, ok := resp.Nouls[dim.Name]; ok {
			val = nAns.Noul
		} else {
			return nil, NewTypeSafeError(fmt.Sprintf("Dimension %q not found in response Scores or Nouls.", dim.Name))
		}

		contrib := val * dim.Weight
		contributions[dim.Name] = contrib
		weightedSum += contrib
		totalWeight += dim.Weight
	}

	if totalWeight == 0 {
		return nil, NewTypeSafeError("Total weight across dimensions must be greater than zero.")
	}

	avgConf := 1.0
	if confWeightSum > 0 {
		avgConf = weightedConfSum / confWeightSum
	}
	if !hasConf {
		minConf = 1.0
	}

	return &CompositeScoreResult{
		WeightedScore:      weightedSum / totalWeight,
		WeightedConfidence: avgConf,
		MinConfidence:      minConf,
		Contributions:      contributions,
	}, nil
}

// ---------------------------------------------------------------------------
// Architectural Pattern 3: Concurrent Batch Evaluation (Worker Pool)
// ---------------------------------------------------------------------------

// BatchItemResult holds the result or error for a single item in a concurrent BatchSystemOne call.
type BatchItemResult struct {
	Index    int
	Response *SystemOneResponse
	Err      error
}

// BatchSystemOne evaluates multiple SystemOneRequest items concurrently using a bounded worker pool
// (defaulting to 8 concurrent workers if concurrency <= 0), preserving input order in the returned slice.
func (c *Client) BatchSystemOne(
	ctx context.Context,
	requests []SystemOneRequest,
	concurrency int,
	opts ...RequestOption,
) []BatchItemResult {
	n := len(requests)
	results := make([]BatchItemResult, n)
	if n == 0 {
		return results
	}
	if concurrency <= 0 {
		concurrency = 8
	}
	if concurrency > n {
		concurrency = n
	}

	jobs := make(chan int, n)
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for w := 0; w < concurrency; w++ {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				resp, err := c.SystemOne(ctx, requests[idx], opts...)
				results[idx] = BatchItemResult{
					Index:    idx,
					Response: resp,
					Err:      err,
				}
			}
		}()
	}
	wg.Wait()
	return results
}
