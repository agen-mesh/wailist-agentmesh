package engine_test

import (
	"strconv"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
)

// Message() used to pick "most recent output" by iterating rc.outputs, a Go
// map whose iteration order is randomized. Run enough distinct keys that a
// map-order implementation would almost certainly disagree with insertion
// order at least once, to catch a regression back to that approach.
func TestRunContext_MessageReturnsMostRecentlySetInInsertionOrder(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"trigger input"`))
	if got := rc.Message(); got != "trigger input" {
		t.Fatalf("want trigger input before any Set, got %q", got)
	}

	ids := []string{"n1", "n2", "n3", "n4", "n5", "n6", "n7", "n8", "n9", "n10"}
	for i, id := range ids {
		rc.Set(id, i)
		want := strconv.Itoa(i)
		if got := rc.Message(); got != want {
			t.Fatalf("after Set(%s, %d): want Message() == %q, got %q", id, i, want, got)
		}
	}
}

func TestRunContext_MessageReflectsOverwriteNotReinsertion(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("a", "first")
	rc.Set("b", "second")
	// Overwriting "a" should not move it to the end of insertion order --
	// "b" is still the most recently *inserted* key, so Message() should
	// keep reflecting "b"'s (unchanged) value, not "a"'s new one.
	rc.Set("a", "first-updated")
	if got := rc.Message(); got != "second" {
		t.Fatalf("want overwrite to not reorder, got %q", got)
	}
}

// LastOutput() exists specifically so a connector's message template can
// pick one field out of a structured output -- unlike Message(), it must
// return the raw value, not a pre-stringified one.
func TestRunContext_LastOutputReturnsRawValueNotStringified(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", map[string]any{"extract": "hello", "count": 3})

	got, ok := rc.LastOutput().(map[string]any)
	if !ok {
		t.Fatalf("want map[string]any, got %T: %v", rc.LastOutput(), rc.LastOutput())
	}
	if got["extract"] != "hello" || got["count"] != 3 {
		t.Errorf("want raw map preserved, got %v", got)
	}
}

func TestRunContext_LastOutputBeforeAnySetReturnsInput(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`{"city":"NYC"}`))
	got, ok := rc.LastOutput().(map[string]any)
	if !ok || got["city"] != "NYC" {
		t.Fatalf("want raw trigger input map, got %T: %v", rc.LastOutput(), rc.LastOutput())
	}
}
