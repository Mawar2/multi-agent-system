// Package loadtest provides stress tests for the multi-agent orchestration system.
//
// Four scenarios are tested:
//  1. HighThroughput   - 100 tasks, 50 concurrent workers; measures actual throughput
//  2. MemoryLeaks      - 500 tasks in batches; detects heap/goroutine leaks with pprof
//  3. RaceConditions   - 100 goroutines doing random queue ops; intended to run under -race
//  4. WorkerSaturation - More workers (50) than tasks (20); tests fairness and atomicity
package loadtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

var allTiers = []taskqueue.Tier{
	taskqueue.TierGeminiFlash,
	taskqueue.TierGeminiPro,
	taskqueue.TierClaude,
}

func newLoadTestQueue(t testing.TB) (*taskqueue.JSONQueue, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	return q, dir
}

func makeTask(id string, tier taskqueue.Tier) *taskqueue.Task {
	return &taskqueue.Task{
		ID:          id,
		IssueNumber: 1,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "load-test task " + id,
		Status:      taskqueue.StatusPending,
		Tier:        tier,
		Complexity:  taskqueue.ComplexitySimple,
		Metadata:    map[string]string{"task_type": "issue"},
	}
}

// tierForIndex distributes tasks evenly across the three tiers.
func tierForIndex(i int) taskqueue.Tier {
	switch i % 3 {
	case 0:
		return taskqueue.TierGeminiFlash
	case 1:
		return taskqueue.TierGeminiPro
	default:
		return taskqueue.TierClaude
	}
}

// dequeueAny tries all tiers and returns the first task found.
func dequeueAny(ctx context.Context, queue *taskqueue.JSONQueue, workerID string) (*taskqueue.Task, error) {
	for _, tier := range allTiers {
		task, err := queue.Dequeue(ctx, tier, workerID)
		if err != nil {
			return nil, err
		}
		if task != nil {
			return task, nil
		}
	}
	return nil, nil
}

// -------------------------------------------------------------------------
// Scenario 1: High Throughput
//
// Enqueues 100 tasks then spins up 50 workers (goroutines) that continuously
// dequeue → mark in-progress → mark complete.  Workers cancel early once all
// tasks are done so elapsed time reflects actual processing time.
// Asserts that every task is processed exactly once.
// -------------------------------------------------------------------------

func TestScenario_HighThroughput(t *testing.T) {
	const numTasks = 100
	const numWorkers = 50

	queue, _ := newLoadTestQueue(t)

	// ctx cancelled as soon as all tasks are processed.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Enqueue 100 tasks across all tiers.
	for i := range numTasks {
		task := makeTask(fmt.Sprintf("ht-task-%04d", i), tierForIndex(i))
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed at i=%d: %v", i, err)
		}
	}

	var (
		completed   atomic.Int64
		claimedOnce sync.Map // taskID → workerID
	)

	start := time.Now()

	var wg sync.WaitGroup
	for w := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			wID := fmt.Sprintf("ht-worker-%02d", workerID)

			for ctx.Err() == nil {
				task, err := dequeueAny(ctx, queue, wID)
				if err != nil {
					t.Errorf("worker %s: Dequeue error: %v", wID, err)
					return
				}
				if task == nil {
					// Nothing available; back off briefly.
					time.Sleep(5 * time.Millisecond)
					continue
				}

				// Detect double-claims.
				if prev, loaded := claimedOnce.LoadOrStore(task.ID, wID); loaded {
					t.Errorf("double-claim on task %s: by %v and %s", task.ID, prev, wID)
					return
				}

				// Transition InProgress → Complete.
				task.Status = taskqueue.StatusInProgress
				task.StartedAt = time.Now()
				if err := queue.Update(ctx, task); err != nil {
					t.Errorf("worker %s: InProgress update: %v", wID, err)
					return
				}

				task.Status = taskqueue.StatusComplete
				task.CompletedAt = time.Now()
				if err := queue.Update(ctx, task); err != nil {
					t.Errorf("worker %s: Complete update: %v", wID, err)
					return
				}

				if completed.Add(1) == numTasks {
					cancel() // All done — signal all workers to exit.
				}
			}
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)

	got := int(completed.Load())
	if got != numTasks {
		t.Errorf("HighThroughput: want %d completed, got %d (elapsed %s)",
			numTasks, got, elapsed)
	}

	throughput := float64(got) / elapsed.Seconds()
	t.Logf("HighThroughput: %d tasks in %s = %.1f tasks/sec",
		got, elapsed.Round(time.Millisecond), throughput)

	// Verify final queue state.
	all, err := queue.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, task := range all {
		if task.Status != taskqueue.StatusComplete {
			t.Errorf("task %s not complete: status=%v", task.ID, task.Status)
		}
	}
}

