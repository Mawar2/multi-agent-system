// Command hunter discovers federal contracting opportunities on SAM.gov
// and prints them for routing into the multi-agent task queue.
//
// Usage:
//
//	SAM_API_KEY=<key> hunter [--keywords kw1,kw2] [--naics 541511] [--limit 100]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/hunter"
)

func main() {
	keywords := flag.String("keywords", "", "comma-separated search keywords")
	naics := flag.String("naics", "", "comma-separated NAICS codes")
	setAsides := flag.String("setasides", "", "comma-separated set-aside types (e.g. SBA)")
	days := flag.Int("days", 30, "only show opportunities posted within this many days")
	limit := flag.Int("limit", 100, "maximum number of results")
	flag.Parse()

	apiKey := os.Getenv("SAM_API_KEY")
	if apiKey == "" {
		log.Fatal("SAM_API_KEY environment variable not set")
	}

	client := hunter.NewSAMGovClient(apiKey)

	q := hunter.SearchQuery{
		PostedAfter: time.Now().AddDate(0, 0, -*days),
		Limit:       *limit,
	}
	if *keywords != "" {
		q.Keywords = strings.Split(*keywords, ",")
	}
	if *naics != "" {
		q.NAICSCodes = strings.Split(*naics, ",")
	}
	if *setAsides != "" {
		q.SetAsides = strings.Split(*setAsides, ",")
	}

	ctx := context.Background()
	opps, err := client.Search(ctx, q)
	if err != nil {
		log.Fatalf("SAM.gov search failed: %v", err)
	}

	fmt.Printf("Found %d opportunities (last %d days)\n\n", len(opps), *days)
	for i, opp := range opps {
		fmt.Printf("%d. [%s] %s\n", i+1, opp.NoticeID, opp.Title)
		if opp.Agency != "" {
			fmt.Printf("   Agency:  %s\n", opp.Agency)
		}
		if opp.NAICSCode != "" {
			fmt.Printf("   NAICS:   %s\n", opp.NAICSCode)
		}
		if opp.ResponseDeadline != "" {
			fmt.Printf("   Due:     %s\n", opp.ResponseDeadline)
		}
		if opp.SetAside != "" {
			fmt.Printf("   Set-Aside: %s\n", opp.SetAside)
		}
		if opp.UIURL != "" {
			fmt.Printf("   URL:     %s\n", opp.UIURL)
		}
		fmt.Println()
	}
}
