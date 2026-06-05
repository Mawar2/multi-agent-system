// Package worker provides autonomous agent workers that claim and complete tasks from the queue.
//
// Workers implement the Worker interface and autonomously:
//   - Claim tasks from the queue based on their tier (gemini-flash, gemini-pro, claude)
//   - Parse project conventions (CLAUDE.md, CONVENTIONS.md)
//   - Execute tasks using LLM backends (Claude Code CLI, Antigravity, etc.)
//   - Create feature branches following project patterns
//   - Implement solutions using TDD when required
//   - Run tests and linters
//   - Create pull requests with proper formatting
//   - Report health and completion statistics
//
// # Implementation
//
// The ClaudeCodeWorker is the primary implementation in Phase 1, using Claude Code CLI
// as the backend. Future phases may add GeminiWorker for Antigravity integration.
//
// # Usage Example
//
//	// Create worker
//	backend := llm.NewClaudeCodeBackend()
//	queue := taskqueue.NewJSONQueue("/path/to/queue.json")
//	worker := worker.NewClaudeCodeWorker(
//		"claude-worker-1",
//		taskqueue.TierClaude,
//		queue,
//		backend,
//		"/path/to/projects",
//	)
//
//	// Worker loop
//	for {
//		// Claim task
//		task, err := worker.Claim(ctx)
//		if err != nil {
//			log.Printf("Failed to claim: %v", err)
//			continue
//		}
//		if task == nil {
//			time.Sleep(30 * time.Second)
//			continue
//		}
//
//		// Execute task
//		result, err := worker.Execute(ctx, task)
//		if err != nil {
//			log.Printf("Failed to execute: %v", err)
//			worker.Release(ctx, task.ID)
//			continue
//		}
//
//		if result.Success {
//			log.Printf("PR created: #%d on branch %s", result.PRNumber, result.BranchName)
//		} else {
//			log.Printf("Task failed: %s", result.ErrorMsg)
//		}
//	}
//
// # Convention-Driven Approach
//
// Workers read project conventions from CLAUDE.md and CONVENTIONS.md to understand:
//   - Branch naming patterns (e.g., "feature/KAI-{ticket}-{summary}")
//   - Commit message formats (e.g., "{ticket}_{description}")
//   - Forbidden files (e.g., utils.go, helpers.go)
//   - Test commands (e.g., "make test")
//   - Lint commands (e.g., "make lint")
//   - TDD requirements
//
// This allows workers to adapt to any project's conventions without hardcoding rules.
//
// # Phase 1 Scope
//
// Phase 1 workers:
//   - Use Claude Code CLI as backend (local, free)
//   - Claim tasks from JSON-backed queue
//   - Parse conventions from local project directories
//   - Create PRs but never auto-merge (human approval required)
//   - Report health and statistics
//
// # Future Phases
//
// Phase 2+:
//   - GeminiWorker using Antigravity backend (Gemini via subscription)
//   - Firestore-backed queue for distributed workers
//   - Advanced health monitoring and metrics
//   - Worker pool management with auto-scaling
package worker
