// hunter searches SAM.gov for contract opportunities and prints them as JSON.
//
// Usage:
//
//	SAM_API_KEY=<key> hunter <keyword> [limit]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/Mawar2/multi-agent-system/internal/hunter"
)

func main() {
	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <keyword> [limit]\n", os.Args[0])
		os.Exit(1)
	}

	keyword := os.Args[1]
	limit := 10
	if len(os.Args) >= 3 {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n <= 0 {
			log.Fatalf("limit must be a positive integer, got %q", os.Args[2])
		}
		limit = n
	}

	client := hunter.NewClient(apiKey)
	opps, err := client.SearchOpportunities(context.Background(), keyword, limit)
	if err != nil {
		log.Fatalf("search failed: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(opps); err != nil {
		log.Fatalf("encoding results: %v", err)
	}
}
