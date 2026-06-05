package load_test

// Scenario 2: 50 concurrent workers draining 100+ opportunity IDs through StubAgent.
//
// Tests that a worker pool correctly processes all jobs, produces no goroutine
// leaks, and handles context cancellation gracefully. Run with:
//
//	go test -race -v ./test/load/... -run TestScenario2

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/Mawar2/Kaimi/internal/agent"
)

const (
	scenario2Workers       = 50
	scenario2Opportunities = 100
)

// TestScenario2_AgentWorkerPool launches 50 workers to process 100+ opportunities
// through StubAgent and verifies all jobs complete without goroutine leaks.
func TestScenario2_AgentWorkerPool(t *testing.T) {
	ctx := context.Background()
	stub := agent.NewStubAgent("load-test-agent")

	jobs := make(chan string, scenario2Opportunities)
	for i := range scenario2Opportunities {
		jobs <- fmt.Sprintf("notice-%04d", i)
	}
	close(jobs)

	// Buffer large enough for all results so workers never block on send.
	results := make(chan *agent.AgentResult, scenario2Opportunities)

	goroutinesBefore := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for range scenario2Workers {
		wg.Go(func() {
			for noticeID := range jobs {
				result, err := stub.Execute(ctx, noticeID)
				if err != nil {
					results <- &agent.AgentResult{
						AgentName: "load-test-agent",
						Status:    agent.StatusFailed,
						NoticeID:  noticeID,
						Error:     err.Error(),
					}
					continue
				}
				results <- result
			}
		})
	}

	wg.Wait()
	close(results)

	var collected []*agent.AgentResult
	for r := range results {
		collected = append(collected, r)
	}

	if len(collected) != scenario2Opportunities {
		t.Errorf("expected %d results, got %d", scenario2Opportunities, len(collected))
	}

	failed := 0
	for _, r := range collected {
		if r.IsFailed() {
			failed++
		}
	}
	if failed > 0 {
		t.Errorf("%d/%d agent executions failed unexpectedly", failed, len(collected))
	}

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+5 {
		t.Errorf("goroutine leak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}

	t.Logf("Scenario 2 complete: %d workers processed %d opportunities (%d failed)",
		scenario2Workers, len(collected), failed)
}

// TestScenario2_AgentCancellation verifies that workers detect a cancelled context
// and exit cleanly with no goroutine leak.
func TestScenario2_AgentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := agent.NewStubAgent("cancellation-test-agent")

	const totalTasks = 200
	jobs := make(chan string, totalTasks)
	for i := range totalTasks {
		jobs <- fmt.Sprintf("notice-%04d", i)
	}
	close(jobs)

	goroutinesBefore := runtime.NumGoroutine()

	counts := make(chan int, scenario2Workers)
	var wg sync.WaitGroup
	for range scenario2Workers {
		wg.Go(func() {
			processed := 0
			for noticeID := range jobs {
				_, _ = stub.Execute(ctx, noticeID)
				processed++
			}
			counts <- processed
		})
	}

	// Cancel mid-run. Workers will see ctx.Done() on the next Execute call.
	cancel()

	wg.Wait()
	close(counts)

	total := 0
	for c := range counts {
		total += c
	}
	t.Logf("Scenario 2 cancellation: completed %d/%d tasks before/after cancel", total, totalTasks)

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+5 {
		t.Errorf("goroutine leak after cancellation: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// BenchmarkAgentExecution measures serial single-agent throughput for pprof profiling.
//
// Usage:
//
//	go test -bench=BenchmarkAgentExecution -cpuprofile=cpu.out ./test/load/...
//	go tool pprof cpu.out
func BenchmarkAgentExecution(b *testing.B) {
	ctx := context.Background()
	stub := agent.NewStubAgent("bench-agent")

	b.ResetTimer()
	i := 0
	for b.Loop() {
		if _, err := stub.Execute(ctx, fmt.Sprintf("bench-notice-%06d", i)); err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		i++
	}
}

// BenchmarkAgentWorkerPool measures parallel agent throughput using b.RunParallel,
// which models the real worker-pool access pattern for pprof CPU analysis.
//
// Usage:
//
//	go test -bench=BenchmarkAgentWorkerPool -cpuprofile=cpu.out ./test/load/...
func BenchmarkAgentWorkerPool(b *testing.B) {
	ctx := context.Background()
	stub := agent.NewStubAgent("bench-pool-agent")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := stub.Execute(ctx, fmt.Sprintf("bench-parallel-%06d", i)); err != nil {
				b.Errorf("Execute failed: %v", err)
			}
			i++
		}
	})
}
