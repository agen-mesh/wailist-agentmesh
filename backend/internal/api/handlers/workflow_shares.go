package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// Sharing a workflow with somebody who is not you.
//
// Two ways in, one snapshot behind both. POST /workflows/{id}/share freezes a
// sanitised copy and returns a token; the link /s/{token} reads it back
// without a session, and POST /shares/{token}/import plants it in the
// caller's own workspace. POST /workflows/import is the offline half: the
// same graph pasted as a code, for a recipient who was handed text rather
// than a URL.
//
// Everything that leaves or enters goes through SanitizeGraphForShare. See
// share_sanitize.go for what that means and why it is an allowlist.

const (
	// A graph past these is not a workflow somebody built, it is a payload.
	// PUT /workflows/{id} has no limits at all -- deliberately not changed
	// here, since tightening an endpoint every existing canvas already uses
	// is a separate decision with a separate blast radius -- but a public
	// route and an endpoint that accepts a stranger's JSON both need their
	// own floor under them.
	maxShareNodes = 300
	maxShareEdges = 600
	// Mirrors the CHECK on workflow_shares.graph. Measured after sanitising,
	// which is the only size that matters: uploaded file bytes are the one
	// thing that makes a graph large, and they are gone by then.
	maxShareGraphBytes = 1 << 20
	// The body cap on both import routes. Larger than maxShareGraphBytes
	// because a pasted graph has not been sanitised yet and may still be
	// carrying the file bytes that sanitising is about to drop.
	maxImportBodyBytes = 2 << 20

	// An upper bound on the expiry a caller may ask for. "Never" stays the
	// default; this only stops a nonsense value reaching the column.
	maxShareExpiryDays = 365

	// Mirrors the CHECK on workflow_shares.description. Only the import path
	// needs it in Go: every other description reaching the column comes from
	// a workflow the caller already owns, while a pasted code's is a
	// stranger's string.
	maxShareDescriptionChars = 2000
)

// ShareImportRequirements is what the RECIPIENT still has to supply, worked
// out from the sanitised graph they are being given.
//
// Deliberately not the same thing as ShareRedactions, which counts what was
// taken off the sharer's copy. The two answer different questions for
// different people, and echoing the sharer's counts at a recipient would be
// both less useful and slightly revealing -- "the workflow you are importing
// had 3 API keys in it" is not a recipient's business.
//
// Derived rather than stored, so it stays correct for a link made before this
// code existed.
type ShareImportRequirements struct {
	// Provider nodes running on their own key rather than the platform's.
	APIKeys int `json:"apiKeys"`
	// File params with no bytes: the recipient uploads their own.
	Files int `json:"files"`
	// Google nodes, which need the recipient's own connected account.
	ConnectedAccounts int `json:"connectedAccounts"`
	// Connector providers the recipient has to reconnect, named and sorted --
	// "slack", "jira". Named rather than counted because "reconnect Slack and
	// Jira" is something a person can act on where "2 connectors" is not, and
	// because the names are already plain in the graph they are about to
	// import. Distinct: two Slack nodes are still one account to connect.
	Connectors []string `json:"connectors"`
}

// Any reports whether the recipient has anything to do before running it.
func (r ShareImportRequirements) Any() bool {
	return r.APIKeys+r.Files+r.ConnectedAccounts+len(r.Connectors) > 0
}

// RequirementsForImport inspects a sanitised graph and says what is missing.
//
// A method rather than a free function because the connector half of the
// answer needs the provider registry, which hangs off Deps.
//
// That registry is the source of truth here, deliberately. A connector's
// credential lives in node.Secrets under connectorSecretKey(provider) --
// "slackOAuthAccessToken" -- and the sanitiser drops Secrets wholesale, so by
// the time this runs there is no trace of what was taken. What survives is
// the node's Template, and for an action node the template name and the
// provider name are the same string (see ExecuteAction's dispatch). Asking
// the registry instead of keeping a list here means a connector added later
// is covered on the day it is added.
//
// Without this the preview told somebody importing a Slack workflow that it
// was "ready to run as-is". It was not: the token had been stripped, and
// nothing on the page said so.
func (d *Deps) RequirementsForImport(graph models.WorkflowGraph) ShareImportRequirements {
	var req ShareImportRequirements
	connectors := d.registerConnectorProviders()
	needed := map[string]bool{}

	for _, n := range graph.Nodes {
		switch n.Type {
		case models.NodeTypeProvider:
			// keyMode "platform" bills the person running it against
			// AgentMesh's own key, so that node needs nothing. Anything else
			// is BYOK, and the key was stripped.
			if n.KeyMode != "platform" {
				req.APIKeys++
			}
		case models.NodeTypeGoogle:
			req.ConnectedAccounts++
		}
		if n.Template != "" && !needed[n.Template] {
			if _, ok := connectors[n.Template]; ok {
				needed[n.Template] = true
			}
		}
		for _, p := range n.CustomParams {
			if p.Kind == "file" && p.Value == "" {
				req.Files++
			}
		}
	}

	for name := range needed {
		req.Connectors = append(req.Connectors, name)
	}
	// Sorted so the sentence the preview builds reads the same on every
	// request; Go's map order would otherwise reshuffle it on each reload.
	sort.Strings(req.Connectors)
	return req
}

