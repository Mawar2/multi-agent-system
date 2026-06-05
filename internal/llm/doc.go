// Package llm defines the LLM backend abstraction for the multi-agent task system.
//
// Architecture:
//
// The multi-agent system coordinates work across multiple LLM backends (Claude Code CLI,
// Gemini via Antigravity, etc.). This package abstracts the common interface so agents
// can be swapped without changing worker logic.
//
// Backends:
//
//   - ClaudeCodeBackend: Spawns local Claude Code CLI agents for complex tasks (TierClaude).
//     Phase 1 implementation wraps the Task tool; actual process spawning added in Phase 2.
//
//   - (Future) AntigravityBackend: Routes tasks to Gemini Flash/Pro via Gemini API.
//     Phase 2+: Gemini Flash for simple tasks, Gemini Pro for moderate tasks.
//
//   - (Future) VertexAIBackend: Direct Vertex AI API integration for enterprise deployment.
//     Phase 3+: Used when moving beyond local Claude Code to cloud LLM services.
//
// Usage:
//
// Create a backend and execute prompts:
//
//	backend := NewClaudeCodeBackend()
//	result, err := backend.Execute(ctx, "Implement feature X", "claude-sonnet-4.5")
//	if err != nil {
//		// Handle error
//	}
//	// result contains agent output: logs, branch name, PR number, etc.
//
// Worker Usage:
//
// Workers in the task queue system use backends to execute claimed tasks:
//
//	task := queue.Claim()
//	backend := selectBackendForTier(task.Tier)
//	result, err := backend.Execute(ctx, buildPrompt(task), selectModel(task.Complexity))
//
// Phase 1 Implementation Notes:
//
// ClaudeCodeBackend is currently a Phase 1 placeholder. Key TODOs:
//
// 1. CLI Process Spawning: Implement actual process fork/exec to spawn claude agent
// 2. Task Tool Integration: Use Task tool (via MCP or direct invocation) to spawn workers
// 3. Result Parsing: Extract branch name, PR number, logs from agent output
// 4. Error Handling: Map process errors and agent failures to LLM backend errors
// 5. Resource Limits: Add CPU/memory constraints on spawned processes (Phase 2)
// 6. Retry Logic: Implement exponential backoff for transient failures (Phase 2)
// 7. Fallback Routing: Route to Antigravity if CLI unavailable (Phase 2)
//
// The interface is stable; implementations will change as the platform evolves.
package llm
