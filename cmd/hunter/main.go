package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Mawar2/multi-agent-system/internal/hunter"
)

func main() {
	keyword := flag.String("keyword", "", "Search keyword (e.g. \"software development\")")
	naics := flag.String("naics", "", "NAICS code filter (e.g. 541511)")
	limit := flag.Int("limit", 25, "Maximum number of results to return")
	postedFrom := flag.String("from", "", "Earliest post date MM/DD/YYYY")
	postedTo := flag.String("to", "", "Latest post date MM/DD/YYYY")
	flag.Parse()

	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	client := hunter.NewClient(apiKey)

	fmt.Fprintf(os.Stderr, "Searching SAM.gov opportunities...\n")

	result, err := client.Search(context.Background(), hunter.SearchParams{
		Keyword:    *keyword,
		NAICS:      *naics,
		Limit:      *limit,
		PostedFrom: *postedFrom,
		PostedTo:   *postedTo,
	})
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Found %d total records, returning %d\n",
		result.TotalRecords, len(result.Opportunities))

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		log.Fatalf("Failed to encode results: %v", err)
	}
}
