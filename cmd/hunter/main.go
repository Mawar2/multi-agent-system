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
	keywords := flag.String("keywords", "", "Free-text keywords to search for")
	naics := flag.String("naics", "", "NAICS code filter (e.g. 541511)")
	limit := flag.Int("limit", 10, "Maximum results per page")
	offset := flag.Int("offset", 0, "Pagination offset (zero-based)")
	from := flag.String("from", "", "Posted-from date filter (MM/DD/YYYY)")
	to := flag.String("to", "", "Posted-to date filter (MM/DD/YYYY)")
	flag.Parse()

	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	client := hunter.NewClient(apiKey)

	result, err := client.Search(context.Background(), hunter.SearchParams{
		Keywords:   *keywords,
		NAICSCode:  *naics,
		Limit:      *limit,
		Offset:     *offset,
		PostedFrom: *from,
		PostedTo:   *to,
	})
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("Found %d total opportunities (showing %d)\n\n",
		result.TotalRecords, len(result.Opportunities))

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for i, opp := range result.Opportunities {
		fmt.Printf("--- #%d ---\n", i+1)
		if err := enc.Encode(opp); err != nil {
			log.Printf("encode opportunity: %v", err)
		}
	}
}
