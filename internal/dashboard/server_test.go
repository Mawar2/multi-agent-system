package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

// --- fakes ---

type fakeQueue struct {
	tasks []*taskqueue.Task
}

func (q *fakeQueue) Enqueue(_ context.Context, t *taskqueue.Task) error { return nil }
func (q *fakeQueue) Dequeue(_ context.Context, _ taskqueue.Tier, _ string) (*taskqueue.Task, error) {
	return nil, nil
}
func (q *fakeQueue) Update(_ context.Context, _ *taskqueue.Task) error { return nil }
func (q *fakeQueue) Get(_ context.Context, _ string) (*taskqueue.Task, error) {
	return nil, nil
}
func (q *fakeQueue) List(_ context.Context, _ *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	return q.tasks, nil
}
func (q *fakeQueue) Release(_ context.Context, _ string) error { return nil }

type fakeWorker struct {
	id     string
	tier   taskqueue.Tier
	health *worker.HealthStatus
}

func (w *fakeWorker) ID() string            { return w.id }
func (w *fakeWorker) Tier() taskqueue.Tier  { return w.tier }
func (w *fakeWorker) Claim(_ context.Context) (*taskqueue.Task, error) { return nil, nil }
func (w *fakeWorker) Execute(_ context.Context, _ *taskqueue.Task) (*worker.Result, error) {
	return nil, nil
}
func (w *fakeWorker) Release(_ context.Context, _ string) error { return nil }
func (w *fakeWorker) Health(_ context.Context) (*worker.HealthStatus, error) {
	return w.health, nil
}

type fakeWorkerProvider struct {
	workers []worker.Worker
}

func (p *fakeWorkerProvider) Workers() []worker.Worker { return p.workers }

// --- helpers ---

func newTestServer(q taskqueue.TaskQueue, wp WorkerProvider) *Server {
	return NewServer(":0", q, wp)
}

func doGet(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, req)
	return rec
}

func doPost(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, req)
	return rec
}

// --- tests ---

func TestHandleHealth(t *testing.T) {
	s := newTestServer(&fakeQueue{}, nil)
	rec := doGet(t, s, "/api/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var h HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("expected status=ok, got %q", h.Status)
	}
	if h.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestHandleStatusEmpty(t *testing.T) {
	s := newTestServer(&fakeQueue{}, &fakeWorkerProvider{})
	rec := doGet(t, s, "/api/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var sr StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &sr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sr.Queue.Total != 0 {
		t.Errorf("expected total=0, got %d", sr.Queue.Total)
	}
	if sr.Stats.SuccessRate != 0 {
		t.Errorf("expected success_rate=0, got %f", sr.Stats.SuccessRate)
	}
	if sr.Workers == nil {
		t.Error("workers should not be nil")
	}
}

func TestHandleStatusWithTasks(t *testing.T) {
	tasks := []*taskqueue.Task{
		{ID: "1", Status: taskqueue.StatusComplete},
		{ID: "2", Status: taskqueue.StatusComplete},
		{ID: "3", Status: taskqueue.StatusFailed, ErrorMsg: "quality gates failed: tests failed"},
		{ID: "4", Status: taskqueue.StatusPending},
		{ID: "5", Status: taskqueue.StatusInProgress},
	}
	s := newTestServer(&fakeQueue{tasks: tasks}, &fakeWorkerProvider{
		workers: []worker.Worker{
			&fakeWorker{
				id:   "claude-1",
				tier: taskqueue.TierClaude,
				health: &worker.HealthStatus{
					WorkerID:       "claude-1",
					Tier:           taskqueue.TierClaude,
					Healthy:        true,
					TasksCompleted: 2,
					TasksFailed:    1,
					LastHeartbeat:  time.Now().Format(time.RFC3339),
				},
			},
		},
	})
	rec := doGet(t, s, "/api/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var sr StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &sr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sr.Queue.Total != 5 {
		t.Errorf("expected total=5, got %d", sr.Queue.Total)
	}
	if sr.Queue.Complete != 2 {
		t.Errorf("expected complete=2, got %d", sr.Queue.Complete)
	}
	if sr.Queue.Failed != 1 {
		t.Errorf("expected failed=1, got %d", sr.Queue.Failed)
	}
	// success rate = 2/(2+1) * 100 ≈ 66.67
	if sr.Stats.SuccessRate < 66 || sr.Stats.SuccessRate > 67 {
		t.Errorf("expected success_rate ~66.67, got %f", sr.Stats.SuccessRate)
	}
	if sr.Stats.QualityGateFailures != 1 {
		t.Errorf("expected quality_gate_failures=1, got %d", sr.Stats.QualityGateFailures)
	}
	if len(sr.Workers) != 1 || !sr.Workers[0].Healthy {
		t.Errorf("expected 1 healthy worker")
	}
}

func TestHandleTasksEmpty(t *testing.T) {
	s := newTestServer(&fakeQueue{}, nil)
	rec := doGet(t, s, "/api/tasks")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Must be [] not null
	if body[0] != '[' {
		t.Errorf("expected JSON array, got: %s", body)
	}
}

func TestHandleTasksReturnsTasks(t *testing.T) {
	now := time.Now()
	tasks := []*taskqueue.Task{
		{
			ID:          "abc",
			IssueNumber: 42,
			RepoOwner:   "Mawar2",
			RepoName:    "Kaimi",
			Title:       "Fix the thing",
			Status:      taskqueue.StatusComplete,
			Tier:        taskqueue.TierClaude,
			Complexity:  taskqueue.ComplexityMedium,
			WorkerID:    "claude-1",
			PRNumber:    7,
			Attempts:    1,
			CompletedAt: now,
		},
	}
	s := newTestServer(&fakeQueue{tasks: tasks}, nil)
	rec := doGet(t, s, "/api/tasks")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result []TaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	tr := result[0]
	if tr.IssueNumber != 42 {
		t.Errorf("expected issue_number=42, got %d", tr.IssueNumber)
	}
	if tr.Status != "complete" {
		t.Errorf("expected status=complete, got %q", tr.Status)
	}
	if tr.PRNumber != 7 {
		t.Errorf("expected pr_number=7, got %d", tr.PRNumber)
	}
}

func TestHandleIndex(t *testing.T) {
	s := newTestServer(nil, nil)
	rec := doGet(t, s, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct == "" || len(ct) < 9 || ct[:9] != "text/html" {
		t.Errorf("expected text/html content-type, got %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty HTML body")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s := newTestServer(&fakeQueue{}, nil)
	for _, path := range []string{"/api/health", "/api/status", "/api/tasks"} {
		rec := doPost(t, s, path)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: expected 405, got %d", path, rec.Code)
		}
	}
}

func TestCORSHeader(t *testing.T) {
	s := newTestServer(&fakeQueue{}, nil)
	rec := doGet(t, s, "/api/health")
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("expected CORS header '*', got %q", origin)
	}
}
