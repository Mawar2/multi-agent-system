// Package samgov provides a client for interacting with the SAM.gov Opportunities API.
//
// The client supports two modes:
// - Live mode: makes real HTTP requests to the SAM.gov API
// - Cached mode: uses pre-recorded fixtures for fast, deterministic testing
//
// This package will be fully implemented when the Hunter agent is built.
// This is Phase 0 scaffolding.
package samgov

import (
	"context"

	"github.com/Mawar2/Kaimi/internal/opportunity"
)

// Client provides methods for fetching federal contracting opportunities from SAM.gov.
type Client interface {
	// FetchByNAICS retrieves all active opportunities matching the given NAICS codes.
	// Returns a slice of opportunities, or an error if the API call fails.
	//
	// TODO(phase-0): Implement in Hunter agent ticket.
	FetchByNAICS(ctx context.Context, naicsCodes []string) ([]*opportunity.Opportunity, error)

	// FetchByID retrieves a single opportunity by its SAM.gov notice ID.
	// Returns the opportunity if found, or an error if not found or API call fails.
	//
	// TODO(phase-0): Implement in Hunter agent ticket.
	FetchByID(ctx context.Context, noticeID string) (*opportunity.Opportunity, error)
}

// Config holds configuration for the SAM.gov API client.
type Config struct {
	// APIKey is the SAM.gov API key for authentication.
	// Required for live mode.
	APIKey string

	// BaseURL is the SAM.gov API base URL.
	// Defaults to the production API if empty.
	BaseURL string

	// UseCached indicates whether to use cached fixtures instead of live API.
	// When true, the client reads from test/fixtures/ instead of making HTTP requests.
	UseCached bool
}

// TODO(phase-0): Implement NewClient(config Config) (Client, error) in Hunter ticket.
// TODO(phase-0): Implement live HTTP client.
// TODO(phase-0): Implement cached fixture client for testing.
