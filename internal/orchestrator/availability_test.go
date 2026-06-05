package orchestrator

import (
	"errors"
	"testing"
	"time"
)

func TestModelAvailability_UnknownModelIsAvailable(t *testing.T) {
	ma := NewModelAvailability()
	if !ma.IsAvailable("unknown-model") {
		t.Error("unknown model should be available (fail-open)")
	}
}

func TestModelAvailability_MarkQuotaExhausted(t *testing.T) {
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("gemini-flash", time.Hour)

	if ma.IsAvailable("gemini-flash") {
		t.Error("model should be unavailable after MarkQuotaExhausted")
	}
}

func TestModelAvailability_RetryAfterExpiry(t *testing.T) {
	ma := NewModelAvailability()
	// Negative duration means the retry window is already in the past.
	ma.MarkQuotaExhausted("gemini-flash", -1*time.Millisecond)

	if !ma.IsAvailable("gemini-flash") {
		t.Error("model should be available again after RetryAfter has elapsed")
	}
}

func TestModelAvailability_MarkAvailable(t *testing.T) {
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("gemini-pro", time.Hour)
	ma.MarkAvailable("gemini-pro")

	if !ma.IsAvailable("gemini-pro") {
		t.Error("model should be available after explicit MarkAvailable call")
	}
}

func TestModelAvailability_StatusNilForUnknown(t *testing.T) {
	ma := NewModelAvailability()
	if ma.Status("never-registered") != nil {
		t.Error("Status should be nil for a model that has never been registered")
	}
}

func TestModelAvailability_StatusReturnsCopy(t *testing.T) {
	ma := NewModelAvailability()
	ma.MarkQuotaExhausted("gemini-flash", time.Hour)

	s := ma.Status("gemini-flash")
	if s == nil {
		t.Fatal("Status should not be nil after MarkQuotaExhausted")
	}
	// Mutating the returned value must not affect internal state.
	s.QuotaExhausted = false
	if !ma.status["gemini-flash"].QuotaExhausted {
		t.Error("Status returned a reference, not a copy — mutation leaked into tracker")
	}
}

func TestModelAvailability_ConcurrentAccess(t *testing.T) {
	ma := NewModelAvailability()
	done := make(chan struct{})

	go func() {
		for range 100 {
			ma.MarkQuotaExhausted("gemini-flash", time.Millisecond)
		}
		close(done)
	}()

	for range 100 {
		ma.IsAvailable("gemini-flash")
	}
	<-done
}

// --- DetectQuotaError ---

func TestDetectQuotaError_NilError(t *testing.T) {
	isQuota, _ := DetectQuotaError(nil)
	if isQuota {
		t.Error("nil error should not be a quota error")
	}
}

func TestDetectQuotaError_GeminiQuotaExceeded(t *testing.T) {
	err := errors.New("RESOURCE_EXHAUSTED: quota exceeded for project")
	isQuota, retryAfter := DetectQuotaError(err)
	if !isQuota {
		t.Error("should detect 'quota exceeded' as a quota error")
	}
	if retryAfter != time.Hour {
		t.Errorf("retryAfter = %v, want 1h", retryAfter)
	}
}

func TestDetectQuotaError_GeminiResourceExhausted(t *testing.T) {
	err := errors.New("resource exhausted: you have exceeded your quota")
	isQuota, retryAfter := DetectQuotaError(err)
	if !isQuota {
		t.Error("should detect 'resource exhausted' as a quota error")
	}
	if retryAfter != time.Hour {
		t.Errorf("retryAfter = %v, want 1h", retryAfter)
	}
}

func TestDetectQuotaError_Claude429(t *testing.T) {
	err := errors.New("rate limit exceeded: 429 too many requests")
	isQuota, retryAfter := DetectQuotaError(err)
	if !isQuota {
		t.Error("should detect '429' rate limit as a quota error")
	}
	if retryAfter != 5*time.Minute {
		t.Errorf("retryAfter = %v, want 5m", retryAfter)
	}
}

func TestDetectQuotaError_AnthropicOverloaded(t *testing.T) {
	err := errors.New("overloaded: service temporarily unavailable")
	isQuota, retryAfter := DetectQuotaError(err)
	if !isQuota {
		t.Error("should detect 'overloaded' as a quota error")
	}
	if retryAfter != 2*time.Minute {
		t.Errorf("retryAfter = %v, want 2m", retryAfter)
	}
}

func TestDetectQuotaError_NonQuotaError(t *testing.T) {
	err := errors.New("connection refused: server not running")
	isQuota, retryAfter := DetectQuotaError(err)
	if isQuota {
		t.Error("connection error should not be a quota error")
	}
	if retryAfter != 0 {
		t.Errorf("retryAfter should be 0 for non-quota error, got %v", retryAfter)
	}
}