// --- Creating a link -------------------------------------------------------

// CreateShare freezes a sanitised copy of the workflow and returns its token.
//
// The stored nodes are sanitised WITHOUT being decrypted first, which is not
// an oversight: the sanitiser drops APIKey, EmailAPIKey and Secrets outright,
// so whether they are ciphertext or plaintext at that moment changes nothing
// about the result, and decrypting would only put the real values in memory
// on a path that has no use for them.
func (d *Deps) CreateShare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	wf, err := d.Store.GetWorkflow(ctx, id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	var body struct {
		// 0 or absent means the link never expires.
		ExpiresInDays int `json:"expiresInDays"`
		// Set by the Share dialog when it opens, which wants "a link to this,
		// as it is now" rather than "another link". False (the default) keeps
		// the plain mint-a-new-one behaviour that "New link" needs.
		ReuseIfUnchanged bool `json:"reuseIfUnchanged"`
	}
	// An empty body is legitimate here -- "share this, no expiry" needs no
	// fields -- so only a malformed one is an error.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ExpiresInDays < 0 || body.ExpiresInDays > maxShareExpiryDays {
		respond.Error(w, http.StatusBadRequest, "expiry must be between 0 and 365 days")
		return
	}
	var expiresAt *time.Time
	if body.ExpiresInDays > 0 {
		t := time.Now().UTC().AddDate(0, 0, body.ExpiresInDays)
		expiresAt = &t
	}

	graph, redactions := SanitizeGraphForShare(models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges})
	if msg, ok := shareGraphFits(graph); !ok {
		respond.Error(w, http.StatusRequestEntityTooLarge, msg)
		return
	}
	// An empty canvas is not something to hand anybody. ImportWorkflowGraph
	// has always refused a graph with no nodes; this end had no such guard,
	// so a brand-new workflow would mint a real, live link to nothing --
	// which the recipient opens, is told is "ready to run as-is", and imports
	// as an empty workflow.
	if len(graph.Nodes) == 0 {
		respond.Error(w, http.StatusBadRequest, "Add a node before sharing this workflow.")
		return
	}

	// A token is minted whether or not it ends up being used: the store
	// decides that, under a lock, and throws this away when it hands back a
	// link the workflow already has. Wasting 16 bytes of entropy is cheaper
	// than doing the lookup out here, where it could not be in the same
	// transaction as the insert it guards.
	token, err := randURLSafe(16)
	if err != nil {
		log.Printf("share workflow %s: token: %v", id, err)
		respond.Error(w, http.StatusInternalServerError, "could not create a share link")
		return
	}

	share, reused, err := d.Store.CreateWorkflowShare(ctx, models.WorkflowShare{
		Token:      token,
		WorkflowID: wf.ID,
		UserID:     userID,
		Name:       wf.Name,
		// Copied, not read through workflow_id, for the same frozen-snapshot
		// reason as the graph: rewording the workflow later must not silently
		// reword a link somebody has already passed on.
		Description: wf.Description,
		Graph:       graph,
		NodeCount:   len(graph.Nodes),
		EdgeCount:   len(graph.Edges),
		ExpiresAt:   expiresAt,
	}, body.ReuseIfUnchanged)
	if err != nil {
		if errors.Is(err, db.ErrShareQuotaExceeded) {
			respond.Error(w, http.StatusConflict, "You have too many share links. Revoke one before creating another.")
			return
		}
		log.Printf("share workflow %s: %v", id, err)
		respond.Error(w, http.StatusInternalServerError, "could not create a share link")
		return
	}

	// 200 when an existing link came back, 201 when one was made. The
	// redaction counts come from THIS sanitise either way, so a reused link
	// still tells the sharer what is being left out -- the dialog used to
	// decide reuse for itself, had no counts to show on that path, and went
	// quiet at exactly the moment somebody was about to hand the link over.
	status := http.StatusCreated
	if reused {
		status = http.StatusOK
	}
	respond.JSON(w, status, map[string]any{
		"share": share,
		// What was taken off the sharer's copy, so the dialog can say so
		// before they hand the link over.
		"redactions": redactions,
	})
}

