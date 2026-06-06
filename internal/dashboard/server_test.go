package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

// --- mock queue ---

type mockQueue struct {
	tasks []*taskqueue.Task
}

func (m *mockQueue) Enqueue(_ context.Context, task *taskqueue.Task) error { return nil }
func (m *mockQueue) Dequeue(_ context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error) {
	return nil, nil
}
func (m *mockQueue) Update(_ context.Context, task *taskqueue.Task) error { return nil }
func (m *mockQueue) Get(_ context.Context, taskID string) (*taskqueue.Task, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockQueue) List(_ context.Context, _ *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	return m.tasks, nil
}
func (m *mockQueue) Release(_ context.Context, _ string) error { return nil }

// --- mock worker ---

type mockWorker struct {
	id     string
	tier   taskqueue.Tier
	health *worker.HealthStatus
}

func (w *mockWorker) Claim(_ context.Context) (*taskqueue.Task, error) { return nil, nil }
func (w *mockWorker) Execute(_ context.Context, _ *taskqueue.Task) (*worker.Result, error) {
	return nil, nil
}
func (w *mockWorker) Release(_ context.Context, _ string) error { return nil }
func (w *mockWorker) Health(_ context.Context) (*worker.HealthStatus, error) {
	return w.health, nil
}
func (w *mockWorker) ID() string           { return w.id }
func (w *mockWorker) Tier() taskqueue.Tier { return w.tier }

// --- helpers ---

func newTestServer(q taskqueue.TaskQueue, workers []worker.Worker) *Server {
	return New(":0", q, workers)
}

func makeRequest(s *Server, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/", s.handleIndex)
	mux.ServeHTTP(rr, req)
	return rr
}

// --- tests ---

func TestHandleHealth(t *testing.T) {
	s := newTestServer(&mockQueue{}, nil)
	rr := makeRequest(s, http.MethodGet, "/api/health")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want JSON content-type, got %q", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("want status=ok, got %q", body["status"])
	}
}

func TestHandleStatusEmpty(t *testing.T) {
	s := newTestServer(&mockQueue{}, nil)
	rr := makeRequest(s, http.MethodGet, "/api/status")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	if resp.Workers == nil {
		t.Error("workers should not be nil")
	}
	if resp.Stats.TotalTasks != 0 {
		t.Errorf("want 0 total tasks, got %d", resp.Stats.TotalTasks)
	}
}

func TestHandleStatusWithTasks(t *testing.T) {
	q := &mockQueue{tasks: []*taskqueue.Task{
		{ID: "t1", IssueNumber: 1, Title: "Fix bug", Status: taskqueue.StatusComplete,
			Tier: taskqueue.TierGeminiFlash},
		{ID: "t2", IssueNumber: 2, Title: "Add feature", Status: taskqueue.StatusFailed,
			ErrorMsg: "quality gates failed: tests failed", Tier: taskqueue.TierGeminiPro},
		{ID: "t3", IssueNumber: 3, Title: "Refactor", Status: taskqueue.StatusPending,
			Tier: taskqueue.TierClaude},
		{ID: "t4", IssueNumber: 4, Title: "Review", Status: taskqueue.StatusInProgress,
			Tier: taskqueue.TierGeminiFlash},
	}}

	workers := []worker.Worker{
		&mockWorker{
			id:   "gemini-flash-1",
			tier: taskqueue.TierGeminiFlash,
			health: &worker.HealthStatus{
				WorkerID:       "gemini-flash-1",
				Tier:           taskqueue.TierGeminiFlash,
				Healthy:        true,
				TasksCompleted: 3,
				TasksFailed:    1,
				LastHeartbeat:  "2026-06-06T00:00:00Z",
			},
		},
	}

	s := newTestServer(q, workers)
	rr := makeRequest(s, http.MethodGet, "/api/status")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp.Stats.TotalTasks != 4 {
		t.Errorf("want 4 total tasks, got %d", resp.Stats.TotalTasks)
	}

	// 1 complete / (1 complete + 1 failed) = 50%
	if resp.Stats.SuccessRate < 49.9 || resp.Stats.SuccessRate > 50.1 {
		t.Errorf("want ~50%% success rate, got %.1f", resp.Stats.SuccessRate)
	}

	if resp.Stats.QualityGateFailures != 1 {
		t.Errorf("want 1 quality gate failure, got %d", resp.Stats.QualityGateFailures)
	}

	if resp.Queue.Pending != 1 {
		t.Errorf("want queue.pending=1, got %d", resp.Queue.Pending)
	}
	if resp.Queue.InProgress != 1 {
		t.Errorf("want queue.in_progress=1, got %d", resp.Queue.InProgress)
	}
	if resp.Queue.Depth != 2 {
		t.Errorf("want queue.depth=2, got %d", resp.Queue.Depth)
	}

	if len(resp.Workers) != 1 {
		t.Fatalf("want 1 worker, got %d", len(resp.Workers))
	}
	if resp.Workers[0].ID != "gemini-flash-1" {
		t.Errorf("unexpected worker id: %q", resp.Workers[0].ID)
	}
	if !resp.Workers[0].Healthy {
		t.Error("worker should be healthy")
	}
	if resp.Workers[0].TasksCompleted != 3 {
		t.Errorf("want tasks_completed=3, got %d", resp.Workers[0].TasksCompleted)
	}
}

func TestHandleTasksEmpty(t *testing.T) {
	s := newTestServer(&mockQueue{}, nil)
	rr := makeRequest(s, http.MethodGet, "/api/tasks")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var tasks []*taskqueue.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if tasks == nil {
		t.Error("tasks should decode to empty slice, not nil")
	}
	if len(tasks) != 0 {
		t.Errorf("want 0 tasks, got %d", len(tasks))
	}
}

func TestHandleTasksReturnsTasks(t *testing.T) {
	q := &mockQueue{tasks: []*taskqueue.Task{
		{ID: "abc", IssueNumber: 7, Title: "Do stuff", Status: taskqueue.StatusPending},
	}}
	s := newTestServer(q, nil)
	rr := makeRequest(s, http.MethodGet, "/api/tasks")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var tasks []*taskqueue.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "abc" {
		t.Errorf("unexpected task id: %q", tasks[0].ID)
	}
}

func TestHandleIndex(t *testing.T) {
	s := newTestServer(&mockQueue{}, nil)
	rr := makeRequest(s, http.MethodGet, "/")

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("want text/html content-type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Error("response body does not look like HTML")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s := newTestServer(&mockQueue{}, nil)
	for _, path := range []string{"/api/status", "/api/tasks", "/api/health"} {
		rr := makeRequest(s, http.MethodPost, path)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s POST: want 405, got %d", path, rr.Code)
		}
	}
}

func TestCORSHeader(t *testing.T) {
	s := newTestServer(&mockQueue{}, nil)
	rr := makeRequest(s, http.MethodGet, "/api/status")
	if v := rr.Header().Get("Access-Control-Allow-Origin"); v != "*" {
		t.Errorf("want CORS header *, got %q", v)
	}
}
