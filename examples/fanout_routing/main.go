package main

import (
	"context"
	"fmt"
	"log"

	"github.com/orcawhisperer/typesafe-sdk-go"
)

type Intent string

const (
	IntentRefund   Intent = "refund"
	IntentBug      Intent = "bug_report"
	IntentQuestion Intent = "general_question"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	intentQ := typesafe.DefineChoice("intent", "Classify the customer's primary intent.", map[Intent]typesafe.Description{
		IntentRefund:   "Customer requests money back or disputes a charge",
		IntentBug:      "Customer reports broken functionality or an outage",
		IntentQuestion: "Customer asks how a feature works",
	})
	hasOrderIDQ := typesafe.DefineNoul("has_order_id", "Does the message include an order or invoice ID?")
	severityQ := typesafe.DefineScore("severity", "How severe is the business impact?", "Minor", "Moderate", "Critical")

	// Speculative fan-out: send all questions in a single round-trip and let Go code decide what is relevant.
	resp, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State:     "Order #INV-90210 charged my card twice! Please refund the duplicate charge immediately.",
		Questions: typesafe.BindQuestions(intentQ, hasOrderIDQ, severityQ),
	})
	if err != nil {
		log.Fatalf("Evaluation failed: %v", err)
	}

	// Confidence-gated routing: act automatically when confidence >= 0.80, review when >= 0.50.
	decision := typesafe.RouteChoice(intentQ.MustAnswer(resp), 0.80, 0.50)
	fmt.Printf("Intent: %s | Confidence: %.2f | Gate Action: %s\n", decision.Answer, decision.Confidence, decision.Action)

	if decision.Action == typesafe.GateActionAct && decision.Answer == IntentRefund {
		if hasOrderIDQ.MustAnswer(resp).Noul > 0.80 {
			fmt.Println("-> Triggering automated refund check workflow.")
		} else {
			fmt.Println("-> Prompting customer for their order ID.")
		}
	}
}
