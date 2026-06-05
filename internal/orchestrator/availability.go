package orchestrator

import (
	"strings"
	"sync"
	"time"
)

// ModelStatus holds a snapshot of a single model's availability state.
type ModelStatus struct {
	Available      bool
	QuotaExhausted bool
	LastErrorTime  time.Time
	RetryAfter     time.Time
}

// ModelAvailability tracks which models are quota-exhausted and when they
// recover. All methods are safe for concurrent use.
type ModelAvailability struct {
	mu     sync.RWMutex
	status map[string]*ModelStatus
}

// NewModelAvailability creates a tracker with no models pre-marked unavailable.
func NewModelAvailability() *ModelAvailability {
	return &ModelAvailability{
		status: make(map[string]*ModelStatus),
	}
}

// IsAvailable returns true if model can currently accept requests.
// Unknown models are assumed available (fail-open ensures tasks are never
// silently dropped just because a model name was not pre-registered).
func (m *ModelAvailability) IsAvailable(model string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := m.status[model]
	if s == nil {
		return true
	}

	// If the retry window has passed, treat the model as available again.
	if s.QuotaExhausted && time.Now().After(s.RetryAfter) {
		return true
	}

	return s.Available && !s.QuotaExhausted
}

// MarkQuotaExhausted records that model has exhausted its quota.
// The model will be treated as unavailable until retryAfter has elapsed.
func (m *ModelAvailability) MarkQuotaExhausted(model string, retryAfter time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status[model] == nil {
		m.status[model] = &ModelStatus{}
	}
	s := m.status[model]
	s.Available = false
	s.QuotaExhausted = true
	s.LastErrorTime = time.Now()
	s.RetryAfter = time.Now().Add(retryAfter)
}

// MarkAvailable clears any quota-exhausted state for model, making it
// immediately available again (e.g. after an explicit quota reset signal).
func (m *ModelAvailability) MarkAvailable(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.status[model] = &ModelStatus{Available: true}
}

// Status returns a copy of the named model's current status.
// Returns nil if the model has never been registered.
func (m *ModelAvailability) Status(model string) *ModelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := m.status[model]
	if s == nil {
		return nil
	}
	snapshot := *s
	return &snapshot
}

// DetectQuotaError inspects err and returns (isQuota, retryAfter).
// Returns (false, 0) when err is nil or is not a recognised quota/rate-limit
// error. Callers should pass the returned duration to MarkQuotaExhausted.
func DetectQuotaError(err error) (bool, time.Duration) {
	if err == nil {
		return false, 0
	}

	lower := strings.ToLower(err.Error())

	// Gemini: RESOURCE_EXHAUSTED / quota exceeded
	if strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "resource exhausted") {
		return true, time.Hour
	}

	// Claude / generic HTTP 429
	if strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "429") {
		return true, 5 * time.Minute
	}

	// Anthropic overloaded
	if strings.Contains(lower, "overloaded") {
		return true, 2 * time.Minute
	}

	return false, 0
}
