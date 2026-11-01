package world

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestOrbAMRExactSets(t *testing.T) {
	cases := []struct {
		name        string
		requiredACR string
		claimedACR  string
		methods     []string
		kind        string
	}{
		{"documented", OrbACR, OrbACR, []string{"pop", "iris"}, "world_proof"},
		{"documented_reversed", OrbACR, OrbACR, []string{"iris", "pop"}, "world_proof"},
		{"legacy_orb", OrbACR, OrbACR, []string{"pop"}, "world_proof"},
		{"passkey", OrbACR, OrbACR, []string{"user", "pop", "mfa"}, "passkey"},
		{"legacy_orb_face", OrbFaceACR, OrbFaceACR, []string{"face", "pop"}, "face_proof"},
		{"duplicate_pop", OrbACR, OrbACR, []string{"pop", "iris", "pop"}, ""},
		{"duplicate_iris", OrbACR, OrbACR, []string{"iris", "pop", "iris"}, ""},
		{"unknown_method", OrbACR, OrbACR, []string{"pop", "iris", "unknown"}, ""},
		{"additional_known_method", OrbACR, OrbACR, []string{"pop", "iris", "user"}, ""},
		{"missing_pop", OrbACR, OrbACR, []string{"iris"}, ""},
		{"empty_methods", OrbACR, OrbACR, []string{}, ""},
		{"null_methods", OrbACR, OrbACR, nil, ""},
		{"wrong_acr", OrbFaceACR, OrbFaceACR, []string{"pop", "iris"}, ""},
		{"unknown_acr", OrbACR, OrbACR + "-unknown", []string{"pop", "iris"}, ""},
		{"requested_class_mismatch", OrbACR, OrbFaceACR, []string{"face", "pop"}, ""},
		{"orb_rejects_face_methods", OrbACR, OrbACR, []string{"pop", "face"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			f.cfg.RequiredACR = tc.requiredACR
			f.payload = func(p map[string]any) {
				p["acr"] = tc.claimedACR
				p["amr"] = tc.methods
			}
			store := newFixtureStore()
			client := f.client(t, store)
			binding := fixtureBinding()
			attempt, query := beginFixture(t, f, client, binding)
			evidence, err := client.Callback(context.Background(), binding, query)
			completions := 0
			if tc.kind == "" {
				assertNoEvidence(t, evidence, err)
				if !errors.Is(err, ErrToken) {
					t.Fatal("unsupported method set did not fail token validation")
				}
			} else {
				completions = 1
				if err != nil {
					t.Fatal("documented or retained exact method set rejected")
				}
				if evidence.AttemptID != attempt.AttemptID || evidence.Binding != binding || evidence.AuthenticationKind != tc.kind ||
					evidence.ACR != tc.claimedACR || !slices.Equal(evidence.AMR, tc.methods) {
					t.Fatal("validated evidence changed its method set or binding")
				}
			}
			if store.completed != completions || f.exchanges != 1 {
				t.Fatal("unexpected completion or exchange count")
			}
			replay, replayErr := client.Callback(context.Background(), binding, query)
			assertNoEvidence(t, replay, replayErr)
			if !errors.Is(replayErr, ErrAttemptUnavailable) || f.exchanges != 1 || store.completed != completions {
				t.Fatal("method-set validation reopened a consumed attempt")
			}
		})
	}
}
