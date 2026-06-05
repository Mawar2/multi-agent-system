// Package orchestrator provides the supervisor and routing logic for the multi-agent system.
// The supervisor polls GitHub Issues, classifies them by complexity, and routes them to
// the appropriate worker tier.
package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
	"github.com/google/uuid"
)

// Supervisor is the coordinator that polls GitHub Issues, routes them to the task queue,
// and monitors worker progress.
//
// The supervisor runs a continuous loop that:
// 1. Polls GitHub Issues from configured projects
// 2. Routes each issue (classifies complexity, assigns tier)
// 3. Enqueues tasks for workers to claim
// 4. Monitors for stalled tasks (timeout check)
// 5. Releases stalled tasks back to the queue
type Supervisor struct {
	config       *Config
	queue        taskqueue.TaskQueue
	router       Router
	ticketClient ticket.Client

	// Internal state for monitoring
	shutdown chan struct{}
	done     chan struct{}
}

// NewSupervisor creates a new Supervisor instance.
func NewSupervisor(
	config *Config,
	queue taskqueue.TaskQueue,
	router Router,
	ticketClient ticket.Client,
) *Supervisor {
	return &Supervisor{
		config:       config,
		queue:        queue,
		router:       router,
		ticketClient: ticketClient,
		shutdown:     make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Run starts the supervisor's main loop.
// It polls GitHub Issues, routes them to the queue, and monitors for stalled tasks.
// Runs until the context is cancelled.
func (s *Supervisor) Run(ctx context.Context) error {
	log.Println("Supervisor: Starting main loop")

	pollInterval := time.Duration(s.config.PollIntervalSeconds) * time.Second
	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	// Monitor stalled tasks every 30 seconds
	monitorTicker := time.NewTicker(30 * time.Second)
	defer monitorTicker.Stop()

	// Do initial poll immediately
	if err := s.pollIssues(ctx); err != nil {
		log.Printf("Supervisor: Initial poll failed: %v", err)
	}

	// Start monitoring loop
	for {
		select {
		case <-ctx.Done():
			log.Println("Supervisor: Context cancelled, shutting down")
			close(s.done)
			return ctx.Err()

		case <-s.shutdown:
			log.Println("Supervisor: Shutdown signal received")
			close(s.done)
			return nil

		case <-pollTicker.C:
			log.Println("Supervisor: Polling GitHub Issues")
			if err := s.pollIssues(ctx); err != nil {
				log.Printf("Supervisor: Poll failed: %v", err)
				// Continue loop on error - don't crash
			}

		case <-monitorTicker.C:
			log.Println("Supervisor: Monitoring for stalled tasks")
			if err := s.monitorStalledTasks(ctx); err != nil {
				log.Printf("Supervisor: Stall monitoring failed: %v", err)
				// Continue loop on error
			}
		}
	}
}

// pollIssues fetches issues from all configured projects and processes them.
func (s *Supervisor) pollIssues(ctx context.Context) error {
	for _, project := range s.config.Projects {
		log.Printf("Supervisor: Polling project %s/%s", project.RepoOwner, project.RepoName)

		// Fetch open issues from the repository
		issues, err := s.ticketClient.FetchIssues(ctx, project.RepoOwner, project.RepoName, project.Labels)
		if err != nil {
			log.Printf("Supervisor: Failed to fetch issues for %s/%s: %v",
				project.RepoOwner, project.RepoName, err)
			return fmt.Errorf("failed to fetch issues for %s/%s: %w",
				project.RepoOwner, project.RepoName, err)
		}

		log.Printf("Supervisor: Found %d open issues in %s/%s", len(issues), project.RepoOwner, project.RepoName)

		// Process each issue
		for _, issue := range issues {
			if err := s.processIssue(ctx, issue); err != nil {
				log.Printf("Supervisor: Failed to process issue #%d: %v", issue.Number, err)
				// Continue processing other issues on error
			}
		}
	}

	return nil
}

// processIssue routes a single issue and enqueues it as a task.
// Skips issues that already have PRs or are already in the queue.
func (s *Supervisor) processIssue(ctx context.Context, issue *ticket.Issue) error {
	log.Printf("Supervisor: Processing issue #%d: %s", issue.Number, issue.Title)

	// Check if issue already has a PR
	prStatus, err := s.ticketClient.CheckPRStatus(ctx, issue.RepoOwner, issue.RepoName, issue.Number)
	if err != nil {
		log.Printf("Supervisor: Failed to check PR status for issue #%d: %v", issue.Number, err)
		// Continue processing - treat as no PR
	} else if prStatus != nil {
		log.Printf("Supervisor: Skipping issue #%d - already has PR #%d (%s)",
			issue.Number, prStatus.Number, prStatus.State)
		return nil
	}

	// Check if issue is already in the queue
	filter := &taskqueue.TaskFilter{
		RepoOwner: issue.RepoOwner,
		RepoName:  issue.RepoName,
	}
	existingTasks, err := s.queue.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to list existing tasks: %w", err)
	}

	for _, task := range existingTasks {
		if task.IssueNumber == issue.Number && !task.Status.IsTerminal() {
			log.Printf("Supervisor: Skipping issue #%d - already in queue as task %s (status: %v)",
				issue.Number, task.ID, task.Status)
			return nil
		}
	}

	// Route the issue (classify complexity and assign tier)
	complexity, tier, err := s.router.Route(ctx, issue)
	if err != nil {
		return fmt.Errorf("failed to route issue #%d: %w", issue.Number, err)
	}

	log.Printf("Supervisor: Routed issue #%d - complexity: %v, tier: %v",
		issue.Number, complexity, tier)

	// Create task
	task := &taskqueue.Task{
		ID:          uuid.New().String(),
		IssueNumber: issue.Number,
		RepoOwner:   issue.RepoOwner,
		RepoName:    issue.RepoName,
		Title:       issue.Title,
		Description: issue.Body,
		Complexity:  complexity,
		Tier:        tier,
		Status:      taskqueue.StatusPending,
		WorkerID:    "",
		Attempts:    0,
		Metadata:    make(map[string]string),
	}

	// Add labels to metadata
	if len(issue.Labels) > 0 {
		task.Metadata["labels"] = fmt.Sprintf("%v", issue.Labels)
	}

	// Enqueue task
	if err := s.queue.Enqueue(ctx, task); err != nil {
		return fmt.Errorf("failed to enqueue task for issue #%d: %w", issue.Number, err)
	}

	log.Printf("Supervisor: Enqueued task %s for issue #%d", task.ID, issue.Number)
	return nil
}

// monitorStalledTasks finds tasks that have been in progress too long and releases them.
// A task is considered stalled if it has been Claimed or InProgress longer than the configured timeout.
func (s *Supervisor) monitorStalledTasks(ctx context.Context) error {
	timeout := time.Duration(s.config.TaskTimeoutMinutes) * time.Minute

	// Find all claimed or in-progress tasks
	claimedStatus := taskqueue.StatusClaimed
	inProgressStatus := taskqueue.StatusInProgress

	claimedFilter := &taskqueue.TaskFilter{Status: &claimedStatus}
	inProgressFilter := &taskqueue.TaskFilter{Status: &inProgressStatus}

	claimedTasks, err := s.queue.List(ctx, claimedFilter)
	if err != nil {
		return fmt.Errorf("failed to list claimed tasks: %w", err)
	}

	inProgressTasks, err := s.queue.List(ctx, inProgressFilter)
	if err != nil {
		return fmt.Errorf("failed to list in-progress tasks: %w", err)
	}

	// Combine both lists
	allActiveTasks := make([]*taskqueue.Task, 0, len(claimedTasks)+len(inProgressTasks))
	allActiveTasks = append(allActiveTasks, claimedTasks...)
	allActiveTasks = append(allActiveTasks, inProgressTasks...)

	// Check each task for stall
	stalledCount := 0
	for _, task := range allActiveTasks {
		if !task.IsStalled(timeout) {
			continue
		}

		log.Printf("Supervisor: Task %s (issue #%d) is stalled - claimed at %v, started at %v",
			task.ID, task.IssueNumber, task.ClaimedAt, task.StartedAt)

		// Check if max retries exceeded
		if task.Attempts >= s.config.MaxRetryAttempts {
			log.Printf("Supervisor: Task %s has exceeded max retry attempts (%d), marking as failed",
				task.ID, s.config.MaxRetryAttempts)

			task.Status = taskqueue.StatusFailed
			task.ErrorMsg = fmt.Sprintf("Task stalled after %d attempts", task.Attempts)
			task.CompletedAt = time.Now()

			if err := s.queue.Update(ctx, task); err != nil {
				log.Printf("Supervisor: Failed to update failed task %s: %v", task.ID, err)
			}
			continue
		}

		// Release task back to queue
		if err := s.queue.Release(ctx, task.ID); err != nil {
			log.Printf("Supervisor: Failed to release stalled task %s: %v", task.ID, err)
			continue
		}

		log.Printf("Supervisor: Released stalled task %s (attempt %d/%d)",
			task.ID, task.Attempts+1, s.config.MaxRetryAttempts)
		stalledCount++
	}

	if stalledCount > 0 {
		log.Printf("Supervisor: Released %d stalled tasks", stalledCount)
	}

	return nil
}

// Shutdown gracefully shuts down the supervisor.
// It signals the main loop to stop and waits for it to complete.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	log.Println("Supervisor: Initiating shutdown")

	// Signal shutdown
	close(s.shutdown)

	// Wait for main loop to finish or context timeout
	select {
	case <-s.done:
		log.Println("Supervisor: Shutdown complete")
		return nil
	case <-ctx.Done():
		log.Println("Supervisor: Shutdown timeout exceeded")
		return ctx.Err()
	}
}
