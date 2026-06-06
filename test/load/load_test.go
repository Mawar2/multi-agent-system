// Package load implements stress-test scenarios for the taskqueue package.
//
// Run scenarios:
//
//	go test ./test/load/... -v
//
// Run benchmarks with pprof:
//
//	go test -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof ./test/load/...
//	go tool pprof cpu.prof
package load

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// newTestTask returns a minimal Task ready to be enqueued.
func newTestTask(id string, tier taskqueue.Tier) *taskqueue.Task {
	return &taskqueue.Task{
		ID:        id,
		RepoOwner: "Mawar2",
		RepoName:  "Kaimi",
		Title:     "Load test task " + id,
		Status:    taskqueue.StatusPending,
		Tier:      tier,
	}
}

func makeQueue(t *testing.T) *taskqueue.JSONQueue {
	t.Helper()
	q, err := taskqueue.NewJSONQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONQueue: %v", err)
	}
	return q
}

func makeQueueB(b *testing.B) *taskqueue.JSONQueue {
	b.Helper()
	q, err := taskqueue.NewJSONQueue(b.TempDir())
	if err != nil {
		b.Fatalf("NewJSONQueue: %v", err)
	}
	return q
}

// TestScenario_HighThroughput enqueues 100 tasks and drains them with 50
// concurrent workers, reporting throughput (tasks/sec).
func TestScenario_HighThroughput(t *testing.T) {
	const (
		numTasks   = 100
		numWorkers = 50
	)
	ctx := context.Background()
	q := makeQueue(t)

	for i := 0; i < numTasks; i++ {
		id := fmt.Sprintf("ht-%04d", i)
		if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiFlash)); err != nil {
			t.Fatalf("Enqueue %s: %v", id, err)
		}
	}

	var claimed int64
	start := time.Now()
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := fmt.Sprintf("ht-worker-%d", w)
		go func() {
			defer wg.Done()
			for {
				task, err := q.Dequeue(ctx, taskqueue.TierGeminiFlash, workerID)
				if err != nil {
					t.Errorf("Dequeue: %v", err)
					return
				}
				if task == nil {
					return
				}
				atomic.AddInt64(&claimed, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	total := atomic.LoadInt64(&claimed)

	if total != numTasks {
		t.Errorf("claimed %d tasks, want %d", total, numTasks)
	}
	t.Logf("HighThroughput: %d tasks in %s (%.0f tasks/sec)",
		total, elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds())
}

// TestScenario_MemoryLeaks processes 5 batches of 100 tasks and checks that
// heap allocation and goroutine count remain stable across batches.
func TestScenario_MemoryLeaks(t *testing.T) {
	const (
		numBatches    = 5
		tasksPerBatch = 100
	)
	ctx := context.Background()
	q := makeQueue(t)

	runtime.GC()
	baseGoroutines := runtime.NumGoroutine()
	heapAllocs := make([]uint64, numBatches)

	for batch := 0; batch < numBatches; batch++ {
		for i := 0; i < tasksPerBatch; i++ {
			id := fmt.Sprintf("ml-%d-%04d", batch, i)
			if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiPro)); err != nil {
				t.Fatalf("batch %d Enqueue: %v", batch, err)
			}
		}

		// Drain the batch with a single worker.
		for {
			task, err := q.Dequeue(ctx, taskqueue.TierGeminiPro, "ml-worker")
			if err != nil {
				t.Fatalf("batch %d Dequeue: %v", batch, err)
			}
			if task == nil {
				break
			}
		}

		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapAllocs[batch] = ms.HeapAlloc
	}

	finalGoroutines := runtime.NumGoroutine()
	if leaked := finalGoroutines - baseGoroutines; leaked > 2 {
		t.Errorf("goroutine leak: base=%d final=%d (+%d leaked)",
			baseGoroutines, finalGoroutines, leaked)
	}

	first, last := heapAllocs[0], heapAllocs[numBatches-1]
	t.Logf("MemoryLeaks: heap batch1=%d KB batch%d=%d KB; goroutines base=%d final=%d",
		first/1024, numBatches, last/1024, baseGoroutines, finalGoroutines)

	// Heap must not grow more than 4× across batches (directory scan overhead
	// grows linearly with retained claimed-task files, so allow generous slack).
	if last > first*4+2*1024*1024 {
		t.Errorf("heap grew too much: %d KB → %d KB", first/1024, last/1024)
	}
}