// ListWorkflowShares returns the sharer's own links for one workflow,
// including revoked and expired ones -- this is the view where those still
// matter.
func (d *Deps) ListWorkflowShares(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	wf, err := d.Store.GetWorkflow(ctx, id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	shares, err := d.Store.ListWorkflowShares(ctx, id, userID)
	if err != nil {
		log.Printf("list shares for workflow %s: %v", id, err)
		respond.Error(w, http.StatusInternalServerError, "could not load this workflow's share links")
		return
	}
	// The listing is about the links, not their contents -- and a page
	// rendering a hundred rows has no use for a hundred graphs.
	for i := range shares {
		shares[i].Graph = models.WorkflowGraph{}
	}
	respond.JSON(w, http.StatusOK, map[string]any{"shares": shares})
}

// ListMyShares returns every link this user has out, across every workflow.
//
// The allowance MaxActiveWorkflowShares counts per user, not per workflow, so
// without this a person at the limit was told to "revoke one" with nowhere to
// see what they had -- the Share dialog only ever listed the links belonging
// to the workflow it was opened from.
func (d *Deps) ListMyShares(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	shares, err := d.Store.ListUserShares(ctx, userID)
	if err != nil {
		log.Printf("list shares for %s: %v", userID, err)
		respond.Error(w, http.StatusInternalServerError, "could not load your share links")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"shares": shares,
		// So the screen can say "12 of 100" rather than leaving somebody to
		// discover the ceiling by hitting it.
		"limit": db.MaxActiveWorkflowShares,
	})
}

