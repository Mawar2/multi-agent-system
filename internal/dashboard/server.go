package dashboard

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

//go:embed static/index.html
var indexHTML []byte

// Server is the observability HTTP server for the multi-agent system.
type Server struct {
	queue     taskqueue.TaskQueue
	workers   []worker.Worker
	startTime time.Time
}

// NewServer creates a new dashboard Server.
func NewServer(queue taskqueue.TaskQueue, workers []worker.Worker) *Server {
	return &Server{
		queue:     queue,
		workers:   workers,
		startTime: time.Now(),
	}
}

// Handler returns an http.Handler serving the dashboard and REST API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/", s.handleIndex)
	return withCORS(mux)
}

// StatusResponse is returned by GET /api/status.
type StatusResponse struct {
	Workers []WorkerStatus `json:"workers"`
	Queue   QueueStatus    `json:"queue"`
	System  SystemStats    `json:"system"`
}

// WorkerStatus holds per-worker observability data.
type WorkerStatus struct {
	ID             string `json:"id"`
	Tier           string `json:"tier"`
	Healthy        bool   `json:"healthy"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
	LastHeartbeat  string `json:"last_heartbeat"`
}

// QueueStatus is the queue depth broken down by task status.
type QueueStatus struct {
	Pending    int `json:"pending"`
	Claimed    int `json:"claimed"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	Complete   int `json:"complete"`
	Failed     int `json:"failed"`
	Total      int `json:"total"`
}

// SystemStats holds aggregate computed metrics.
type SystemStats struct {
	SuccessRate         float64 `json:"success_rate"`
	QualityGateFailures int     `json:"quality_gate_failures"`
	EstimatedSavingsUSD float64 `json:"estimated_savings_usd"`
	UptimeSeconds       int64   `json:"uptime_seconds"`
}

type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, healthResponse{Status: "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()

	workerStatuses := make([]WorkerStatus, 0, len(s.workers))
	for _, wkr := range s.workers {
		h, err := wkr.Health(ctx)
		if err != nil {
			workerStatuses = append(workerStatuses, WorkerStatus{
				ID:      wkr.ID(),
				Tier:    wkr.Tier().String(),
				Healthy: false,
			})
			continue
		}
		workerStatuses = append(workerStatuses, WorkerStatus{
			ID:             h.WorkerID,
			Tier:           h.Tier.String(),
			Healthy:        h.Healthy,
			TasksCompleted: h.TasksCompleted,
			TasksFailed:    h.TasksFailed,
			LastHeartbeat:  h.LastHeartbeat,
		})
	}

	all, err := s.queue.List(ctx, &taskqueue.TaskFilter{})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	qs := QueueStatus{}
	qualityGateFailures := 0
	for _, t := range all {
		switch t.Status {
		case taskqueue.StatusPending:
			qs.Pending++
		case taskqueue.StatusClaimed:
			qs.Claimed++
		case taskqueue.StatusInProgress:
			qs.InProgress++
		case taskqueue.StatusReview:
			qs.Review++
		case taskqueue.StatusComplete:
			qs.Complete++
		case taskqueue.StatusFailed:
			qs.Failed++
			if strings.Contains(t.ErrorMsg, "quality gates") {
				qualityGateFailures++
			}
		}
	}
	qs.Total = len(all)

	terminal := qs.Complete + qs.Failed
	successRate := 0.0
	if terminal > 0 {
		successRate = float64(qs.Complete) / float64(terminal)
	}

	writeJSON(w, StatusResponse{
		Workers: workerStatuses,
		Queue:   qs,
		System: SystemStats{
			SuccessRate:         successRate,
			QualityGateFailures: qualityGateFailures,
			EstimatedSavingsUSD: float64(qualityGateFailures) * 0.10,
			UptimeSeconds:       int64(time.Since(s.startTime).Seconds()),
		},
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()

	all, err := s.queue.List(ctx, &taskqueue.TaskFilter{})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sort.Slice(all, func(i, j int) bool {
		return latestTime(all[i]).After(latestTime(all[j]))
	})

	limit := len(all)
	if limit > 30 {
		limit = 30
	}
	tasks := make([]*taskqueue.Task, 0, limit)
	tasks = append(tasks, all[:limit]...)
	writeJSON(w, tasks)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func latestTime(t *taskqueue.Task) time.Time {
	if !t.CompletedAt.IsZero() {
		return t.CompletedAt
	}
	if !t.StartedAt.IsZero() {
		return t.StartedAt
	}
	return t.ClaimedAt
}

