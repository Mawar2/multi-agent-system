package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

// fakeQueue is a test double for taskqueue.TaskQueue.
type fakeQueue struct {
	tasks []*taskqueue.Task
}

func (q *fakeQueue) Enqueue(_ context.Context, _ *taskqueue.Task) error { return nil }
func (q *fakeQueue) Dequeue(_ context.Context, _ taskqueue.Tier, _ string) (*taskqueue.Task, error) {
	return nil, nil
}
func (q *fakeQueue) Update(_ context.Context, _ *taskqueue.Task) error        { return nil }
func (q *fakeQueue) Get(_ context.Context, _ string) (*taskqueue.Task, error) { return nil, nil }
func (q *fakeQueue) Release(_ context.Context, _ string) error                { return nil }
func (q *fakeQueue) List(_ context.Context, _ *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	if q.tasks == nil {
		return []*taskqueue.Task{}, nil
	}
	return q.tasks, nil
}

// fakeWorker is a test double for worker.Worker.
type fakeWorker struct {
	id        string
	tier      taskqueue.Tier
	healthy   bool
	completed int
	failed    int
}

func (w *fakeWorker) Claim(_ context.Context) (*taskqueue.Task, error) { return nil, nil }
func (w *fakeWorker) Execute(_ context.Context, _ *taskqueue.Task) (*worker.Result, error) {
	return nil, nil
}
func (w *fakeWorker) Release(_ context.Context, _ string) error { return nil }
func (w *fakeWorker) Health(_ context.Context) (*worker.HealthStatus, error) {
	return &worker.HealthStatus{
		WorkerID:       w.id,
		Tier:           w.tier,
		Healthy:        w.healthy,
		TasksCompleted: w.completed,
		TasksFailed:    w.failed,
		LastHeartbeat:  time.Now().Format(time.RFC3339),
	}, nil
}
func (w *fakeWorker) ID() string           { return w.id }
func (w *fakeWorker) Tier() taskqueue.Tier { return w.tier }

// TestHandleHealth verifies GET /api/health returns {"status":"ok"}.
func TestHandleHealth(t *testing.T) {
	s := NewServer(&fakeQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rec.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("want status=ok, got %q", resp["status"])
	}
}

// TestHandleStatusEmpty verifies /api/status returns a valid zero-count structure.
func TestHandleStatusEmpty(t *testing.T) {
	s := NewServer(&fakeQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rec.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Workers == nil {
		t.Error("workers must not be nil")
	}
	if resp.Queue.Total != 0 {
		t.Errorf("want queue total 0, got %d", resp.Queue.Total)
	}
	if resp.System.SuccessRate != 0 {
		t.Errorf("want success_rate 0, got %f", resp.System.SuccessRate)
	}
}

// TestHandleStatusWithTasks verifies aggregation: success rate, queue counts, quality gate failures.
func TestHandleStatusWithTasks(t *testing.T) {
	tasks := []*taskqueue.Task{
		{ID: "1", Status: taskqueue.StatusComplete},
		{ID: "2", Status: taskqueue.StatusComplete},
		{ID: "3", Status: taskqueue.StatusFailed, ErrorMsg: "quality gates failed: tests failed"},
		{ID: "4", Status: taskqueue.StatusFailed, ErrorMsg: "some other error"},
		{ID: "5", Status: taskqueue.StatusPending},
	}
	workers := []worker.Worker{
		&fakeWorker{id: "claude-1", tier: taskqueue.TierClaude, healthy: true, completed: 2, failed: 1},
	}
	s := NewServer(&fakeQueue{tasks: tasks}, workers)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rec.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp.Queue.Complete != 2 {
		t.Errorf("want complete=2, got %d", resp.Queue.Complete)
	}
	if resp.Queue.Failed != 2 {
		t.Errorf("want failed=2, got %d", resp.Queue.Failed)
	}
	if resp.Queue.Pending != 1 {
		t.Errorf("want pending=1, got %d", resp.Queue.Pending)
	}
	if resp.Queue.Total != 5 {
		t.Errorf("want total=5, got %d", resp.Queue.Total)
	}
	// 2 complete / (2 complete + 2 failed) = 0.5
	if resp.System.SuccessRate != 0.5 {
		t.Errorf("want success_rate=0.5, got %f", resp.System.SuccessRate)
	}
	if resp.System.QualityGateFailures != 1 {
		t.Errorf("want quality_gate_failures=1, got %d", resp.System.QualityGateFailures)
	}
	if resp.System.EstimatedSavingsUSD != 0.10 {
		t.Errorf("want estimated_savings=0.10, got %f", resp.System.EstimatedSavingsUSD)
	}
	if len(resp.Workers) != 1 {
		t.Fatalf("want 1 worker, got %d", len(resp.Workers))
	}
	if resp.Workers[0].ID != "claude-1" {
		t.Errorf("want worker id=claude-1, got %s", resp.Workers[0].ID)
	}
	if !resp.Workers[0].Healthy {
		t.Error("want worker healthy=true")
	}
}

// TestHandleTasksEmpty verifies /api/tasks returns [] not null when the queue is empty.
func TestHandleTasksEmpty(t *testing.T) {
	s := NewServer(&fakeQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("want '[]', got %q", rec.Body.String())
	}
}

// TestHandleTasksReturnsTasks verifies tasks from the queue appear in the response.
func TestHandleTasksReturnsTasks(t *testing.T) {
	tasks := []*taskqueue.Task{
		{ID: "task-1", IssueNumber: 1, Status: taskqueue.StatusComplete},
		{ID: "task-2", IssueNumber: 2, Status: taskqueue.StatusPending},
	}
	s := NewServer(&fakeQueue{tasks: tasks}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rec.Code)
	}
	var result []*taskqueue.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("want 2 tasks, got %d", len(result))
	}
}

// TestHandleIndex verifies GET / serves an HTML page.
func TestHandleIndex(t *testing.T) {
	s := NewServer(&fakeQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("want Content-Type text/html, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Error("response body should contain an <html> element")
	}
}

// TestMethodNotAllowed verifies POST to API routes returns 405.
func TestMethodNotAllowed(t *testing.T) {
	s := NewServer(&fakeQueue{}, nil)
	routes := []string{"/api/health", "/api/status", "/api/tasks"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, route, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: want 405, got %d", route, rec.Code)
			}
		})
	}
}

// TestCORSHeader verifies Access-Control-Allow-Origin: * is present on responses.
func TestCORSHeader(t *testing.T) {
	s := NewServer(&fakeQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if cors := rec.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("want Access-Control-Allow-Origin=*, got %q", cors)
	}
}
