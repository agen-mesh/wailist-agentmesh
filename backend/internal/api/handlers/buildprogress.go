package handlers

import (
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/respond"
)

// Build progress for the chat: while BuildWorkflow runs, the chat polls
// BuildWorkflowProgress about once a second and shows each step the builder
// takes ("Searched the web for ...", "Added HTTP Request ...").
//
// Polling, not a stream, on purpose. The builder's POST goes through the
// frontend's /api rewrite proxy, and that proxy does not reliably hold a
// long-lived text/event-stream open (see useRunTranscript.ts, where run logs
// bypass it for exactly that reason). Short polls are the same kind of
// request every other API call already makes through it.
//
// In memory, per process: progress is only useful for the minute or two a
// build runs. With several backend instances, a poll that lands on another
// one simply sees no steps -- the build itself is unaffected.

const buildProgressTTL = 15 * time.Minute

// buildIDPattern bounds a client-chosen build id to something that is safe
// to use as a map key and cheap to hold.
var buildIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type buildProgressEntry struct {
	progress nodes.BuildProgress
	done     bool
	updated  time.Time
}

type buildProgressStore struct {
	mu      sync.Mutex
	entries map[string]*buildProgressEntry
}

var buildProgress = &buildProgressStore{entries: map[string]*buildProgressEntry{}}

// buildProgressKey scopes an entry to the user and workflow as well as the
// build id, so a build id guessed or reused by anyone else finds nothing.
func buildProgressKey(userID, workflowID, buildID string) string {
	return userID + "\x00" + workflowID + "\x00" + buildID
}

func (s *buildProgressStore) set(key string, p nodes.BuildProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, e := range s.entries {
		if now.Sub(e.updated) > buildProgressTTL {
			delete(s.entries, k)
		}
	}
	e := s.entries[key]
	if e == nil {
		e = &buildProgressEntry{}
		s.entries[key] = e
	}
	e.progress, e.updated = p, now
}

func (s *buildProgressStore) finish(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[key]
	if e == nil {
		e = &buildProgressEntry{}
		s.entries[key] = e
	}
	e.done, e.progress.Current, e.updated = true, "", time.Now()
}

func (s *buildProgressStore) get(key string) (nodes.BuildProgress, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[key]; e != nil {
		return e.progress, e.done
	}
	return nodes.BuildProgress{}, false
}

// BuildWorkflowProgress returns the steps a build has taken so far, what it
// is doing now, and whether it has finished. A build that has not started
// yet, or belongs to someone else, reads as no steps and not done.
func (d *Deps) BuildWorkflowProgress(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	buildID := r.URL.Query().Get("buildId")
	if !buildIDPattern.MatchString(buildID) {
		respond.Error(w, http.StatusBadRequest, "invalid buildId")
		return
	}
	p, done := buildProgress.get(buildProgressKey(userID, chi.URLParam(r, "id"), buildID))
	steps := p.Steps
	if steps == nil {
		steps = []nodes.BuildStep{}
	}
	respond.JSON(w, http.StatusOK, map[string]any{"steps": steps, "current": p.Current, "done": done})
}
