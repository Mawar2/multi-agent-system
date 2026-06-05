// Package orchestrator provides the supervisor and routing logic for the multi-agent system.
//
// The orchestrator coordinates the multi-agent workflow by:
//   - Polling GitHub Issues from configured repositories
//   - Classifying issues by complexity (simple, medium, complex)
//   - Routing issues to appropriate worker tiers (Gemini Flash, Gemini Pro, Claude)
//   - Managing task lifecycle through the queue
//   - Monitoring for stalled tasks and enforcing retry policies
//
// The package includes three main components:
//
//   - Supervisor: The main coordinator that polls issues, routes them to workers, and monitors task health
//   - Router: Classifies issue complexity and determines appropriate worker tier assignment
//   - Config: Configuration management for projects, worker tiers, and supervisor behavior
//
// The supervisor operates in a continuous loop, polling at configurable intervals,
// and ensures fault tolerance through automatic stall detection and retry mechanisms.
package orchestrator