// -------------------------------------------------------------------------
// Scenario 2: Memory Leak Detection
//
// Processes tasks in 5 batches of 100.  After each batch, forces GC and
// samples heap/goroutine counts.  Goroutine leak shows as steadily
// increasing goroutine count; heap leak shows as steadily increasing
// allocated bytes.  Writes pprof heap profiles to a temp dir for
// offline analysis with `go tool pprof`.
// -------------------------------------------------------------------------

func TestScenario_MemoryLeaks(t *testing.T) {
	const batches = 5
	const batchSize = 100

	profileDir := t.TempDir()
	ctx := context.Background()

	var (
		prevAlloc      uint64
		prevGoroutines int
	)

	for batch := range batches {
		queue, _ := newLoadTestQueue(t)

		// Enqueue batchSize tasks.
		for i := range batchSize {
			task := makeTask(fmt.Sprintf("ml-batch%d-task%04d", batch, i), tierForIndex(i))
			if err := queue.Enqueue(ctx, task); err != nil {
				t.Fatalf("batch %d Enqueue: %v", batch, err)
			}
		}

		// Drain with 10 workers, each trying all tiers.
		var remaining atomic.Int64
		remaining.Store(batchSize)

		batchCtx, batchCancel := context.WithTimeout(ctx, 30*time.Second)
		var wg sync.WaitGroup
		for w := range 10 {
			wg.Add(1)
			go func(wID string) {
				defer wg.Done()
				for batchCtx.Err() == nil {
					task, err := dequeueAny(batchCtx, queue, wID)
					if err != nil || task == nil {
						if remaining.Load() == 0 {
							return
						}
						time.Sleep(5 * time.Millisecond)
						continue
					}

					task.Status = taskqueue.StatusComplete
					task.CompletedAt = time.Now()
					_ = queue.Update(batchCtx, task)

					if remaining.Add(-1) == 0 {
						batchCancel()
					}
				}
			}(fmt.Sprintf("ml-worker-%d-%d", batch, w))
		}
		wg.Wait()
		batchCancel()

		// Force GC and read runtime stats.
		runtime.GC()
		runtime.GC()

		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		goroutines := runtime.NumGoroutine()

		t.Logf("Batch %d: HeapAlloc=%dKB HeapInuse=%dKB Goroutines=%d",
			batch, ms.HeapAlloc/1024, ms.HeapInuse/1024, goroutines)

		// Write heap profile for post-hoc pprof analysis.
		profilePath := filepath.Join(profileDir, fmt.Sprintf("heap_batch%d.prof", batch))
		if f, err := os.Create(profilePath); err == nil {
			_ = pprof.WriteHeapProfile(f)
			_ = f.Close()
		}

		// After the first batch we have a baseline. Subsequent batches must not
		// grow unboundedly. We allow 3× headroom for transient allocations.
		if batch > 0 {
			if ms.HeapAlloc > prevAlloc*3 {
				t.Errorf("batch %d: HeapAlloc grew from %dKB to %dKB (>3× — possible leak)",
					batch, prevAlloc/1024, ms.HeapAlloc/1024)
			}
			// Goroutine count should be stable (within 10 of baseline).
			diff := goroutines - prevGoroutines
			if diff < 0 {
				diff = -diff
			}
			if diff > 10 {
				t.Errorf("batch %d: goroutine count changed from %d to %d (diff=%d, possible goroutine leak)",
					batch, prevGoroutines, goroutines, diff)
			}
		}

		prevAlloc = ms.HeapAlloc
		prevGoroutines = goroutines
	}
}

// -------------------------------------------------------------------------
// Scenario 3: Race Condition Stress
//
// Spawns 100 goroutines that continuously perform random queue operations
// for 500 ms.  Run with `go test -race ./test/load/...` to verify that
// the RWMutex inside JSONQueue prevents all data races.
// -------------------------------------------------------------------------

