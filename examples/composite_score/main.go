package main

import (
	"context"
	"fmt"
	"log"

	"github.com/orcawhisperer/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	resp, err := client.Ask(
		context.Background(),
		"URGENT: Production database connection pool is exhausted and checkout is returning 500 errors for all users.",
		typesafe.Questions{
			"scope":        typesafe.Score("How many users are affected?", "Single user", "Subset of users", "All users"),
			"reversibility": typesafe.Score("How hard is it to work around?", "Easy workaround", "Difficult workaround", "Complete blocker"),
			"is_security":  typesafe.Noul("Does this involve a security breach or data leak?"),
		},
	)
	if err != nil {
		log.Fatalf("Ask failed: %v", err)
	}

	// Combine atomic scores in code using explicit weights.
	composite, err := typesafe.ComputeCompositeScore(resp, []typesafe.ScoreDimension{
		{Name: "scope", Weight: 0.50, MaxScore: 2.0},
		{Name: "reversibility", Weight: 0.35, MaxScore: 2.0},
		{Name: "is_security", Weight: 0.15},
	})
	if err != nil {
		log.Fatalf("Composite score failed: %v", err)
	}

	fmt.Printf("Composite Priority Score: %.3f (Min Confidence: %.2f)\n",
		composite.WeightedScore, composite.MinConfidence)
}
