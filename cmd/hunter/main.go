package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Mawar2/multi-agent-system/internal/hunter"
)

func main() {
	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	ctx := context.Background()
	client := hunter.NewClient(apiKey)

	opts := hunter.SearchOptions{
		Keywords: []string{
			"software development",
			"information technology",
			"cloud services",
			"cybersecurity",
			"data analytics",
		},
		DaysBack:   7,
		Limit:      100,
		NAICSCodes: []string{"541511", "541512", "541513", "541519", "518210"},
	}

	fmt.Println("Searching SAM.gov for IT/software contract opportunities...")

	result, err := client.Search(ctx, opts)
	if err != nil {
		log.Fatalf("SAM.gov search failed: %v", err)
	}

	fmt.Printf("Found %d total opportunities (%d matching NAICS filter)\n\n",
		result.Total, len(result.Opportunities))

	for i, opp := range result.Opportunities {
		fmt.Printf("[%d] %s\n", i+1, opp.Title)
		fmt.Printf("    Agency:   %s\n", opp.Agency)
		fmt.Printf("    Type:     %s\n", opp.Type)
		fmt.Printf("    NAICS:    %s\n", opp.NAICSCode)
		fmt.Printf("    SetAside: %s\n", opp.SetAside)
		fmt.Printf("    Posted:   %s\n", opp.PostedDate)
		fmt.Printf("    Deadline: %s\n", opp.ResponseDeadLine)
		if len(opp.PointOfContact) > 0 {
			poc := opp.PointOfContact[0]
			fmt.Printf("    Contact:  %s <%s>\n", poc.FullName, poc.Email)
		}
		if opp.UILink != "" {
			fmt.Printf("    Link:     %s\n", opp.UILink)
		}
		fmt.Println()
	}

	if len(result.Opportunities) == 0 {
		fmt.Println("No opportunities found matching the search criteria.")
		fmt.Println("Try adjusting keywords, date range, or NAICS codes.")
	}
}
