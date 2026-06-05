package orchestrator

import (
	"testing"
	"time"
)

func TestFallbackRouter_PrimaryAvailable(t *testing.T) {
	ma := NewModelAvailability()
	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())

	tier, event := router.GetAvailableTier(TierGeminiFlash)
	if tier != TierGeminiFlash {
		t.Errorf("tier = %v, want %v", tier, TierGeminiFlash)
	}
	if event != nil {
		t.Error("no failover event expected when primary is available")
	}
}

func TestFallbackRouter_FailoverToFirstFallback(t *testing.T) {
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("gemini-flash", time.Hour)

	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())
	tier, event := router.GetAvailableTier(TierGeminiFlash)

	if tier != TierGeminiPro {
		t.Errorf("tier = %v, want %v", tier, TierGeminiPro)
	}
	if event == nil {
		t.Fatal("expected FailoverEvent when failover occurs")
	}
	if event.OriginalTier != TierGeminiFlash {
		t.Errorf("event.OriginalTier = %v, want %v", event.OriginalTier, TierGeminiFlash)
	}
	if event.FallbackTier != TierGeminiPro {
		t.Errorf("event.FallbackTier = %v, want %v", event.FallbackTier, TierGeminiPro)
	}
	if event.Reason == "" {
		t.Error("FailoverEvent.Reason should not be empty")
	}
}

func TestFallbackRouter_FailoverToSecondFallback(t *testing.T) {
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("gemini-flash", time.Hour)
	ma.MarkQuotaExhausted("gemini-pro", time.Hour)

	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())
	tier, event := router.GetAvailableTier(TierGeminiFlash)

	if tier != TierClaude {
		t.Errorf("tier = %v, want %v", tier, TierClaude)
	}
	if event == nil {
		t.Fatal("expected FailoverEvent when failover occurs")
	}
	if event.FallbackTier != TierClaude {
		t.Errorf("event.FallbackTier = %v, want %v", event.FallbackTier, TierClaude)
	}
}

func TestFallbackRouter_AllExhaustedReturnsPrimary(t *testing.T) {
	ma := NewModelAvailability()
	// Exhaust every model in the Flash fallback chain.
	ma.MarkQuotaExhausted("gemini-flash", time.Hour)
	ma.MarkQuotaExhausted("gemini-pro", time.Hour)
	ma.MarkQuotaExhausted("claude-sonnet", time.Hour)

	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())
	tier, event := router.GetAvailableTier(TierGeminiFlash)

	// Primary is returned so the caller can queue the task for later.
	if tier != TierGeminiFlash {
		t.Errorf("tier = %v, want primary %v when all exhausted", tier, TierGeminiFlash)
	}
	if event != nil {
		t.Error("no FailoverEvent expected when all models exhausted (task held for retry)")
	}
}

func TestFallbackRouter_QuotaResetRestoresPrimary(t *testing.T) {
	ma := NewModelAvailability()
	// Negative duration puts the RetryAfter in the past.
	ma.MarkQuotaExhausted("gemini-flash", -1*time.Millisecond)

	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())
	tier, event := router.GetAvailableTier(TierGeminiFlash)

	if tier != TierGeminiFlash {
		t.Errorf("tier = %v, want primary %v after quota reset", tier, TierGeminiFlash)
	}
	if event != nil {
		t.Error("no failover expected once quota has reset")
	}
}

func TestFallbackRouter_UnknownTierAssumedAvailable(t *testing.T) {
	ma := NewModelAvailability()
	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())

	const unknownTier ModelTier = "unknown-tier"
	tier, event := router.GetAvailableTier(unknownTier)
	if tier != unknownTier {
		t.Errorf("tier = %v, want %v", tier, unknownTier)
	}
	if event != nil {
		t.Error("no failover expected for unknown tier (assumed available)")
	}
}

func TestFallbackRouter_MediumTaskChain(t *testing.T) {
	// Medium task: Pro → Claude → Flash
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("gemini-pro", time.Hour)

	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())
	tier, event := router.GetAvailableTier(TierGeminiPro)

	if tier != TierClaude {
		t.Errorf("medium task failover: tier = %v, want %v", tier, TierClaude)
	}
	if event == nil {
		t.Fatal("expected FailoverEvent for medium task failover")
	}
}

func TestFallbackRouter_ComplexTaskChain(t *testing.T) {
	// Complex task: Claude → Opus → Pro
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("claude-sonnet", time.Hour)

	router := NewFallbackRouter(ma, DefaultFallbackChains(), DefaultTierModels())
	tier, event := router.GetAvailableTier(TierClaude)

	if tier != TierClaudeOpus {
		t.Errorf("complex task failover: tier = %v, want %v", tier, TierClaudeOpus)
	}
	if event == nil {
		t.Fatal("expected FailoverEvent for complex task failover")
	}
}