// TestScenario_RaceConditions runs 100 goroutines for 500 ms, each performing
// mixed enqueue / dequeue / list / dequeue+release operations. Expects 0 errors.
func TestScenario_RaceConditions(t *testing.T) {
	const (
		numGoroutines = 100
		duration      = 500 * time.Millisecond
	)
	ctx := context.Background()
	q := makeQueue(t)

	// Seed the queue so dequeue goroutines have work from the start.
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("rc-init-%04d", i)
		if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierClaude)); err != nil {
			t.Fatalf("setup Enqueue: %v", err)
		}
	}

	var errCount int64
	var opCounter int64
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workerID := fmt.Sprintf("rc-worker-%d", id)
			for time.Now().Before(deadline) {
				n := atomic.AddInt64(&opCounter, 1)
				switch n % 4 {
				case 0: // enqueue a fresh task
					tid := fmt.Sprintf("rc-dyn-%d", n)
					if err := q.Enqueue(ctx, newTestTask(tid, taskqueue.TierClaude)); err != nil {
						atomic.AddInt64(&errCount, 1)
					}
				case 1: // dequeue (no release — simulates a worker consuming a task)
					if _, err := q.Dequeue(ctx, taskqueue.TierClaude, workerID); err != nil {
						atomic.AddInt64(&errCount, 1)
					}
				case 2: // list (read-only)
					if _, err := q.List(ctx, nil); err != nil {
						atomic.AddInt64(&errCount, 1)
					}
				case 3: // dequeue then immediately release (simulates a worker crash)
					task, err := q.Dequeue(ctx, taskqueue.TierClaude, workerID)
					if err != nil {
						atomic.AddInt64(&errCount, 1)
					} else if task != nil {
						// Ignore release error: another goroutine may have already
						// re-claimed and completed the task between dequeue and release.
						_ = q.Release(ctx, task.ID)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	errors := atomic.LoadInt64(&errCount)
	ops := atomic.LoadInt64(&opCounter)
	t.Logf("RaceConditions: %d ops over %s with %d goroutines; errors=%d",
		ops, duration, numGoroutines, errors)
	if errors > 0 {
		t.Errorf("got %d errors during concurrent operations", errors)
	}
}

// TestScenario_WorkerSaturation races 50 workers against a 20-task queue and
// verifies each task is claimed exactly once (0 double-claims).
func TestScenario_WorkerSaturation(t *testing.T) {
	const (
		numWorkers = 50
		numTasks   = 20
	)
	ctx := context.Background()
	q := makeQueue(t)

	for i := 0; i < numTasks; i++ {
		id := fmt.Sprintf("sat-%04d", i)
		if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiFlash)); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	var claimed int64
	var doubles int64
	var claimedIDs sync.Map
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := fmt.Sprintf("sat-worker-%d", w)
		go func() {
			defer wg.Done()
			task, err := q.Dequeue(ctx, taskqueue.TierGeminiFlash, workerID)
			if err != nil {
				t.Errorf("Dequeue: %v", err)
				return
			}
			if task == nil {
				return
			}
			atomic.AddInt64(&claimed, 1)
			if _, alreadyClaimed := claimedIDs.LoadOrStore(task.ID, workerID); alreadyClaimed {
				atomic.AddInt64(&doubles, 1)
				t.Errorf("double-claim: task %s", task.ID)
			}
		}()
	}

	wg.Wait()
	total := atomic.LoadInt64(&claimed)
	d := atomic.LoadInt64(&doubles)
	t.Logf("WorkerSaturation: %d workers vs %d tasks; claimed=%d double-claims=%d",
		numWorkers, numTasks, total, d)
	if total != numTasks {
		t.Errorf("expected %d tasks claimed, got %d", numTasks, total)
	}
}

// BenchmarkEnqueue measures single-goroutine enqueue throughput.
func BenchmarkEnqueue(b *testing.B) {
	ctx := context.Background()
	q := makeQueueB(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-enq-%d", i)
		if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiFlash)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDequeue measures single-goroutine dequeue throughput from a
// pre-filled queue of b.N tasks.
func BenchmarkDequeue(b *testing.B) {
	ctx := context.Background()
	q := makeQueueB(b)

	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-deq-%d", i)
		if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiFlash)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task, err := q.Dequeue(ctx, taskqueue.TierGeminiFlash, "bench-worker")
		if err != nil {
			b.Fatal(err)
		}
		if task == nil {
			b.Fatal("unexpectedly ran out of tasks")
		}
	}
}

// BenchmarkConcurrentEnqueueDequeue measures throughput with GOMAXPROCS
// goroutines interleaving enqueue and dequeue operations.
// Attach pprof profiles via -cpuprofile and -memprofile flags.
func BenchmarkConcurrentEnqueueDequeue(b *testing.B) {
	ctx := context.Background()
	q := makeQueueB(b)

	// Seed so workers have tasks to dequeue from the first iteration.
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("bench-seed-%d", i)
		if err := q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiPro)); err != nil {
			b.Fatal(err)
		}
	}

	var workerSeq int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		wn := atomic.AddInt64(&workerSeq, 1)
		workerID := fmt.Sprintf("bench-concurrent-%d", wn)
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				id := fmt.Sprintf("bench-conc-%d-%d", wn, i)
				_ = q.Enqueue(ctx, newTestTask(id, taskqueue.TierGeminiPro))
			} else {
				_, _ = q.Dequeue(ctx, taskqueue.TierGeminiPro, workerID)
			}
			i++
		}
	})
}
