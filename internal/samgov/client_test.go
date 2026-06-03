package samgov

import (
	"testing"
)

// TestConfig_Defaults verifies that Config has sensible zero values.
func TestConfig_Defaults(t *testing.T) {
	var cfg Config

	if cfg.APIKey != "" {
		t.Errorf("Expected empty APIKey, got %q", cfg.APIKey)
	}
	if cfg.BaseURL != "" {
		t.Errorf("Expected empty BaseURL, got %q", cfg.BaseURL)
	}
	if cfg.UseCached {
		t.Error("Expected UseCached to be false")
	}
}

// TestConfig_CachedMode verifies that cached mode can be configured.
func TestConfig_CachedMode(t *testing.T) {
	cfg := Config{
		UseCached: true,
	}

	if !cfg.UseCached {
		t.Error("Expected UseCached to be true")
	}
}

// TODO(phase-0): Add client implementation tests in Hunter ticket.
// - Test FetchByNAICS with cached fixtures
// - Test FetchByID with cached fixtures
// - Test error handling for missing fixtures
// - Test API response parsing (once live client is implemented)
