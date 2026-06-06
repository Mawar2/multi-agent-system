package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/dashboard"
	"github.com/Mawar2/multi-agent-system/internal/llm"
	"github.com/Mawar2/multi-agent-system/internal/orchestrator"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "orchestrator.yml", "Path to configuration file")
	dashboardAddr := flag.String("dashboard-addr", ":8080", "Dashboard listen address (empty to disable)")
	flag.Parse()

	// Load configuration
	fmt.Printf("Loading configuration from %s...\n", *configPath)
	config, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Initialize task queue
	fmt.Printf("Initializing task queue at %s...\n", config.TaskQueueDir)
	queue, err := taskqueue.NewJSONQueue(config.TaskQueueDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating task queue: %v\n", err)
		os.Exit(1)
	}

	// Initialize router
	fmt.Println("Initializing task router...")
	router := orchestrator.NewRuleBasedRouter()

	// Initialize GitHub REST client (uses gh CLI)
	fmt.Println("Initializing GitHub REST client...")
	restClient := ticket.NewGitHubRESTClient()

	// Initialize supervisor (use REST client directly for PR monitoring support)
	fmt.Println("Initializing supervisor...")
	supervisor := orchestrator.NewSupervisor(config, queue, router, restClient)

	// Initialize workers
	fmt.Println("Initializing worker pools...")
	workers := initializeWorkers(config, queue)
	fmt.Printf("Started %d workers\n", len(workers))

	// Start observability dashboard
	if *dashboardAddr != "" {
		srv := dashboard.NewServer(queue, workers)
		go func() {
			fmt.Printf("Dashboard listening on http://%s\n", *dashboardAddr)
			if err := srv.ListenAndServe(*dashboardAddr); err != nil {
				fmt.Fprintf(os.Stderr, "Dashboard error: %v\n", err)
			}
		}()
	}

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal, stopping...")
		cancel()
	}()

	// Start supervisor
	fmt.Println("Starting supervisor...")
	fmt.Printf("Monitoring %d project(s):\n", len(config.Projects))
	for _, proj := range config.Projects {
		fmt.Printf("  - %s/%s\n", proj.RepoOwner, proj.RepoName)
	}
	fmt.Println("\nSupervisor running. Press Ctrl+C to stop.")

	// Start workers
	for _, w := range workers {
		go runWorker(ctx, w)
	}

	// Run supervisor (blocks until context cancelled)
	if err := supervisor.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Supervisor error: %v\n", err)
		os.Exit(1)
	}

	// Graceful shutdown
	fmt.Println("Shutting down...")
	if err := supervisor.Shutdown(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
	}

	fmt.Println("Supervisor stopped.")
}

// initializeWorkers creates worker pools based on configuration.
func initializeWorkers(config *orchestrator.Config, queue taskqueue.TaskQueue) []worker.Worker {
	var workers []worker.Worker

	// Create LLM backend (Claude Code CLI for Phase 1)
	backend := llm.NewClaudeCodeBackend()

	// Gemini Flash workers
	for i := 0; i < config.WorkerTiers.GeminiFlash.MaxWorkers; i++ {
		workerID := fmt.Sprintf("gemini-flash-%d", i+1)
		w := worker.NewClaudeCodeWorker(
			workerID,
			taskqueue.TierGeminiFlash,
			queue,
			backend,
			"./projects", // Project checkout directory
		)
		workers = append(workers, w)
	}

	// Gemini Pro workers
	for i := 0; i < config.WorkerTiers.GeminiPro.MaxWorkers; i++ {
		workerID := fmt.Sprintf("gemini-pro-%d", i+1)
		w := worker.NewClaudeCodeWorker(
			workerID,
			taskqueue.TierGeminiPro,
			queue,
			backend,
			"./projects",
		)
		workers = append(workers, w)
	}

	// Claude workers
	for i := 0; i < config.WorkerTiers.Claude.MaxWorkers; i++ {
		workerID := fmt.Sprintf("claude-%d", i+1)
		w := worker.NewClaudeCodeWorker(
			workerID,
			taskqueue.TierClaude,
			queue,
			backend,
			"./projects",
		)
		workers = append(workers, w)
	}

	return workers
}

// runWorker runs a single worker in a loop.
func runWorker(ctx context.Context, w worker.Worker) {
	fmt.Printf("[%s] Worker started (tier: %s)\n", w.ID(), w.Tier())

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] Worker stopping\n", w.ID())
			return
		default:
			// Try to claim a task
			task, err := w.Claim(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s] Error claiming task: %v\n", w.ID(), err)
				continue
			}

			if task == nil {
				// No tasks available, wait a bit
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			// Execute the task
			fmt.Printf("[%s] Claimed task %s (issue #%d)\n", w.ID(), task.ID, task.IssueNumber)
			result, err := w.Execute(ctx, task)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s] Error executing task %s: %v\n", w.ID(), task.ID, err)
				// Release task back to queue
				if err := w.Release(ctx, task.ID); err != nil {
					fmt.Fprintf(os.Stderr, "[%s] Error releasing task %s: %v\n", w.ID(), task.ID, err)
				}
				continue
			}

			if result.Success {
				fmt.Printf("[%s] Completed task %s - PR #%d created\n", w.ID(), task.ID, result.PRNumber)
			} else {
				fmt.Printf("[%s] Task %s failed: %s\n", w.ID(), task.ID, result.ErrorMsg)
			}
		}
	}
}