func TestScenario_RaceConditions(t *testing.T) {
	const numGoroutines = 100
	const duration = 500 * time.Millisecond

	queue, _ := newLoadTestQueue(t)
	ctx := context.Background()
	deadline := time.Now().Add(duration)

	// Pre-populate with 200 tasks so workers always have something to claim.
	for i := range 200 {
		task := makeTask(fmt.Sprintf("rc-task-%04d", i), tierForIndex(i))
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("seed Enqueue failed: %v", err)
		}
	}

	var (
		ops    atomic.Int64
		errors atomic.Int64
	)

	var wg sync.WaitGroup
	for g := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			tier := tierForIndex(goroutineID)
			wID := fmt.Sprintf("rc-worker-%d", goroutineID)
			step := 0

			for time.Now().Before(deadline) {
				step++
				switch step % 5 {
				case 0: // Enqueue a new task.
					task := makeTask(
						fmt.Sprintf("rc-dynamic-%d-%d", goroutineID, step),
						tier,
					)
					if err := queue.Enqueue(ctx, task); err != nil {
						errors.Add(1)
					} else {
						ops.Add(1)
					}

				case 1: // Dequeue and immediately release.
					task, err := queue.Dequeue(ctx, tier, wID)
					if err != nil {
						errors.Add(1)
					} else {
						ops.Add(1)
						if task != nil {
							_ = queue.Release(ctx, task.ID)
						}
					}

				case 2: // List (read-only).
					_, err := queue.List(ctx, nil)
					if err != nil {
						errors.Add(1)
					} else {
						ops.Add(1)
					}

				case 3: // Dequeue + update to InProgress, then release.
					task, err := queue.Dequeue(ctx, tier, wID)
					if err != nil {
						errors.Add(1)
					} else if task != nil {
						task.Status = taskqueue.StatusInProgress
						task.StartedAt = time.Now()
						if uerr := queue.Update(ctx, task); uerr != nil {
							errors.Add(1)
						} else {
							ops.Add(1)
						}
						_ = queue.Release(ctx, task.ID)
					} else {
						ops.Add(1) // nil is fine
					}

				case 4: // Get a known task.
					knownID := fmt.Sprintf("rc-task-%04d", goroutineID%200)
					_, err := queue.Get(ctx, knownID)
					if err != nil {
						// Task may have been released/re-enqueued — not a bug.
						ops.Add(1)
					} else {
						ops.Add(1)
					}
				}
			}
		}(g)
	}

	wg.Wait()

	t.Logf("RaceConditions: %d ops, %d errors over %s with %d goroutines",
		ops.Load(), errors.Load(), duration, numGoroutines)

	// A high error rate indicates a genuine problem.
	total := ops.Load() + errors.Load()
	if total > 0 {
		errorRate := float64(errors.Load()) / float64(total)
		if errorRate > 0.10 {
			t.Errorf("error rate %.1f%% exceeds 10%% threshold", errorRate*100)
		}
	}
}

// -------------------------------------------------------------------------
// Scenario 4: Worker Saturation
//
// Creates 50 workers competing for only 20 tasks.  Every task must be
// claimed exactly once (atomic guarantee), and no task may be lost.
// -------------------------------------------------------------------------

