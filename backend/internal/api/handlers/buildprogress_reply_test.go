package handlers

import (
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
)

// The reply is kept in the progress store, not only returned from the POST,
// because the POST may never reach the client: the build runs on a context
// detached from the request, so a proxy timeout ends the response while the
// build carries on and saves. Without this the user sees a failure for a
// build that worked, and the only recovery is a page reload.
func TestBuildProgressCarriesTheFinishedReply(t *testing.T) {
	key := buildProgressKey("u1", "w1", "build-abc123")
	buildProgress.set(key, nodes.BuildProgress{Current: "Planning the next step"})

	if _, done := buildProgress.get(key); done {
		t.Fatal("a build in progress reported done")
	}
	if reply := buildProgress.reply(key); reply != "" {
		t.Fatalf("a build in progress has a reply already: %q", reply)
	}

	buildProgress.finish(key, "Built it. Test run answer: 42")

	p, done := buildProgress.get(key)
	if !done {
		t.Fatal("a finished build does not report done")
	}
	if p.Current != "" {
		t.Errorf("a finished build still shows a current step: %q", p.Current)
	}
	if got := buildProgress.reply(key); got != "Built it. Test run answer: 42" {
		t.Errorf("reply = %q", got)
	}
}

// An unknown build must not look finished, or the chat would settle a turn
// that never started.
func TestBuildProgressForAnUnknownBuildIsNotDone(t *testing.T) {
	key := buildProgressKey("u1", "w1", "never-existed")
	if _, done := buildProgress.get(key); done {
		t.Fatal("an unknown build reported done")
	}
	if reply := buildProgress.reply(key); reply != "" {
		t.Fatalf("an unknown build has a reply: %q", reply)
	}
}

// A build that ended with no answer is still done. An empty reply with
// done=true is a different state from still running, and the chat has to be
// able to tell them apart.
func TestBuildProgressCanFinishWithNoReply(t *testing.T) {
	key := buildProgressKey("u1", "w1", "build-empty1")
	buildProgress.set(key, nodes.BuildProgress{Current: "Reading your request"})
	buildProgress.finish(key, "")

	if _, done := buildProgress.get(key); !done {
		t.Fatal("a build that ended without an answer does not report done")
	}
	if reply := buildProgress.reply(key); reply != "" {
		t.Errorf("reply = %q, want empty", reply)
	}
}
