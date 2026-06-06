// Command hunter queries SAM.gov for contracting opportunities and prints them.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Mawar2/multi-agent-system/internal/hunter"
)

func main() {
	keywords := flag.String("keywords", "", "Free-text keyword search (e.g. \"software development\")")
	naics := flag.String("naics", "", "NAICS code filter (e.g. \"541511\")")
	limit := flag.Int("limit", 10, "Maximum number of results (1-100)")
	offset := flag.Int("offset", 0, "Pagination offset")
	flag.Parse()

	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	client := hunter.NewClient(apiKey)

	opts := hunter.SearchOptions{
		Keywords:  *keywords,
		NAICSCode: *naics,
		Limit:     *limit,
		Offset:    *offset,
	}

	result, err := client.Search(context.Background(), opts)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("Found %d total opportunities (showing %d)\n\n", result.TotalRecords, len(result.Opportunities))

	for i, opp := range result.Opportunities {
		fmt.Printf("[%d] %s\n", i+1, opp.Title)
		fmt.Printf("    Notice ID:    %s\n", opp.NoticeID)
		fmt.Printf("    NAICS:        %s\n", opp.NAICS)
		fmt.Printf("    Department:   %s\n", opp.Department)
		fmt.Printf("    Type:         %s\n", opp.Type)
		fmt.Printf("    Posted:       %s\n", opp.PostedDate)
		fmt.Printf("    Response due: %s\n", opp.ResponseDate)
		fmt.Printf("    Active:       %s\n", opp.Active)
		fmt.Println()
	}
}