// RevokeShare kills a link.
//
// 404 rather than 403 when the token belongs to somebody else, matching every
// other ownership check here: a caller should not learn that a token they do
// not own exists. Revoking one that is already revoked is also 404 -- there
// was nothing live to revoke.
func (d *Deps) RevokeShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	revoked, err := d.Store.RevokeWorkflowShare(ctx, token, userID)
	if err != nil {
		log.Printf("revoke share: %v", err)
		respond.Error(w, http.StatusInternalServerError, "could not revoke this link")
		return
	}
	if !revoked {
		respond.Error(w, http.StatusNotFound, "share link not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Reading a link --------------------------------------------------------

// GetShare serves a snapshot to anybody holding the token, with no session.
//
// Public because a share link that only opens for people who already have an
// account is not a share link. What makes that safe is not this handler but
// what is in the row: SanitizeGraphForShare ran before anything was stored,
// so there is no credential here to leak and no workflow id to correlate.
//
// Missing, revoked and expired all return the same 404, the rule
// PublicTrigger already follows: the response must not tell a caller whether
// a token ever existed.
func (d *Deps) GetShare(w http.ResponseWriter, r *http.Request) {
	if !shareReads.allow(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", strconv.Itoa(int(shareReadWindow.Seconds())))
		respond.Error(w, http.StatusTooManyRequests, "too many requests -- try again in a moment")
		return
	}

	share, ok := d.liveShare(w, r)
	if !ok {
		return
	}
	// How many people took a copy is the sharer's business. WorkflowID and
	// UserID are json:"-" already; this is the one that is not.
	share.ImportCount = 0

	respond.JSON(w, http.StatusOK, map[string]any{
		"share": share,
		// What the recipient still has to supply, worked out from the graph
		// in front of them.
		"requirements": d.RequirementsForImport(share.Graph),
	})
}

// liveShare loads a share and 404s unless it is readable, writing the
// response itself when it is not. Shared by the public read and the import so
// the two can never disagree about what "still live" means.
func (d *Deps) liveShare(w http.ResponseWriter, r *http.Request) (models.WorkflowShare, bool) {
	token := chi.URLParam(r, "token")
	share, err := d.Store.GetWorkflowShare(r.Context(), token)
	if err != nil {
		if !errors.Is(err, db.ErrShareNotFound) {
			log.Printf("read share: %v", err)
		}
		respond.Error(w, http.StatusNotFound, "this share link is no longer available")
		return models.WorkflowShare{}, false
	}
	if !share.Live(time.Now()) {
		respond.Error(w, http.StatusNotFound, "this share link is no longer available")
		return models.WorkflowShare{}, false
	}
	return share, true
}

// --- Importing -------------------------------------------------------------

// ImportShare copies a shared snapshot into the caller's own workspace.
//
// The graph is sanitised again on the way in even though it was sanitised on
// the way out. That is not belt-and-braces for its own sake: it makes the
// guarantee independent of how the row got there, so a snapshot written by an
// older build, or restored from a backup taken before the sanitiser covered
// some field, still cannot carry that field into a new workflow.
func (d *Deps) ImportShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	share, ok := d.liveShare(w, r)
	if !ok {
		return
	}

	graph, _ := SanitizeGraphForShare(share.Graph)
	// The description travels with the name. It is snapshotted beside the
	// graph for exactly this, and the preview shows it to the recipient
	// before they import -- dropping it here meant the one sentence saying
	// what the workflow is for vanished at the moment they accepted it.
	wf, ok := d.createImportedWorkflow(w, ctx, userID, share.Name, share.Description, graph)
	if !ok {
		return
	}

	// After the workflow exists, never in front of it: the import is the
	// outcome, the counter is a statistic, and a failed UPDATE must not cost
	// the caller a workflow that was created.
	if err := d.Store.RecordWorkflowShareImport(ctx, share.Token); err != nil {
		log.Printf("record share import %s: %v", share.Token, err)
	}

	respond.JSON(w, http.StatusCreated, wf)
}

// ImportWorkflowGraph creates a workflow from a graph the caller posts.
//
// This is the paste-a-code half of sharing, and it exists as its own endpoint
// rather than reusing POST /workflows + PUT /workflows/{id} for two reasons.
//
// It is atomic: the two-call version left an importer holding an empty
// workflow whenever the PUT failed, and leaned on the browser to delete it
// afterwards. And it is sanitised: PUT's encryptField passes an
// "enc:"-prefixed value through untouched, and on a workflow created moments
// earlier there is no prior value to compare it against, so a crafted code
// carrying `"apiKey": "enc:..."` would be stored verbatim and would decrypt
// with the server key on the next run. The ordinary save path is right to
// behave that way for its own caller -- the canvas round-trips its own
// ciphertext -- which is exactly why a stranger's graph must not use it.
func (d *Deps) ImportWorkflowGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	var body struct {
		Name string `json:"name"`
		// Optional: a code minted before the field existed carries none, and
		// an absent one simply leaves the imported workflow without a
		// description rather than failing the import.
		Description string                `json:"description"`
		Nodes       []models.WorkflowNode `json:"nodes"`
		Edges       []models.WorkflowEdge `json:"edges"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBodyBytes)).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "that does not look like a workflow")
		return
	}
	if len(body.Nodes) == 0 {
		respond.Error(w, http.StatusBadRequest, "that workflow has no nodes")
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Imported workflow"
	}
	// A console's name is reserved whoever is asking, and an import is the
	// one path where a name arrives from outside this app entirely.
	if isReservedSystemWorkflowName(name) {
		name = "Imported workflow"
	}

	graph, _ := SanitizeGraphForShare(models.WorkflowGraph{Nodes: body.Nodes, Edges: body.Edges})
	if msg, ok := shareGraphFits(graph); !ok {
		respond.Error(w, http.StatusRequestEntityTooLarge, msg)
		return
	}
	if len(graph.Nodes) == 0 {
		respond.Error(w, http.StatusBadRequest, "that workflow has no usable nodes")
		return
	}

	// Bounded for the same reason the column is: this arrives from outside
	// the app, and the description is prose nobody needs a novel of.
	description := strings.TrimSpace(body.Description)
	if len(description) > maxShareDescriptionChars {
		description = description[:maxShareDescriptionChars]
	}

	wf, ok := d.createImportedWorkflow(w, ctx, userID, name, description, graph)
	if !ok {
		return
	}
	respond.JSON(w, http.StatusCreated, wf)
}

// createImportedWorkflow is the shared tail of both import paths: clamp, mint
// the recipient's own webhook secrets, insert, and hand back the masked row.
// Writes the error response itself and reports ok=false.
func (d *Deps) createImportedWorkflow(
	w http.ResponseWriter, ctx context.Context, userID, name, description string, graph models.WorkflowGraph,
) (models.Workflow, bool) {
	nodes := make([]models.WorkflowNode, len(graph.Nodes))
	copy(nodes, graph.Nodes)
	clampRetryFields(nodes)
	// The sharer's webhook secret was dropped by the sanitiser, so every
	// webhook trigger arrives without one. This mints the RECIPIENT their
	// own -- without it the node exists but can never be triggered, since
	// PublicTrigger requires a secret and fails closed.
	nodes = ensureWebhookSecrets(nodes, d.EncryptionKey)

	wf, err := d.Store.CreateWorkflowWithGraph(ctx, name, description, userID, models.WorkflowGraph{
		Nodes: nodes,
		Edges: graph.Edges,
	})
	if err != nil {
		log.Printf("import workflow for %s: %v", userID, err)
		respond.Error(w, http.StatusInternalServerError, "could not import this workflow")
		return models.Workflow{}, false
	}

	// The same masking every other workflow response goes through. The
	// webhook secret is unmasked back to its new owner deliberately -- they
	// are the owner now, and they have to read it to point anything at the
	// trigger.
	decrypted := decryptNodes(wf.Nodes, d.EncryptionKey)
	wf.Nodes = unmaskWebhookSecrets(maskNodes(wf.Nodes), decrypted)
	return wf, true
}

// shareGraphFits reports whether a sanitised graph is within what a share may
// hold, and why not when it is not. The message names the actual limit, since
// "too large" alone leaves somebody with a 40-node workflow guessing.
func shareGraphFits(graph models.WorkflowGraph) (string, bool) {
	if len(graph.Nodes) > maxShareNodes {
		return "This workflow has too many nodes to share (limit " + strconv.Itoa(maxShareNodes) + ").", false
	}
	if len(graph.Edges) > maxShareEdges {
		return "This workflow has too many connections to share (limit " + strconv.Itoa(maxShareEdges) + ").", false
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		return "This workflow could not be prepared for sharing.", false
	}
	if len(encoded) > maxShareGraphBytes {
		return "This workflow is too large to share.", false
	}
	return "", true
}

// --- Rate limiting the public read ----------------------------------------

const (
	shareReadWindow    = time.Minute
	maxShareReadsPerIP = 60
	shareSweepInterval = 5 * time.Minute
)

// shareReadLimiter throttles the one route here that anybody can call.
//
// Scope, stated as honestly as geofence.go's pingLimiter states its own: this
// is IN-PROCESS and therefore PER-REPLICA, and it keys on an address a proxy
// may have collapsed or a caller may have forged. It is not a defence against
// a determined scraper and is not trying to be. Guessing a token is already
// infeasible -- 128 bits from crypto/rand -- so what remains is somebody
// walking a list of tokens they already hold, and a per-minute ceiling makes
// that slow and noisy at no legitimate cost: opening a link is one request,
// and nobody opens sixty a minute by hand.
type shareReadLimiter struct {
	mu        sync.Mutex
	hits      map[string]shareReadCount
	lastSweep time.Time
}

type shareReadCount struct {
	count int
	since time.Time
}

var shareReads shareReadLimiter

func (l *shareReadLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	if l.hits == nil {
		l.hits = make(map[string]shareReadCount)
	}
	entry, seen := l.hits[key]
	if !seen || now.Sub(entry.since) >= shareReadWindow {
		entry = shareReadCount{since: now}
	}
	entry.count++
	l.hits[key] = entry
	allowed := entry.count <= maxShareReadsPerIP

	needsSweep := now.Sub(l.lastSweep) > shareSweepInterval
	if needsSweep {
		l.lastSweep = now
	}
	l.mu.Unlock()

	// Off the request path, for the reason pingLimiter gives: the caller
	// whose request happened to land on the sweep should not wait behind a
	// full map scan for a decision that has already been made.
	if needsSweep {
		go l.sweep(now)
	}
	return allowed
}

func (l *shareReadLimiter) sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.hits {
		if now.Sub(entry.since) >= shareReadWindow {
			delete(l.hits, key)
		}
	}
}

// clientIP is best-effort and says so. X-Forwarded-For is trusted because the
// app runs behind a proxy that sets it, and RemoteAddr would otherwise be
// that proxy for every caller alike -- which would turn the limiter above
// into a global one. A forged header buys an attacker a fresh bucket, which
// is precisely why the limiter is documented as a deterrent rather than a
// control.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, found := strings.Cut(fwd, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
