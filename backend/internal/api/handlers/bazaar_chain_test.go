package handlers

import (
	"testing"

	"github.com/agentmesh/backend/internal/chain"
)

func TestDefaultBazaarBaseURLFollowsActiveProfile(t *testing.T) {
	t.Cleanup(func() { chain.Use("") })
	if defaultBazaarBaseURL() != chain.Whitechain.FacilitatorURL {
		t.Fatalf("default facilitator = %s", defaultBazaarBaseURL())
	}
	chain.Use("arc")
	if defaultBazaarBaseURL() != chain.Arc.FacilitatorURL {
		t.Fatalf("arc facilitator = %s, want %s", defaultBazaarBaseURL(), chain.Arc.FacilitatorURL)
	}
}
