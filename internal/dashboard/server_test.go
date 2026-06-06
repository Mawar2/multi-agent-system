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

// mockQueue implements taskqueue.TaskQueue for testing.
type mockQueue struct {
	tasks []*taskqueue.Task
}

func (m *mockQueue) Enqueue(_ context.Context, _ *taskqueue.Task) error { return nil }
func (m *mockQueue) Dequeue(_ context.Context, _ taskqueue.Tier, _ string) (*taskqueue.Task, error) {
	return nil, nil
}
func (m *mockQueue) Update(_ context.Context, _ *taskqueue.Task) error { return nil }
func (m *mockQueue) Get(_ context.Context, _ string) (*taskqueue.Task, error) { return nil, nil }
func (m *mockQueue) Release(_ context.Context, _ string) error { return nil }
func (m *mockQueue) List(_ context.Context, _ *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	if m.tasks == nil {
		return []*taskqueue.Task{}, nil
	}
	return m.tasks, nil
}

// mockWorker implements worker.Worker for testing.
type mockWorker struct {
	health *worker.HealthStatus
}

func (w *mockWorker) Claim(_ context.Context) (*taskqueue.Task, error)              { return nil, nil }
func (w *mockWorker) Execute(_ context.Context, _ *taskqueue.Task) (*worker.Result, error) {
	return nil, nil
}
func (w *mockWorker) Release(_ context.Context, _ string) error { return nil }
func (w *mockWorker) Health(_ context.Context) (*worker.HealthStatus, error) {
	return w.health, nil
}
func (w *mockWorker) ID() string          { return w.health.WorkerID }
func (w *mockWorker) Tier() taskqueue.Tier { return w.health.Tier }

func TestHandleHealth(t *testing.T) {
	s := NewServer(&mockQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", resp["status"])
	}
}

func TestHandleStatusEmpty(t *testing.T) {
	s := NewServer(&mockQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.System.TotalTasks != 0 {
		t.Errorf("expected 0 total tasks, got %d", resp.System.TotalTasks)
	}
	if resp.System.SuccessRate != 0 {
		t.Errorf("expected 0 success rate, got %f", resp.System.SuccessRate)
	}
	if resp.Workers == nil {
		t.Error("expected non-nil workers slice")
	}
}

func TestHandleStatusWithTasks(t *testing.T) {
	tasks := []*taskqueue.Task{
		{ID: "1", Status: taskqueue.StatusComplete},
		{ID: "2", Status: taskqueue.StatusComplete},
		{ID: "3", Status: taskqueue.StatusFailed, ErrorMsg: "failed quality gates"},
		{ID: "4", Status: taskqueue.StatusFailed, ErrorMsg: "other error"},
		{ID: "5", Status: taskqueue.StatusPending},
	}
	wk := &mockWorker{health: &worker.HealthStatus{
		WorkerID:       "claude-1",
		Tier:           taskqueue.TierClaude,
		Healthy:        true,
		TasksCompleted: 2,
		TasksFailed:    1,
		LastHeartbeat:  "2026-06-06T00:00:00Z",
	}}
	s := NewServer(&mockQueue{tasks: tasks}, []worker.Worker{wk})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.System.TotalTasks != 5 {
		t.Errorf("expected 5 total tasks, got %d", resp.System.TotalTasks)
	}
	// 2 complete / (2+2) = 50%
	if resp.System.SuccessRate != 50.0 {
		t.Errorf("expected 50%% success rate, got %f", resp.System.SuccessRate)
	}
	if resp.System.QualityGateFailures != 1 {
		t.Errorf("expected 1 quality gate failure, got %d", resp.System.QualityGateFailures)
	}
	if resp.Queue.Pending != 1 {
		t.Errorf("expected 1 pending, got %d", resp.Queue.Pending)
	}
	if resp.Queue.Complete != 2 {
		t.Errorf("expected 2 complete, got %d", resp.Queue.Complete)
	}
	if resp.Queue.Failed != 2 {
		t.Errorf("expected 2 failed, got %d", resp.Queue.Failed)
	}
	if len(resp.Workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(resp.Workers))
	}
	if resp.Workers[0].WorkerID != "claude-1" {
		t.Errorf("expected worker claude-1, got %s", resp.Workers[0].WorkerID)
	}
	if !resp.Workers[0].Healthy {
		t.Error("expected worker to be healthy")
	}
}

func TestHandleTasksEmpty(t *testing.T) {
	s := NewServer(&mockQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Errorf("expected JSON array, got: %s", body)
	}

	var tasks []taskInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if tasks == nil {
		t.Error("expected [] not null")
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestHandleTasksReturnsTasks(t *testing.T) {
	claimedAt := time.Now().Add(-time.Hour)
	tasks := []*taskqueue.Task{
		{
			ID:          "task-1",
			IssueNumber: 42,
			RepoOwner:   "Mawar2",
			RepoName:    "Kaimi",
			Title:       "Test issue",
			Status:      taskqueue.StatusComplete,
			Tier:        taskqueue.TierClaude,
			WorkerID:    "claude-1",
			PRNumber:    101,
			ClaimedAt:   claimedAt,
		},
	}
	s := NewServer(&mockQueue{tasks: tasks}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var result []taskInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	ti := result[0]
	if ti.ID != "task-1" {
		t.Errorf("expected task-1, got %s", ti.ID)
	}
	if ti.IssueNumber != 42 {
		t.Errorf("expected issue 42, got %d", ti.IssueNumber)
	}
	if ti.Status != "complete" {
		t.Errorf("expected complete, got %s", ti.Status)
	}
	if ti.PRNumber != 101 {
		t.Errorf("expected PR 101, got %d", ti.PRNumber)
	}
}

func TestHandleIndex(t *testing.T) {
	s := NewServer(&mockQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content-type, got %s", ct)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Errorf("expected HTML body")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s := NewServer(&mockQueue{}, nil)
	endpoints := []string{"/api/health", "/api/status", "/api/tasks"}
	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: expected 405, got %d", ep, rr.Code)
		}
	}
}

func TestCORSHeader(t *testing.T) {
	s := NewServer(&mockQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %q", got)
	}
}
