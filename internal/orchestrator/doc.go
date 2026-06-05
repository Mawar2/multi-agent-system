// Package orchestrator provides components for routing tasks across model tiers
// and recovering automatically when a model exhausts its quota.
//
// The two core types are:
//   - ModelAvailability: tracks whether each model is quota-exhausted, with
//     automatic recovery when the retry window elapses.
//   - FallbackRouter: selects the best available tier for a task by walking
//     a configured fallback chain (e.g. Flash → Pro → Claude).
//
// Use DetectQuotaError to classify an error returned by a model call, then
// call ModelAvailability.MarkQuotaExhausted to suppress that model until quota
// resets.
package orchestrator