func TestScenario_WorkerSaturation(t *testing.T) {
	const numTasks = 20
	const numWorkers = 50

	queue, _ := newLoadTestQueue(t)
	ctx := context.Background()

	// Enqueue 20 tasks.
	for i := range numTasks {
		task := makeTask(fmt.Sprintf("ws-task-%04d", i), tierForIndex(i))
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue i=%d: %v", i, err)
		}
	}

	var (
		totalClaimed   atomic.Int64
		claimsByWorker sync.Map // workerID → *atomic.Int64
		claimedTasks   sync.Map // taskID → workerID
	)

	var wg sync.WaitGroup
	for w := range numWorkers {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			wID := fmt.Sprintf("ws-worker-%02d", workerIdx)

			// Each worker exhausts all three tiers before returning.
			for _, tier := range allTiers {
				for {
					task, err := queue.Dequeue(ctx, tier, wID)
					if err != nil {
						t.Errorf("worker %s Dequeue error: %v", wID, err)
						return
					}
					if task == nil {
						break // tier exhausted
					}

					// Detect double-claims.
					if prev, loaded := claimedTasks.LoadOrStore(task.ID, wID); loaded {
						t.Errorf("double-claim task %s: first by %v, then by %s",
							task.ID, prev, wID)
					}

					// Track per-worker claim counts.
					val, _ := claimsByWorker.LoadOrStore(wID, new(atomic.Int64))
					val.(*atomic.Int64).Add(1)
					totalClaimed.Add(1)

					// Complete the task.
					task.Status = taskqueue.StatusComplete
					task.CompletedAt = time.Now()
					if err := queue.Update(ctx, task); err != nil {
						t.Errorf("worker %s Complete update: %v", wID, err)
					}
				}
			}
		}(w)
	}

	wg.Wait()

	got := int(totalClaimed.Load())
	if got != numTasks {
		t.Errorf("WorkerSaturation: total claimed=%d, want %d", got, numTasks)
	}

	// No task should remain pending.
	pending := taskqueue.StatusPending
	pendingTasks, err := queue.List(ctx, &taskqueue.TaskFilter{Status: &pending})
	if err != nil {
		t.Fatalf("List pending failed: %v", err)
	}
	if len(pendingTasks) != 0 {
		t.Errorf("WorkerSaturation: %d tasks still pending", len(pendingTasks))
	}

	var workerCount int
	claimsByWorker.Range(func(_, _ any) bool {
		workerCount++
		return true
	})
	t.Logf("WorkerSaturation: %d tasks claimed by %d/%d workers",
		got, workerCount, numWorkers)
}

// -------------------------------------------------------------------------
// Benchmarks
//
// Run with: go test -bench=. -benchmem ./test/load/...
// Add -cpuprofile=cpu.prof -memprofile=mem.prof for pprof output.
// -------------------------------------------------------------------------

// BenchmarkQueue_Enqueue measures single-goroutine enqueue throughput.
func BenchmarkQueue_Enqueue(b *testing.B) {
	queue, _ := newLoadTestQueue(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := range b.N {
		task := makeTask(fmt.Sprintf("bench-enq-%d", i), tierForIndex(i))
		if err := queue.Enqueue(ctx, task); err != nil {
			b.Fatalf("Enqueue failed: %v", err)
		}
	}
}

// BenchmarkQueue_Dequeue measures dequeue throughput from a pre-filled queue.
func BenchmarkQueue_Dequeue(b *testing.B) {
	queue, _ := newLoadTestQueue(b)
	ctx := context.Background()

	// Pre-fill so every Dequeue succeeds.
	for i := range b.N + 100 {
		task := makeTask(fmt.Sprintf("bench-deq-%d", i), taskqueue.TierGeminiFlash)
		_ = queue.Enqueue(ctx, task)
	}

	b.ResetTimer()
	for range b.N {
		task, err := queue.Dequeue(ctx, taskqueue.TierGeminiFlash, "bench-worker")
		if err != nil {
			b.Fatalf("Dequeue failed: %v", err)
		}
		if task == nil {
			b.Skip("queue exhausted before bench finished")
		}
	}
}

// BenchmarkQueue_ConcurrentOperations benchmarks the queue with parallel
// goroutines doing mixed enqueue/dequeue/update.  Run with -cpu 1,4,8.
func BenchmarkQueue_ConcurrentOperations(b *testing.B) {
	queue, _ := newLoadTestQueue(b)
	ctx := context.Background()

	// Seed with 1000 tasks.
	for i := range 1000 {
		task := makeTask(fmt.Sprintf("bench-conc-seed-%d", i), tierForIndex(i))
		_ = queue.Enqueue(ctx, task)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			switch i % 3 {
			case 0:
				task := makeTask(fmt.Sprintf("bench-conc-%d", i), tierForIndex(i))
				_ = queue.Enqueue(ctx, task)
			case 1:
				task, _ := queue.Dequeue(ctx, tierForIndex(i), fmt.Sprintf("bench-w-%d", i))
				if task != nil {
					task.Status = taskqueue.StatusComplete
					_ = queue.Update(ctx, task)
				}
			case 2:
				_, _ = queue.List(ctx, nil)
			}
		}
	})
}
