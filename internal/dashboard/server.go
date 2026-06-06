package dashboard

import (
	"context"
	"encoding/json"
	_ "embed"
	"net/http"
	"strings"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

//go:embed static/index.html
var indexHTML []byte

// Server is an HTTP server exposing system observability data via REST API
// and a browser dashboard UI.
type Server struct {
	addr    string
	queue   taskqueue.TaskQueue
	workers []worker.Worker
	httpSrv *http.Server
}

// New creates a new dashboard Server.
func New(addr string, queue taskqueue.TaskQueue, workers []worker.Worker) *Server {
	return &Server{
		addr:    addr,
		queue:   queue,
		workers: workers,
	}
}

// Start begins serving the dashboard on s.addr. It blocks until ctx is cancelled,
// then performs a graceful shutdown with a 5-second deadline.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/", s.handleIndex)

	s.httpSrv = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
	}()

	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// StatusResponse is the payload returned by GET /api/status.
type StatusResponse struct {
	Timestamp string         `json:"timestamp"`
	Workers   []WorkerStatus `json:"workers"`
	Queue     QueueStatus    `json:"queue"`
	Stats     SystemStats    `json:"stats"`
}

// WorkerStatus holds per-worker observability data.
type WorkerStatus struct {
	ID             string `json:"id"`
	Tier           string `json:"tier"`
	Healthy        bool   `json:"healthy"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
	LastHeartbeat  string `json:"last_heartbeat"`
	ErrorMsg       string `json:"error_msg,omitempty"`
}

// QueueStatus aggregates task counts by status.
type QueueStatus struct {
	// Active counts (work not yet done)
	Depth      int `json:"depth"`       // pending + claimed + in_progress
	Pending    int `json:"pending"`
	Claimed    int `json:"claimed"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	// Terminal counts
	Complete int `json:"complete"`
	Failed   int `json:"failed"`
	Total    int `json:"total"`
}

// SystemStats holds aggregate metrics computed from queue state.
type SystemStats struct {
	TotalTasks           int     `json:"total_tasks"`
	SuccessRate          float64 `json:"success_rate"`           // percentage 0–100
	QualityGateFailures  int     `json:"quality_gate_failures"`  // tasks rejected by quality gates
	EstimatedCostSavings float64 `json:"estimated_cost_savings"` // USD saved by gate filtering
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Collect worker health.
	workerStatuses := make([]WorkerStatus, 0, len(s.workers))
	for _, wkr := range s.workers {
		health, err := wkr.Health(ctx)
		if err != nil {
			workerStatuses = append(workerStatuses, WorkerStatus{
				ID:       wkr.ID(),
				Tier:     wkr.Tier().String(),
				Healthy:  false,
				ErrorMsg: err.Error(),
			})
			continue
		}
		workerStatuses = append(workerStatuses, WorkerStatus{
			ID:             health.WorkerID,
			Tier:           health.Tier.String(),
			Healthy:        health.Healthy,
			TasksCompleted: health.TasksCompleted,
			TasksFailed:    health.TasksFailed,
			LastHeartbeat:  health.LastHeartbeat,
			ErrorMsg:       health.ErrorMsg,
		})
	}

	// Aggregate queue stats.
	allTasks, err := s.queue.List(ctx, &taskqueue.TaskFilter{})
	if err != nil {
		http.Error(w, "failed to list tasks", http.StatusInternalServerError)
		return
	}

	var qs QueueStatus
	qualityGateFailures := 0
	for _, task := range allTasks {
		qs.Total++
		switch task.Status {
		case taskqueue.StatusPending:
			qs.Pending++
			qs.Depth++
		case taskqueue.StatusClaimed:
			qs.Claimed++
			qs.Depth++
		case taskqueue.StatusInProgress:
			qs.InProgress++
			qs.Depth++
		case taskqueue.StatusReview:
			qs.Review++
		case taskqueue.StatusComplete:
			qs.Complete++
		case taskqueue.StatusFailed:
			qs.Failed++
			if strings.Contains(task.ErrorMsg, "quality gates") {
				qualityGateFailures++
			}
		}
	}

	var successRate float64
	if terminal := qs.Complete + qs.Failed; terminal > 0 {
		successRate = float64(qs.Complete) / float64(terminal) * 100
	}

	resp := StatusResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Workers:   workerStatuses,
		Queue:     qs,
		Stats: SystemStats{
			TotalTasks:           qs.Total,
			SuccessRate:          successRate,
			QualityGateFailures:  qualityGateFailures,
			EstimatedCostSavings: float64(qualityGateFailures) * 0.10,
		},
	}

	writeJSON(w, resp)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tasks, err := s.queue.List(r.Context(), &taskqueue.TaskFilter{})
	if err != nil {
		http.Error(w, "failed to list tasks", http.StatusInternalServerError)
		return
	}

	// Ensure JSON encodes as [] not null when empty.
	if tasks == nil {
		tasks = []*taskqueue.Task{}
	}

	writeJSON(w, tasks)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
