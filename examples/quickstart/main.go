package main

import (
	"context"
	"fmt"
	"log"

	"github.com/orcawhisperer/typesafe-sdk-go"
)

// Department is a strongly-typed Go enum for choice options.
type Department string

const (
	DeptBilling   Department = "billing"
	DeptTechnical Department = "technical"
	DeptSales     Department = "sales"
)

func main() {
	// Reads TYPESAFE_API_KEY from the environment by default.
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatalf("Failed to initialize TypeSafe client: %v", err)
	}
	defer client.Close()

	// Define typed questions once and reuse them across requests.
	billingQ := typesafe.DefineNoul("billing", "Is this ticket about billing?")
	deptQ := typesafe.DefineChoice("department", "Which team should handle this?", map[Department]typesafe.Description{
		DeptBilling:   "Payments, invoicing, refunds",
		DeptTechnical: "Bugs, outages, integrations",
		DeptSales:     "Pricing, upgrades, new accounts",
	})
	urgencyQ := typesafe.DefineScore("urgency", "How urgent is this ticket?", "can wait", "this week", "today")

	resp, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State: map[string]any{
			"document": "I was charged twice. Please fix this ASAP.",
		},
		Questions: typesafe.MustBindQuestions(billingQ, deptQ, urgencyQ),
	})
	if err != nil {
		log.Fatalf("SystemOne request failed: %v", err)
	}

	fmt.Printf("Model: %s (Request ID: %s)\n", resp.Model, resp.RequestID)
	fmt.Printf("Billing probability (Noul): %.2f\n", billingQ.MustAnswer(resp).Noul)

	deptAns := deptQ.MustAnswer(resp)
	fmt.Printf("Department (Choice): %s (Confidence: %.2f)\n", deptAns.Choice, deptAns.Confidence)

	urgencyAns := urgencyQ.MustAnswer(resp)
	fmt.Printf("Urgency (Score): %.2f (Confidence: %.2f)\n", urgencyAns.Score, urgencyAns.Confidence)
}
