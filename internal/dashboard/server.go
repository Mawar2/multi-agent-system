package dashboard

import (
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

//go:embed static/index.html
var staticFiles embed.FS

// Server serves the observability dashboard and REST API.
type Server struct {
	queue   taskqueue.TaskQueue
	workers []worker.Worker
	mux     *http.ServeMux
}

// NewServer creates a new dashboard Server.
func NewServer(queue taskqueue.TaskQueue, workers []worker.Worker) *Server {
	s := &Server{
		queue:   queue,
		workers: workers,
		mux:     http.NewServeMux(),
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/tasks", s.handleTasks)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Listen starts the HTTP server on addr. Blocks until the server exits.
func (s *Server) Listen(addr string) error {
	return http.ListenAndServe(addr, s)
}

// --- JSON response types ---

type healthResponse struct {
	Status string `json:"status"`
}

type workerInfo struct {
	WorkerID       string `json:"worker_id"`
	Tier           string `json:"tier"`
	Healthy        bool   `json:"healthy"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
	LastHeartbeat  string `json:"last_heartbeat"`
}

type queueStats struct {
	Pending    int `json:"pending"`
	Claimed    int `json:"claimed"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	Complete   int `json:"complete"`
	Failed     int `json:"failed"`
}

type systemStats struct {
	TotalTasks           int     `json:"total_tasks"`
	SuccessRate          float64 `json:"success_rate"`
	QualityGateFailures  int     `json:"quality_gate_failures"`
	EstimatedCostSavings float64 `json:"estimated_cost_savings"`
}

type statusResponse struct {
	Workers []workerInfo `json:"workers"`
	Queue   queueStats   `json:"queue"`
	System  systemStats  `json:"system"`
}

type taskInfo struct {
	ID          string `json:"id"`
	IssueNumber int    `json:"issue_number"`
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Tier        string `json:"tier"`
	WorkerID    string `json:"worker_id"`
	PRNumber    int    `json:"pr_number"`
	ClaimedAt   string `json:"claimed_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	ErrorMsg    string `json:"error_msg,omitempty"`
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- Handlers ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	content, _ := staticFiles.ReadFile("static/index.html")
	_, _ = w.Write(content)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	workers := make([]workerInfo, 0, len(s.workers))
	for _, wk := range s.workers {
		h, err := wk.Health(ctx)
		if err != nil || h == nil {
			continue
		}
		workers = append(workers, workerInfo{
			WorkerID:       h.WorkerID,
			Tier:           h.Tier.String(),
			Healthy:        h.Healthy,
			TasksCompleted: h.TasksCompleted,
			TasksFailed:    h.TasksFailed,
			LastHeartbeat:  h.LastHeartbeat,
		})
	}

	tasks, _ := s.queue.List(ctx, nil)

	var qs queueStats
	var complete, failed, qualityGateFailures int

	for _, t := range tasks {
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
			complete++
		case taskqueue.StatusFailed:
			qs.Failed++
			failed++
			if strings.Contains(t.ErrorMsg, "quality gates") {
				qualityGateFailures++
			}
		}
	}

	total := qs.Pending + qs.Claimed + qs.InProgress + qs.Review + qs.Complete + qs.Failed

	var successRate float64
	if terminal := complete + failed; terminal > 0 {
		successRate = float64(complete) / float64(terminal) * 100
	}

	writeJSON(w, http.StatusOK, statusResponse{
		Workers: workers,
		Queue:   qs,
		System: systemStats{
			TotalTasks:           total,
			SuccessRate:          successRate,
			QualityGateFailures:  qualityGateFailures,
			EstimatedCostSavings: float64(qualityGateFailures) * 0.10,
		},
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	tasks, _ := s.queue.List(ctx, nil)

	sort.Slice(tasks, func(i, j int) bool {
		ti, tj := tasks[i].ClaimedAt, tasks[j].ClaimedAt
		if ti.IsZero() && tj.IsZero() {
			return tasks[i].ID > tasks[j].ID
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	const maxTasks = 30
	if len(tasks) > maxTasks {
		tasks = tasks[:maxTasks]
	}

	result := make([]taskInfo, 0, len(tasks))
	for _, t := range tasks {
		ti := taskInfo{
			ID:          t.ID,
			IssueNumber: t.IssueNumber,
			RepoOwner:   t.RepoOwner,
			RepoName:    t.RepoName,
			Title:       t.Title,
			Status:      t.Status.String(),
			Tier:        t.Tier.String(),
			WorkerID:    t.WorkerID,
			PRNumber:    t.PRNumber,
			ErrorMsg:    t.ErrorMsg,
		}
		if !t.ClaimedAt.IsZero() {
			ti.ClaimedAt = t.ClaimedAt.Format(time.RFC3339)
		}
		if !t.CompletedAt.IsZero() {
			ti.CompletedAt = t.CompletedAt.Format(time.RFC3339)
		}
		result = append(result, ti)
	}

	writeJSON(w, http.StatusOK, result)
}
