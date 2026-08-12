package handlers

import (
	"sync"
	"testing"
	"time"
)

func TestCheckRunCooldownAllowsFirstTrigger(t *testing.T) {
	d := &Deps{}
	now := time.Unix(1_700_000_000, 0)

	retryAfter, blocked := d.checkRunCooldown("wf-1", now)
	if blocked {
		t.Fatalf("want first trigger allowed, got blocked with retryAfter=%v", retryAfter)
	}
}

func TestCheckRunCooldownBlocksImmediateRetrigger(t *testing.T) {
	d := &Deps{}
	now := time.Unix(1_700_000_000, 0)

	if _, blocked := d.checkRunCooldown("wf-1", now); blocked {
		t.Fatal("want first trigger allowed")
	}

	retryAfter, blocked := d.checkRunCooldown("wf-1", now.Add(1*time.Second))
	if !blocked {
		t.Fatal("want a trigger 1s after the last one to be blocked (cooldown is 5s)")
	}
	if want := 4 * time.Second; retryAfter != want {
		t.Fatalf("want retryAfter=%v, got %v", want, retryAfter)
	}
}

func TestCheckRunCooldownAllowsAfterCooldownElapses(t *testing.T) {
	d := &Deps{}
	now := time.Unix(1_700_000_000, 0)

	if _, blocked := d.checkRunCooldown("wf-1", now); blocked {
		t.Fatal("want first trigger allowed")
	}
	if _, blocked := d.checkRunCooldown("wf-1", now.Add(runTriggerCooldown)); blocked {
		t.Fatal("want a trigger exactly at the cooldown boundary to be allowed")
	}
}

func TestCheckRunCooldownIsPerWorkflow(t *testing.T) {
	d := &Deps{}
	now := time.Unix(1_700_000_000, 0)

	if _, blocked := d.checkRunCooldown("wf-1", now); blocked {
		t.Fatal("want first trigger of wf-1 allowed")
	}
	// A different workflow, triggered a moment later, must not be blocked by
	// wf-1's own cooldown -- this is a per-workflow deterrent, not a global
	// one.
	if _, blocked := d.checkRunCooldown("wf-2", now.Add(1*time.Second)); blocked {
		t.Fatal("want wf-2's own first trigger allowed regardless of wf-1's cooldown")
	}
}

// TestCheckRunCooldownConcurrentBurstAllowsExactlyOne guards the atomicity
// this exists for in the first place: a burst of concurrent requests for
// the same workflow (the actual "bot hammering the trigger endpoint" case
// this feature is meant to blunt) must only ever let ONE through, not a
// whole racing batch that each read "no recent run" before any of them
// recorded their own attempt.
func TestCheckRunCooldownConcurrentBurstAllowsExactlyOne(t *testing.T) {
	d := &Deps{}
	now := time.Unix(1_700_000_000, 0)

	const burst = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0

	wg.Add(burst)
	for i := 0; i < burst; i++ {
		go func() {
			defer wg.Done()
			if _, blocked := d.checkRunCooldown("wf-1", now); !blocked {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 1 {
		t.Fatalf("want exactly 1 of %d concurrent triggers allowed, got %d", burst, allowed)
	}
}
