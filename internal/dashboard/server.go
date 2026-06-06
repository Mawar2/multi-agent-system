package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	_ "embed"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

//go:embed static/index.html
var indexHTML []byte

// WorkerProvider returns the current list of workers.
type WorkerProvider interface {
	Workers() []worker.Worker
}

// Server serves the observability dashboard and REST API.
type Server struct {
	queue   taskqueue.TaskQueue
	workers WorkerProvider
	srv     *http.Server
}

// NewServer creates a new dashboard Server.
func NewServer(addr string, queue taskqueue.TaskQueue, workers WorkerProvider) *Server {
	s := &Server{queue: queue, workers: workers}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/", s.handleIndex)
	s.srv = &http.Server{Addr: addr, Handler: corsMiddleware(mux)}
	return s
}

// Start begins listening in a background goroutine. It returns immediately.
func (s *Server) Start() {
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[dashboard] server error: %v\n", err)
		}
	}()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// HealthResponse is the payload for GET /api/health.
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// WorkerInfo is a single worker's state in the status response.
type WorkerInfo struct {
	ID             string `json:"id"`
	Tier           string `json:"tier"`
	Healthy        bool   `json:"healthy"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
	LastHeartbeat  string `json:"last_heartbeat"`
}

// QueueCounts breaks down the task queue by status.
type QueueCounts struct {
	Pending    int `json:"pending"`
	Claimed    int `json:"claimed"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	Complete   int `json:"complete"`
	Failed     int `json:"failed"`
	Total      int `json:"total"`
}

// SystemStats aggregates high-level system metrics.
type SystemStats struct {
	SuccessRate         float64 `json:"success_rate"`
	QualityGateFailures int     `json:"quality_gate_failures"`
	EstimatedCostSaved  float64 `json:"estimated_cost_saved_usd"`
}

// StatusResponse is the payload for GET /api/status.
type StatusResponse struct {
	Workers []WorkerInfo `json:"workers"`
	Queue   QueueCounts  `json:"queue"`
	Stats   SystemStats  `json:"stats"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, HealthResponse{Status: "ok", Timestamp: time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Collect worker health.
	workerInfos := []WorkerInfo{}
	if s.workers != nil {
		for _, wk := range s.workers.Workers() {
			h, err := wk.Health(ctx)
			if err != nil {
				workerInfos = append(workerInfos, WorkerInfo{
					ID:      wk.ID(),
					Tier:    wk.Tier().String(),
					Healthy: false,
				})
				continue
			}
			workerInfos = append(workerInfos, WorkerInfo{
				ID:             h.WorkerID,
				Tier:           h.Tier.String(),
				Healthy:        h.Healthy,
				TasksCompleted: h.TasksCompleted,
				TasksFailed:    h.TasksFailed,
				LastHeartbeat:  h.LastHeartbeat,
			})
		}
	}

	// Aggregate queue counts.
	var counts QueueCounts
	var qualityGateFailures int
	if s.queue != nil {
		tasks, err := s.queue.List(ctx, &taskqueue.TaskFilter{})
		if err == nil {
			for _, t := range tasks {
				counts.Total++
				switch t.Status {
				case taskqueue.StatusPending:
					counts.Pending++
				case taskqueue.StatusClaimed:
					counts.Claimed++
				case taskqueue.StatusInProgress:
					counts.InProgress++
				case taskqueue.StatusReview:
					counts.Review++
				case taskqueue.StatusComplete:
					counts.Complete++
				case taskqueue.StatusFailed:
					counts.Failed++
				}
				if t.ErrorMsg != "" && containsQualityGateFailure(t.ErrorMsg) {
					qualityGateFailures++
				}
			}
		}
	}

	// Compute success rate over terminal tasks.
	terminal := counts.Complete + counts.Failed
	var successRate float64
	if terminal > 0 {
		successRate = float64(counts.Complete) / float64(terminal) * 100
	}

	// Estimated cost saved: quality gate failures × $0.10 each.
	estimatedSaved := float64(qualityGateFailures) * 0.10

	writeJSON(w, StatusResponse{
		Workers: workerInfos,
		Queue:   counts,
		Stats: SystemStats{
			SuccessRate:         successRate,
			QualityGateFailures: qualityGateFailures,
			EstimatedCostSaved:  estimatedSaved,
		},
	})
}

// TaskResponse is a condensed task view returned by GET /api/tasks.
type TaskResponse struct {
	ID          string `json:"id"`
	IssueNumber int    `json:"issue_number"`
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Tier        string `json:"tier"`
	Complexity  string `json:"complexity"`
	WorkerID    string `json:"worker_id"`
	PRNumber    int    `json:"pr_number"`
	Attempts    int    `json:"attempts"`
	ErrorMsg    string `json:"error_msg,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	result := []TaskResponse{} // non-nil slice so JSON marshals as [] not null
	if s.queue != nil {
		tasks, err := s.queue.List(ctx, &taskqueue.TaskFilter{})
		if err == nil {
			// Sort newest-first by CompletedAt, then StartedAt.
			sort.Slice(tasks, func(i, j int) bool {
				ti := tasks[i].CompletedAt
				tj := tasks[j].CompletedAt
				if ti.IsZero() {
					ti = tasks[i].StartedAt
				}
				if tj.IsZero() {
					tj = tasks[j].StartedAt
				}
				return ti.After(tj)
			})

			// Return the 30 most recent tasks.
			limit := 30
			if len(tasks) < limit {
				limit = len(tasks)
			}
			for _, t := range tasks[:limit] {
				tr := TaskResponse{
					ID:          t.ID,
					IssueNumber: t.IssueNumber,
					RepoOwner:   t.RepoOwner,
					RepoName:    t.RepoName,
					Title:       t.Title,
					Status:      t.Status.String(),
					Tier:        t.Tier.String(),
					Complexity:  t.Complexity.String(),
					WorkerID:    t.WorkerID,
					PRNumber:    t.PRNumber,
					Attempts:    t.Attempts,
					ErrorMsg:    t.ErrorMsg,
				}
				if !t.CompletedAt.IsZero() {
					tr.CompletedAt = t.CompletedAt.UTC().Format(time.RFC3339)
				}
				result = append(result, tr)
			}
		}
	}

	writeJSON(w, result)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}

// corsMiddleware adds permissive CORS headers (dashboard is localhost-only in prod).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func containsQualityGateFailure(msg string) bool {
	for _, kw := range []string{"quality gate", "quality gates", "tests failed", "linter failed", "formatter failed"} {
		if len(msg) >= len(kw) {
			for i := 0; i <= len(msg)-len(kw); i++ {
				if msg[i:i+len(kw)] == kw {
					return true
				}
			}
		}
	}
	return false
}
