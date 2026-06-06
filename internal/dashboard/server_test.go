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

// --- fake queue ---

type fakeQueue struct {
	tasks []*taskqueue.Task
}

func (f *fakeQueue) Enqueue(_ context.Context, t *taskqueue.Task) error { f.tasks = append(f.tasks, t); return nil }
func (f *fakeQueue) Dequeue(_ context.Context, _ taskqueue.Tier, _ string) (*taskqueue.Task, error) {
	return nil, nil
}
func (f *fakeQueue) Update(_ context.Context, t *taskqueue.Task) error { return nil }
func (f *fakeQueue) Get(_ context.Context, id string) (*taskqueue.Task, error) {
	for _, t := range f.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, nil
}
func (f *fakeQueue) List(_ context.Context, _ *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	return f.tasks, nil
}
func (f *fakeQueue) Release(_ context.Context, _ string) error { return nil }

// --- fake worker ---

type fakeWorker struct {
	id     string
	tier   taskqueue.Tier
	health *worker.HealthStatus
}

func (fw *fakeWorker) Claim(_ context.Context) (*taskqueue.Task, error)              { return nil, nil }
func (fw *fakeWorker) Execute(_ context.Context, _ *taskqueue.Task) (*worker.Result, error) {
	return nil, nil
}
func (fw *fakeWorker) Release(_ context.Context, _ string) error { return nil }
func (fw *fakeWorker) Health(_ context.Context) (*worker.HealthStatus, error) {
	return fw.health, nil
}
func (fw *fakeWorker) ID() string           { return fw.id }
func (fw *fakeWorker) Tier() taskqueue.Tier { return fw.tier }

// --- helpers ---

func newServer(q taskqueue.TaskQueue, workers []worker.Worker) *Server {
	return NewServer(q, workers)
}

func doGET(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func doPOST(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

// --- tests ---

func TestHandleHealth(t *testing.T) {
	s := newServer(&fakeQueue{}, nil)
	rr := doGET(t, s, "/api/health")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", resp["status"])
	}
}

func TestHandleStatusEmpty(t *testing.T) {
	s := newServer(&fakeQueue{}, nil)
	rr := doGET(t, s, "/api/status")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.SuccessRate != 0 {
		t.Errorf("expected success_rate=0, got %f", resp.SuccessRate)
	}
	if resp.QueueCounts == nil {
		t.Error("queue_counts must not be nil")
	}
	if resp.Workers == nil {
		t.Error("workers must not be nil")
	}
}

func TestHandleStatusWithTasks(t *testing.T) {
	q := &fakeQueue{tasks: []*taskqueue.Task{
		{ID: "1", Status: taskqueue.StatusComplete},
		{ID: "2", Status: taskqueue.StatusFailed, ErrorMsg: "quality gate: tests failed"},
		{ID: "3", Status: taskqueue.StatusFailed, ErrorMsg: "some other error"},
		{ID: "4", Status: taskqueue.StatusReview},
	}}
	ws := []worker.Worker{
		&fakeWorker{
			id:   "claude-1",
			tier: taskqueue.TierClaude,
			health: &worker.HealthStatus{
				WorkerID:       "claude-1",
				Tier:           taskqueue.TierClaude,
				Healthy:        true,
				TasksCompleted: 3,
				TasksFailed:    1,
			},
		},
	}
	s := newServer(q, ws)
	rr := doGET(t, s, "/api/status")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	// 1 complete / (1 complete + 2 failed) = 33.33...%
	if resp.SuccessRate < 33.0 || resp.SuccessRate > 34.0 {
		t.Errorf("unexpected success_rate %f", resp.SuccessRate)
	}
	if resp.QueueCounts["complete"] != 1 {
		t.Errorf("expected 1 complete, got %d", resp.QueueCounts["complete"])
	}
	if resp.QueueCounts["review"] != 1 {
		t.Errorf("expected 1 review, got %d", resp.QueueCounts["review"])
	}
	if resp.QualityGateFailures != 1 {
		t.Errorf("expected 1 quality gate failure, got %d", resp.QualityGateFailures)
	}
	if resp.EstimatedSavings != 0.10 {
		t.Errorf("expected $0.10 savings, got %f", resp.EstimatedSavings)
	}
	if len(resp.Workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(resp.Workers))
	}
	if !resp.Workers[0].Healthy {
		t.Error("expected worker to be healthy")
	}
	if resp.Workers[0].TasksCompleted != 3 {
		t.Errorf("expected 3 completed, got %d", resp.Workers[0].TasksCompleted)
	}
}

func TestHandleTasksEmpty(t *testing.T) {
	s := newServer(&fakeQueue{}, nil)
	rr := doGET(t, s, "/api/tasks")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := strings.TrimSpace(rr.Body.String())
	if body == "null" {
		t.Error("expected [] not null")
	}
	var tasks []*taskqueue.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if tasks == nil {
		t.Error("expected non-nil slice, got nil")
	}
}

func TestHandleTasksReturnsTasks(t *testing.T) {
	now := time.Now()
	q := &fakeQueue{tasks: []*taskqueue.Task{
		{ID: "abc", IssueNumber: 42, Title: "fix bug", Status: taskqueue.StatusPending, ClaimedAt: now},
	}}
	s := newServer(q, nil)
	rr := doGET(t, s, "/api/tasks")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var tasks []*taskqueue.Task
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "abc" {
		t.Errorf("expected id=abc, got %q", tasks[0].ID)
	}
	if tasks[0].IssueNumber != 42 {
		t.Errorf("expected issue_number=42, got %d", tasks[0].IssueNumber)
	}
}

func TestHandleIndex(t *testing.T) {
	s := newServer(&fakeQueue{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content-type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Error("response body does not contain <html")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s := newServer(&fakeQueue{}, nil)

	for _, path := range []string{"/api/health", "/api/status", "/api/tasks"} {
		rr := doPOST(t, s, path)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: expected 405, got %d", path, rr.Code)
		}
	}
}

func TestCORSHeader(t *testing.T) {
	s := newServer(&fakeQueue{}, nil)

	for _, path := range []string{"/api/health", "/api/status", "/api/tasks"} {
		rr := doGET(t, s, path)
		got := rr.Header().Get("Access-Control-Allow-Origin")
		if got != "*" {
			t.Errorf("GET %s: expected CORS header *, got %q", path, got)
		}
	}
}
