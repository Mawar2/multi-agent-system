package load_test

// Scenario 3: 100 concurrent calls against the cached SAM.gov client.
//
// The cached client loads fixture data once at construction time and serves
// every FetchByNAICS call from memory. This test verifies there are no data
// races in concurrent reads and that all calls return consistent results.
//
// Run with:
//
//	go test -race -v ./test/load/... -run TestScenario3

import (
	"context"
	"runtime"
	"sync"
	"testing"

	"github.com/Mawar2/Kaimi/internal/samgov"
)

const scenario3Requests = 100

// TestScenario3_SAMGovClientLoad fires 100 concurrent FetchByNAICS calls against the
// cached client and verifies consistency and absence of goroutine leaks.
func TestScenario3_SAMGovClientLoad(t *testing.T) {
	client, err := samgov.NewClient(samgov.Config{UseCached: true})
	if err != nil {
		t.Fatalf("failed to create SAM.gov cached client: %v", err)
	}

	ctx := context.Background()
	// These NAICS codes are present in test/fixtures/samgov_response.json.
	naicsCodes := []string{"541512", "541519", "541330"}

	errs := make(chan error, scenario3Requests)
	counts := make(chan int, scenario3Requests)

	goroutinesBefore := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for range scenario3Requests {
		wg.Go(func() {
			opps, err := client.FetchByNAICS(ctx, naicsCodes)
			if err != nil {
				errs <- err
				return
			}
			counts <- len(opps)
		})
	}

	wg.Wait()
	close(errs)
	close(counts)

	for err := range errs {
		t.Errorf("FetchByNAICS error: %v", err)
	}

	var resultCounts []int
	for c := range counts {
		resultCounts = append(resultCounts, c)
	}

	if len(resultCounts) != scenario3Requests {
		t.Errorf("expected %d successful fetches, got %d", scenario3Requests, len(resultCounts))
	}

	// All calls should return the same count - the fixture is deterministic.
	if len(resultCounts) > 1 {
		baseline := resultCounts[0]
		for j, c := range resultCounts[1:] {
			if c != baseline {
				t.Errorf("inconsistent result at index %d: got %d, want %d", j+1, c, baseline)
			}
		}
		t.Logf("Scenario 3 complete: %d concurrent fetches each returned %d opportunities",
			scenario3Requests, baseline)
	}

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+5 {
		t.Errorf("goroutine leak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// BenchmarkSAMGovFetch measures cached FetchByNAICS throughput for pprof analysis.
//
// Usage:
//
//	go test -bench=BenchmarkSAMGovFetch -cpuprofile=cpu.out ./test/load/...
//	go tool pprof cpu.out
func BenchmarkSAMGovFetch(b *testing.B) {
	client, err := samgov.NewClient(samgov.Config{UseCached: true})
	if err != nil {
		b.Fatalf("failed to create SAM.gov cached client: %v", err)
	}
	ctx := context.Background()
	naicsCodes := []string{"541512", "541519", "541330"}

	b.ResetTimer()
	for b.Loop() {
		if _, err := client.FetchByNAICS(ctx, naicsCodes); err != nil {
			b.Fatalf("FetchByNAICS failed: %v", err)
		}
	}
}
