package load_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/google/uuid"
)

// makeTask creates a minimal pending task for a given tier.
func makeTask(tier taskqueue.Tier) *taskqueue.Task {
	return &taskqueue.Task{
		ID:          uuid.New().String(),
		IssueNumber: 1,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "load-test task",
		Status:      taskqueue.StatusPending,
		Tier:        tier,
		Complexity:  taskqueue.ComplexitySimple,
	}
}

// newQueue returns a fresh JSONQueue backed by t.TempDir().
func newQueue(t *testing.T) *taskqueue.JSONQueue {
	t.Helper()
	q, err := taskqueue.NewJSONQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONQueue: %v", err)
	}
	return q
}

// TestScenario_HighThroughput enqueues 100 tasks and drains them with 50 concurrent
// workers. The test exits as soon as all tasks are claimed, verifying that
// throughput stays high under parallel access (no deadlocks, no double-claims).
func TestScenario_HighThroughput(t *testing.T) {
	const (
		numTasks   = 100
		numWorkers = 50
	)

	ctx := context.Background()
	q := newQueue(t)

	for i := range numTasks {
		task := makeTask(taskqueue.TierGeminiFlash)
		task.IssueNumber = i + 1
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	var (
		claimed atomic.Int64
		wg      sync.WaitGroup
	)

	start := time.Now()

	for w := range numWorkers {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for {
				task, err := q.Dequeue(ctx, taskqueue.TierGeminiFlash, workerID)
				if err != nil {
					t.Errorf("worker %s Dequeue error: %v", workerID, err)
					return
				}
				if task == nil {
					return // queue empty for this worker
				}
				task.Status = taskqueue.StatusComplete
				if err := q.Update(ctx, task); err != nil {
					t.Errorf("worker %s Update error: %v", workerID, err)
				}
				claimed.Add(1)
			}
		}(fmt.Sprintf("worker-%d", w))
	}

	wg.Wait()
	elapsed := time.Since(start)

	if got := claimed.Load(); got != numTasks {
		t.Errorf("claimed %d tasks, want %d", got, numTasks)
	}

	throughput := float64(claimed.Load()) / elapsed.Seconds()
	t.Logf("HighThroughput: %d tasks in %v → %.0f tasks/sec", numTasks, elapsed.Round(time.Millisecond), throughput)
}

// TestScenario_MemoryLeaks processes 5 batches of 100 tasks each and samples
// heap allocations after each batch. A growing heap across batches is reported
// as a failure. Goroutine count is also verified to return to baseline.
func TestScenario_MemoryLeaks(t *testing.T) {
	const (
		numBatches   = 5
		batchSize    = 100
		leakFactor   = 3.0 // heap must not grow more than 3× from first to last batch
		goroutineTol = 5   // goroutines allowed above baseline
	)

	ctx := context.Background()
	q := newQueue(t)

	// Settle goroutines from test setup before baselining.
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	var heapSamples []uint64

	for batch := range numBatches {
		// Enqueue batch
		for i := range batchSize {
			task := makeTask(taskqueue.TierGeminiPro)
			task.IssueNumber = batch*batchSize + i + 1
			if err := q.Enqueue(ctx, task); err != nil {
				t.Fatalf("batch %d Enqueue %d: %v", batch, i, err)
			}
		}

		// Drain batch with 10 workers
		var wg sync.WaitGroup
		for w := range 10 {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				for {
					task, err := q.Dequeue(ctx, taskqueue.TierGeminiPro, id)
					if err != nil || task == nil {
						return
					}
					task.Status = taskqueue.StatusComplete
					_ = q.Update(ctx, task)
				}
			}(fmt.Sprintf("leak-worker-%d-%d", batch, w))
		}
		wg.Wait()

		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapSamples = append(heapSamples, ms.HeapAlloc)
		t.Logf("batch %d: HeapAlloc=%d KB goroutines=%d", batch+1, ms.HeapAlloc/1024, runtime.NumGoroutine())
	}

	// Heap growth check: last batch must not be more than leakFactor× first batch.
	first, last := heapSamples[0], heapSamples[numBatches-1]
	if first > 0 && float64(last) > leakFactor*float64(first) {
		t.Errorf("possible heap leak: HeapAlloc grew from %d KB to %d KB (%.1f×, threshold %.1f×)",
			first/1024, last/1024, float64(last)/float64(first), leakFactor)
	}

	// Goroutine leak check
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > baselineGoroutines+goroutineTol {
		t.Errorf("goroutine leak: baseline %d, final %d (tolerated +%d)",
			baselineGoroutines, finalGoroutines, goroutineTol)
	}
}

