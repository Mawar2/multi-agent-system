package load_test

// Scenario 4: Full pipeline - 100+ opportunities through fetch → store → agent with 50 workers.
//
// This is the most representative load test. It exercises the complete data path:
//  1. Fetch from cached SAM.gov client
//  2. Expand to 100+ opportunities with synthetic records
//  3. Phase A: 50 workers saving all opportunities to JSONStore
//  4. Phase B: 50 workers running StubAgent on every opportunity ID
//  5. Verify all records are in the store with no goroutine leaks
//
// Run with:
//
//	go test -race -v ./test/load/... -run TestScenario4
//
// For pprof CPU + memory profiles over the full pipeline:
//
//	go test -bench=BenchmarkFullPipeline -cpuprofile=cpu.out -memprofile=mem.out ./test/load/...
//	go tool pprof cpu.out

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Mawar2/Kaimi/internal/agent"
	"github.com/Mawar2/Kaimi/internal/opportunity"
	"github.com/Mawar2/Kaimi/internal/samgov"
	"github.com/Mawar2/Kaimi/internal/store"
)

const (
	pipelineTarget  = 100
	pipelineWorkers = 50
)

// TestScenario4_FullPipelineLoad runs 100+ opportunities through the complete
// fetch → store → agent pipeline with 50 concurrent workers per phase.
func TestScenario4_FullPipelineLoad(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	s, err := store.NewJSONStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	samClient, err := samgov.NewClient(samgov.Config{UseCached: true})
	if err != nil {
		t.Fatalf("failed to create SAM.gov client: %v", err)
	}

	stub := agent.NewStubAgent("pipeline-agent")

	// Fetch real opportunities from the cached fixture.
	fetched, err := samClient.FetchByNAICS(ctx, []string{"541512", "541519", "541330"})
	if err != nil {
		t.Fatalf("failed to fetch from SAM.gov: %v", err)
	}

	// Expand with synthetic opportunities to reach pipelineTarget.
	allOpps := make([]*opportunity.Opportunity, 0, pipelineTarget)
	allOpps = append(allOpps, fetched...)

	now := time.Now().UTC()
	for i := len(allOpps); i < pipelineTarget; i++ {
		allOpps = append(allOpps, makeOpportunity(fmt.Sprintf("synthetic-%04d", i), now))
	}

	goroutinesBefore := runtime.NumGoroutine()

	// Phase A: save all opportunities concurrently with pipelineWorkers.
	saveJobs := make(chan *opportunity.Opportunity, pipelineTarget)
	for _, opp := range allOpps {
		saveJobs <- opp
	}
	close(saveJobs)

	saveErrs := make(chan error, pipelineTarget)
	var saveWg sync.WaitGroup
	for range pipelineWorkers {
		saveWg.Go(func() {
			for opp := range saveJobs {
				if err := s.Save(ctx, opp); err != nil {
					saveErrs <- fmt.Errorf("save %s: %w", opp.ID, err)
				}
			}
		})
	}
	saveWg.Wait()
	close(saveErrs)

	for err := range saveErrs {
		t.Errorf("save phase: %v", err)
	}

	// Phase B: run the agent on every opportunity ID with pipelineWorkers.
	type result struct {
		id  string
		out *agent.AgentResult
	}
	processJobs := make(chan string, pipelineTarget)
	for _, opp := range allOpps {
		processJobs <- opp.ID
	}
	close(processJobs)

	resultsCh := make(chan result, pipelineTarget)
	var processWg sync.WaitGroup
	for range pipelineWorkers {
		processWg.Go(func() {
			for id := range processJobs {
				r, err := stub.Execute(ctx, id)
				if err != nil {
					t.Errorf("agent execute %s: %v", id, err)
					continue
				}
				resultsCh <- result{id: id, out: r}
			}
		})
	}
	processWg.Wait()
	close(resultsCh)

	var processed []result
	for r := range resultsCh {
		processed = append(processed, r)
	}

	if len(processed) != pipelineTarget {
		t.Errorf("expected %d processed, got %d", pipelineTarget, len(processed))
	}

	failed := 0
	for _, r := range processed {
		if r.out.IsFailed() {
			failed++
		}
	}
	if failed > 0 {
		t.Errorf("%d/%d pipeline items failed", failed, len(processed))
	}

	// Store must hold all pipelineTarget records after both phases.
	all, err := s.List(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list store after pipeline: %v", err)
	}
	if len(all) != pipelineTarget {
		t.Errorf("expected %d in store, got %d", pipelineTarget, len(all))
	}

	// Tolerate +10 for Go runtime goroutine variance.
	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+10 {
		t.Errorf("goroutine leak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}

	t.Logf("Scenario 4 complete: %d opportunities processed through full pipeline with %d workers",
		pipelineTarget, pipelineWorkers)
}

// BenchmarkFullPipeline measures the save→execute throughput of one opportunity
// end-to-end, suitable for pprof CPU and memory profiling.
//
// Usage:
//
//	go test -bench=BenchmarkFullPipeline -cpuprofile=cpu.out -memprofile=mem.out ./test/load/...
//	go tool pprof cpu.out
func BenchmarkFullPipeline(b *testing.B) {
	ctx := context.Background()
	tempDir := b.TempDir()
	s, err := store.NewJSONStore(tempDir)
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}
	stub := agent.NewStubAgent("bench-pipeline-agent")
	now := time.Now().UTC()

	b.ResetTimer()
	i := 0
	for b.Loop() {
		opp := makeOpportunity(fmt.Sprintf("bench-%06d", i), now)
		if err := s.Save(ctx, opp); err != nil {
			b.Fatalf("Save failed: %v", err)
		}
		if _, err := stub.Execute(ctx, opp.ID); err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		i++
	}
}
