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
	keyword := flag.String("keyword", "", "keyword filter for opportunities")
	limit := flag.Int("limit", 25, "maximum number of results")
	postedFrom := flag.String("from", "", "posted-from date (YYYY-MM-DD)")
	postedTo := flag.String("to", "", "posted-to date (YYYY-MM-DD)")
	flag.Parse()

	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	client := hunter.NewClient(apiKey)

	result, err := client.Search(context.Background(), hunter.SearchParams{
		Keyword:    *keyword,
		PostedFrom: *postedFrom,
		PostedTo:   *postedTo,
		Limit:      *limit,
	})
	if err != nil {
		log.Fatalf("search failed: %v", err)
	}

	fmt.Printf("Found %d opportunities\n\n", result.TotalRecords)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for _, opp := range result.Opportunities {
		if err := enc.Encode(opp); err != nil {
			log.Printf("encode error: %v", err)
		}
	}
}
