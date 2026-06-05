package orchestrator

// ModelTier identifies a named model tier used for task routing.
type ModelTier string

const (
	// TierGeminiFlash is the cheapest/fastest tier — used for simple tasks.
	TierGeminiFlash ModelTier = "gemini-flash"
	// TierGeminiPro is the capable mid-tier — used for medium tasks.
	TierGeminiPro ModelTier = "gemini-pro"
	// TierClaude is the Claude Sonnet tier — used for complex tasks.
	TierClaude ModelTier = "claude"
	// TierClaudeOpus is the most powerful Claude tier — last-resort fallback for complex tasks.
	TierClaudeOpus ModelTier = "claude-opus"
)

// FailoverEvent records a single failover for cost tracking and audit logging.
// Callers should log every non-nil FailoverEvent returned by GetAvailableTier.
type FailoverEvent struct {
	OriginalTier ModelTier
	FallbackTier ModelTier
	Reason       string
}

// FallbackRouter routes tasks to an alternate model tier when the primary is
// quota-exhausted. It walks the configured fallback chain and returns the first
// tier whose backing model is available.
type FallbackRouter struct {
	availability   *ModelAvailability
	fallbackChains map[ModelTier][]ModelTier
	tierToModel    map[ModelTier]string
}

// NewFallbackRouter creates a router.
//
// chains maps each tier to its ordered list of fallback tiers (first = most preferred).
// models maps each tier to the model name used by ModelAvailability.
func NewFallbackRouter(
	availability *ModelAvailability,
	chains map[ModelTier][]ModelTier,
	models map[ModelTier]string,
) *FallbackRouter {
	return &FallbackRouter{
		availability:   availability,
		fallbackChains: chains,
		tierToModel:    models,
	}
}

// DefaultFallbackChains returns the fallback chains from the design spec:
//   - Simple  (Flash):  Flash → Pro → Claude
//   - Medium  (Pro):    Pro → Claude → Flash
//   - Complex (Claude): Claude → Opus → Pro
func DefaultFallbackChains() map[ModelTier][]ModelTier {
	return map[ModelTier][]ModelTier{
		TierGeminiFlash: {TierGeminiPro, TierClaude},
		TierGeminiPro:   {TierClaude, TierGeminiFlash},
		TierClaude:      {TierClaudeOpus, TierGeminiPro},
		TierClaudeOpus:  {TierClaude, TierGeminiPro},
	}
}

// DefaultTierModels returns the canonical tier→model-name mapping.
func DefaultTierModels() map[ModelTier]string {
	return map[ModelTier]string{
		TierGeminiFlash: "gemini-flash",
		TierGeminiPro:   "gemini-pro",
		TierClaude:      "claude-sonnet",
		TierClaudeOpus:  "claude-opus",
	}
}

// GetAvailableTier returns the best available tier for primary.
//
// If the primary tier is available, it is returned immediately with no event.
// Otherwise the fallback chain is walked and the first available tier is
// returned with a FailoverEvent describing the routing decision.
//
// If every model in the chain is exhausted, primary is returned unchanged with
// no event — the caller should back off and retry rather than discarding the
// task (preserves the "zero tasks fail" guarantee).
func (r *FallbackRouter) GetAvailableTier(primary ModelTier) (ModelTier, *FailoverEvent) {
	if r.isTierAvailable(primary) {
		return primary, nil
	}

	for _, fallback := range r.fallbackChains[primary] {
		if r.isTierAvailable(fallback) {
			return fallback, &FailoverEvent{
				OriginalTier: primary,
				FallbackTier: fallback,
				Reason:       "quota exhausted",
			}
		}
	}

	// All tiers exhausted — return primary so the caller queues the task for
	// later rather than dropping it.
	return primary, nil
}

// isTierAvailable returns true if the model backing tier is currently available.
// Tiers with no registered model mapping are assumed available so that unknown
// tiers never silently block task routing.
func (r *FallbackRouter) isTierAvailable(tier ModelTier) bool {
	model, ok := r.tierToModel[tier]
	if !ok {
		return true
	}
	return r.availability.IsAvailable(model)
}
