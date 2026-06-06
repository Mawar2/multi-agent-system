package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

//go:embed static
var staticFiles embed.FS

// Server is the observability HTTP server.
type Server struct {
	queue   taskqueue.TaskQueue
	workers []worker.Worker
	mux     *http.ServeMux
}

// NewServer creates a new dashboard Server.
func NewServer(queue taskqueue.TaskQueue, workers []worker.Worker) *Server {
	s := &Server{queue: queue, workers: workers, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/tasks", s.handleTasks)
	sub, _ := fs.Sub(staticFiles, "static")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
	return s
}

// Handler returns the http.Handler (useful for testing).
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe starts the HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

// StatusResponse is the payload returned by /api/status.
type StatusResponse struct {
	QueueCounts         map[string]int `json:"queue_counts"`
	SuccessRate         float64        `json:"success_rate"`
	QualityGateFailures int            `json:"quality_gate_failures"`
	EstimatedSavings    float64        `json:"estimated_savings_usd"`
	Workers             []WorkerInfo   `json:"workers"`
}

// WorkerInfo summarises a single worker for the status payload.
type WorkerInfo struct {
	ID             string `json:"id"`
	Tier           string `json:"tier"`
	Healthy        bool   `json:"healthy"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
}

func corsJSON(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	corsJSON(w)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := context.Background()
	tasks, _ := s.queue.List(ctx, nil)

	counts := map[string]int{
		"pending":     0,
		"claimed":     0,
		"in_progress": 0,
		"review":      0,
		"complete":    0,
		"failed":      0,
	}
	var completed, failed, qgFailed int
	for _, t := range tasks {
		counts[t.Status.String()]++
		switch t.Status {
		case taskqueue.StatusComplete:
			completed++
		case taskqueue.StatusFailed:
			failed++
			if strings.Contains(t.ErrorMsg, "quality gate") {
				qgFailed++
			}
		}
	}

	total := completed + failed
	var successRate float64
	if total > 0 {
		successRate = float64(completed) / float64(total) * 100
	}
	// Each quality-gate failure saves ~$0.10 in AI review cost.
	estimatedSavings := float64(qgFailed) * 0.10

	workerInfos := make([]WorkerInfo, 0, len(s.workers))
	for _, wk := range s.workers {
		hs, err := wk.Health(ctx)
		if err != nil {
			workerInfos = append(workerInfos, WorkerInfo{
				ID:      wk.ID(),
				Tier:    wk.Tier().String(),
				Healthy: false,
			})
			continue
		}
		workerInfos = append(workerInfos, WorkerInfo{
			ID:             hs.WorkerID,
			Tier:           hs.Tier.String(),
			Healthy:        hs.Healthy,
			TasksCompleted: hs.TasksCompleted,
			TasksFailed:    hs.TasksFailed,
		})
	}

	writeJSON(w, StatusResponse{
		QueueCounts:         counts,
		SuccessRate:         successRate,
		QualityGateFailures: qgFailed,
		EstimatedSavings:    estimatedSavings,
		Workers:             workerInfos,
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tasks, _ := s.queue.List(context.Background(), nil)
	if tasks == nil {
		tasks = []*taskqueue.Task{}
	}
	writeJSON(w, tasks)
}