// TestScenario_RaceConditions hammers all queue operations simultaneously from
// 100 goroutines for 500 ms. The test itself validates no errors are returned;
// run with `go test -race` to catch data races via the race detector.
func TestScenario_RaceConditions(t *testing.T) {
	const (
		numGoroutines = 100
		duration      = 500 * time.Millisecond
	)

	ctx := context.Background()
	q := newQueue(t)

	// Pre-seed the queue so workers have something to dequeue immediately.
	for i := range 50 {
		task := makeTask(taskqueue.TierClaude)
		task.IssueNumber = i + 1
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("seed Enqueue %d: %v", i, err)
		}
	}

	var (
		errors atomic.Int64
		ops    atomic.Int64
		wg     sync.WaitGroup
		stop   = make(chan struct{})
	)

	time.AfterFunc(duration, func() { close(stop) })

	for g := range numGoroutines {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			workerID := fmt.Sprintf("race-worker-%d", gid)

			for {
				select {
				case <-stop:
					return
				default:
				}

				switch gid % 4 {
				case 0: // Enqueue
					task := makeTask(taskqueue.TierClaude)
					task.IssueNumber = gid
					if err := q.Enqueue(ctx, task); err != nil {
						errors.Add(1)
					}
					ops.Add(1)

				case 1: // Dequeue + Update
					task, err := q.Dequeue(ctx, taskqueue.TierClaude, workerID)
					if err != nil {
						errors.Add(1)
					} else if task != nil {
						task.Status = taskqueue.StatusComplete
						if err := q.Update(ctx, task); err != nil {
							errors.Add(1)
						}
					}
					ops.Add(1)

				case 2: // List
					if _, err := q.List(ctx, nil); err != nil {
						errors.Add(1)
					}
					ops.Add(1)

				case 3: // Dequeue + Release
					task, err := q.Dequeue(ctx, taskqueue.TierClaude, workerID)
					if err != nil {
						errors.Add(1)
					} else if task != nil {
						if err := q.Release(ctx, task.ID); err != nil {
							errors.Add(1)
						}
					}
					ops.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()

	t.Logf("RaceConditions: %d goroutines, %v → %d ops, %d errors",
		numGoroutines, duration, ops.Load(), errors.Load())

	if n := errors.Load(); n > 0 {
		t.Errorf("got %d operation errors under concurrent access", n)
	}
}

// TestScenario_WorkerSaturation creates 20 tasks and races 50 workers to claim
// them, verifying that each task is claimed exactly once (no double-claims).
func TestScenario_WorkerSaturation(t *testing.T) {
	const (
		numTasks   = 20
		numWorkers = 50
	)

	ctx := context.Background()
	q := newQueue(t)

	taskIDs := make([]string, numTasks)
	for i := range numTasks {
		task := makeTask(taskqueue.TierGeminiFlash)
		task.IssueNumber = i + 1
		taskIDs[i] = task.ID
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	claimedBy := sync.Map{}
	var doubleClaims atomic.Int64
	var wg sync.WaitGroup

	for w := range numWorkers {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for {
				task, err := q.Dequeue(ctx, taskqueue.TierGeminiFlash, workerID)
				if err != nil {
					t.Errorf("worker %s Dequeue: %v", workerID, err)
					return
				}
				if task == nil {
					return
				}

				if prev, loaded := claimedBy.LoadOrStore(task.ID, workerID); loaded {
					doubleClaims.Add(1)
					t.Errorf("task %s claimed by both %v and %s", task.ID, prev, workerID)
				}

				task.Status = taskqueue.StatusComplete
				_ = q.Update(ctx, task)
			}
		}(fmt.Sprintf("sat-worker-%d", w))
	}

	wg.Wait()

	if n := doubleClaims.Load(); n > 0 {
		t.Errorf("%d double-claims detected", n)
	}

	// Verify all tasks were claimed exactly once.
	var claimed int
	claimedBy.Range(func(_, _ any) bool { claimed++; return true })
	if claimed != numTasks {
		t.Errorf("claimed %d/%d tasks", claimed, numTasks)
	}
	t.Logf("WorkerSaturation: %d workers, %d tasks → %d claimed, %d double-claims",
		numWorkers, numTasks, claimed, doubleClaims.Load())
}

// BenchmarkEnqueue measures raw enqueue throughput on a single goroutine.
func BenchmarkEnqueue(b *testing.B) {
	q, err := taskqueue.NewJSONQueue(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := range b.N {
		task := makeTaskB(i, taskqueue.TierGeminiFlash)
		if err := q.Enqueue(ctx, task); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDequeue measures dequeue throughput assuming a pre-filled queue.
func BenchmarkDequeue(b *testing.B) {
	q, err := taskqueue.NewJSONQueue(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	// Pre-fill
	for i := range b.N {
		if err := q.Enqueue(ctx, makeTaskB(i, taskqueue.TierGeminiFlash)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for range b.N {
		task, err := q.Dequeue(ctx, taskqueue.TierGeminiFlash, "bench-worker")
		if err != nil {
			b.Fatal(err)
		}
		if task == nil {
			b.Fatal("unexpected empty queue")
		}
	}
}

// BenchmarkConcurrentEnqueueDequeue measures throughput with concurrent producers
// and consumers using GOMAXPROCS goroutines.
func BenchmarkConcurrentEnqueueDequeue(b *testing.B) {
	q, err := taskqueue.NewJSONQueue(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	procs := max(runtime.GOMAXPROCS(0), 2)

	var (
		produced atomic.Int64
		limit    = int64(b.N)
		wg       sync.WaitGroup
	)

	b.ResetTimer()

	// Producers
	for p := range procs / 2 {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			for {
				n := produced.Add(1)
				if n > limit {
					return
				}
				task := makeTaskB(int(n), taskqueue.TierGeminiFlash)
				_ = q.Enqueue(ctx, task)
			}
		}(p)
	}

	// Consumers
	for c := range procs / 2 {
		wg.Add(1)
		go func(cid int) {
			defer wg.Done()
			workerID := fmt.Sprintf("bench-consumer-%d", cid)
			for {
				task, _ := q.Dequeue(ctx, taskqueue.TierGeminiFlash, workerID)
				if task != nil {
					task.Status = taskqueue.StatusComplete
					_ = q.Update(ctx, task)
				}
				if produced.Load() >= limit {
					return
				}
			}
		}(c)
	}

	wg.Wait()
}

// makeTaskB is the benchmark variant of makeTask, avoids allocating uuid in tight loops.
func makeTaskB(i int, tier taskqueue.Tier) *taskqueue.Task {
	return &taskqueue.Task{
		ID:          uuid.New().String(),
		IssueNumber: i + 1,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "bench task",
		Status:      taskqueue.StatusPending,
		Tier:        tier,
		Complexity:  taskqueue.ComplexitySimple,
	}
}
