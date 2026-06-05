package load_test

// Scenario 1: JSONStore under 100 concurrent writers and 50 concurrent readers.
//
// Tests that the RWMutex in JSONStore correctly serialises writers and allows
// parallel readers without data races or panics. Run with:
//
//	go test -race -v ./test/load/... -run TestScenario1

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Mawar2/Kaimi/internal/opportunity"
	"github.com/Mawar2/Kaimi/internal/store"
)

const (
	scenario1Writers = 100
	scenario1Readers = 50
)

// TestScenario1_StoreLoad launches 100 writers and 50 readers simultaneously against
// a single JSONStore and verifies correctness and absence of goroutine leaks.
func TestScenario1_StoreLoad(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	s, err := store.NewJSONStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	now := time.Now().UTC()

	// Pre-seed 50 opportunities so readers always have data to return.
	for i := range 50 {
		opp := makeOpportunity(fmt.Sprintf("seed-%04d", i), now)
		if err := s.Save(ctx, opp); err != nil {
			t.Fatalf("failed to pre-seed opportunity %d: %v", i, err)
		}
	}

	goroutinesBefore := runtime.NumGoroutine()

	var wg sync.WaitGroup
	writeErrs := make(chan error, scenario1Writers)
	readErrs := make(chan error, scenario1Readers)

	// 100 concurrent writers each writing a unique opportunity.
	for i := range scenario1Writers {
		wg.Go(func() {
			opp := makeOpportunity(fmt.Sprintf("writer-%04d", i), now)
			if err := s.Save(ctx, opp); err != nil {
				writeErrs <- fmt.Errorf("writer %d: %w", i, err)
			}
		})
	}

	// 50 concurrent readers listing all opportunities.
	for i := range scenario1Readers {
		wg.Go(func() {
			if _, err := s.List(ctx, nil); err != nil {
				readErrs <- fmt.Errorf("reader %d: %w", i, err)
			}
		})
	}

	wg.Wait()
	close(writeErrs)
	close(readErrs)

	for err := range writeErrs {
		t.Errorf("write error: %v", err)
	}
	for err := range readErrs {
		t.Errorf("read error: %v", err)
	}

	// All 100 written + 50 seeded records must be present.
	all, err := s.List(ctx, nil)
	if err != nil {
		t.Fatalf("final list failed: %v", err)
	}
	if len(all) != scenario1Writers+50 {
		t.Errorf("expected %d opportunities, got %d", scenario1Writers+50, len(all))
	}

	// Tolerate up to +5 goroutines for Go runtime variance.
	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+5 {
		t.Errorf("goroutine leak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}

	t.Logf("Scenario 1 complete: %d writes + %d concurrent reads; store holds %d records",
		scenario1Writers, scenario1Readers, len(all))
}

// BenchmarkStoreWrite measures serial write throughput for pprof CPU profiling.
//
// Usage:
//
//	go test -bench=BenchmarkStoreWrite -cpuprofile=cpu.out ./test/load/...
//	go tool pprof cpu.out
func BenchmarkStoreWrite(b *testing.B) {
	tempDir := b.TempDir()
	s, err := store.NewJSONStore(tempDir)
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()

	b.ResetTimer()
	i := 0
	for b.Loop() {
		opp := makeOpportunity(fmt.Sprintf("bench-%06d", i), now)
		if err := s.Save(ctx, opp); err != nil {
			b.Fatalf("Save failed: %v", err)
		}
		i++
	}
}

// BenchmarkStoreRead measures list throughput over 100 pre-seeded records.
//
// Usage:
//
//	go test -bench=BenchmarkStoreRead -memprofile=mem.out ./test/load/...
//	go tool pprof mem.out
func BenchmarkStoreRead(b *testing.B) {
	tempDir := b.TempDir()
	s, err := store.NewJSONStore(tempDir)
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()

	for i := range 100 {
		opp := makeOpportunity(fmt.Sprintf("seed-%06d", i), now)
		if err := s.Save(ctx, opp); err != nil {
			b.Fatalf("failed to seed: %v", err)
		}
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := s.List(ctx, nil); err != nil {
			b.Fatalf("List failed: %v", err)
		}
	}
}

// makeOpportunity builds a minimal Opportunity for load testing.
// Defined here and shared across all load test files in this package.
func makeOpportunity(id string, now time.Time) *opportunity.Opportunity {
	return &opportunity.Opportunity{
		ID:               id,
		Title:            "Load Test: " + id,
		Agency:           "Test Agency",
		NAICSCode:        "541512",
		PostedDate:       now,
		ResponseDeadline: now.Add(30 * 24 * time.Hour),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}
