package handlers

import (
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// Turning one person's workflow into something safe to hand to another.
//
// This is an ALLOWLIST, and that is the whole point of the file. It builds a
// fresh models.WorkflowNode and copies named fields onto it, so a field added
// to that struct tomorrow is absent from a share until somebody deliberately
// adds it here. A denylist would do the opposite, and we already know how
// that ends: maskNodes never grew a case for CustomParams, which is why a
// share code built from GetWorkflow's response carries uploaded file bytes
// today.
//
// share_sanitize_test.go enforces the property mechanically -- it reflects
// over WorkflowNode and fails if any field is unclassified -- so this comment
// describes the design rather than being the thing keeping it true.
//
// Kept out of secrets.go on purpose: that file is about encrypting a secret
// at rest and masking it on the way back to its owner. This one is about what
// may cross to somebody who is not the owner at all, which is a different
// question with a different answer (delete, not mask).

// shareConfigDenyKeys are Config entries dropped by exact name.
//
// oauthCredentialID is a foreign key into the SHARER's oauth_credentials row.
// nodes.googleAccessToken checks cred.UserID against the run's owner, so a
// copied workflow cannot actually read the sharer's Gmail -- but the id is
// still an identifier leak, and leaving it produces an import that fails at
// run time with "this connection may have been disconnected" instead of a
// clean "connect your account".
var shareConfigDenyKeys = map[string]bool{
	"oauthCredentialID": true,
	// Phone numbers, on the Twilio action node. Config rather than Secrets
	// because they are not credentials -- which does not make them somebody
	// else's to receive.
	"twilioTo":   true,
	"twilioFrom": true,
}

// shareConfigDenySuffixes drop Config entries by shape rather than by name,
// because the connector-OAuth code mints these per provider at run time
// (connector_oauth.go's connectorSecretKey/connectorRefreshKey and friends):
// "slackOAuthAccessToken", "jiraOAuthCloudID", "mailchimpOAuthDC". Naming
// each one would go stale the first time a connector is added.
//
// "Email" catches jiraEmail, stripeEmail, zendeskEmail and the rest -- the
// account addresses connectors want alongside a token. Personal data, and of
// no use to a recipient who will supply their own.
var shareConfigDenySuffixes = []string{
	"OAuthAccessToken",
	"OAuthRefreshToken",
	"OAuthExpiresAt",
	"OAuthCloudID",
	"OAuthDC",
	"Email",
}

// ShareRedactions counts what sanitising removed, so the Share dialog can
// tell the sharer what is not going out and the recipient what they will have
// to supply. Counts, not values: naming the removed key would be a smaller
// version of the leak being prevented.
type ShareRedactions struct {
	APIKeys           int `json:"apiKeys"`
	Secrets           int `json:"secrets"`
	WebhookSecrets    int `json:"webhookSecrets"`
	UploadedFiles     int `json:"uploadedFiles"`
	AgentWallets      int `json:"agentWallets"`
	EmailAddresses    int `json:"emailAddresses"`
	ConnectedAccounts int `json:"connectedAccounts"`
	LeasedMachines    int `json:"leasedMachines"`
}

// Any reports whether anything at all was removed, so a caller can skip the
// "we removed some things" section rather than render a row of zeroes.
func (r ShareRedactions) Any() bool {
	return r.APIKeys+r.Secrets+r.WebhookSecrets+r.UploadedFiles+
		r.AgentWallets+r.EmailAddresses+r.ConnectedAccounts+r.LeasedMachines > 0
}

// SanitizeGraphForShare returns a copy of graph safe to publish, and a count
// of what it removed.
//
// It runs in BOTH directions deliberately. On the way out it is what makes a
// snapshot publishable. On the way in it is what makes an imported graph
// safe, because the payload on that side is whatever a stranger pasted: an
// "enc:"-prefixed value posted to the ordinary save path is stored verbatim
// (encryptField returns it untouched) and later decrypts with the server key,
// so a hand-crafted share could otherwise plant a WORKING credential in
// somebody else's workflow. Running the same allowlist inbound means a field
// that cannot be shared cannot be imported either.
//
// Edges are filtered to those whose endpoints both resolve to a surviving
// node, and nodes with a duplicate id are dropped. Neither can arise from our
// own canvas; both can arise from a crafted payload, and a dangling edge
// reaches the topological sort as a reference to a node that is not there.
func SanitizeGraphForShare(graph models.WorkflowGraph) (models.WorkflowGraph, ShareRedactions) {
	var red ShareRedactions

	seen := make(map[string]bool, len(graph.Nodes))
	nodes := make([]models.WorkflowNode, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		if n.ID == "" || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		nodes = append(nodes, sanitizeNodeForShare(n, &red))
	}

	edges := make([]models.WorkflowEdge, 0, len(graph.Edges))
	seenEdges := make(map[string]bool, len(graph.Edges))
	for _, e := range graph.Edges {
		if !seen[e.From] || !seen[e.To] {
			continue
		}
		// A repeated edge id goes the same way a repeated node id does. The
		// canvas keys its rendered edges by id, so two edges sharing one give
		// the recipient a key collision: one of the pair does not draw, and
		// the graph reads as wired differently from the one that was shared.
		// An id-less edge is left alone -- the canvas has always tolerated
		// those, and minting a key for one is not this file's business.
		if e.ID != "" {
			if seenEdges[e.ID] {
				continue
			}
			seenEdges[e.ID] = true
		}
		edges = append(edges, e)
	}

	return models.WorkflowGraph{Nodes: nodes, Edges: edges}, red
}

// sanitizeNodeForShare builds one publishable node. Every field is either
// listed here or deliberately absent; see share_sanitize_test.go.
func sanitizeNodeForShare(n models.WorkflowNode, red *ShareRedactions) models.WorkflowNode {
	// Counted before the copy, since the copy is where they stop existing.
	if n.APIKey != "" {
		red.APIKeys++
	}
	if n.EmailAPIKey != "" {
		red.APIKeys++
	}
	if n.Wallet != "" {
		red.AgentWallets++
	}
	if n.EmailTo != "" || n.EmailFrom != "" {
		red.EmailAddresses++
	}
	if n.TendrilNodeID != "" {
		red.LeasedMachines++
	}
	for k, v := range n.Secrets {
		if v == "" {
			continue
		}
		if k == "webhookSecret" {
			// Counted apart because it is the one secret AgentMesh generated
			// FOR the sharer rather than one they pasted, and the one whose
			// absence the recipient never has to act on: ensureWebhookSecrets
			// mints them a fresh one on save. Telling them to "supply a
			// webhook secret" would be wrong advice.
			red.WebhookSecrets++
			continue
		}
		red.Secrets++
	}

	return models.WorkflowNode{
		// Identity and layout. The id must survive verbatim: edges reference
		// it, and an agent's model/tools wiring is nothing but attach edges
		// pointing at these ids. Ids are workflow-scoped, so carrying them
		// across accounts collides with nothing.
		ID:       n.ID,
		Type:     n.Type,
		Template: n.Template,
		X:        n.X,
		Y:        n.Y,

		// What the node is called and what it does.
		Name:         n.Name,
		Label:        n.Label,
		Icon:         n.Icon,
		Description:  n.Description,
		SystemPrompt: n.SystemPrompt,

		// Which model, and whose key pays for it. KeyMode is carried rather
		// than reset: "platform" means the run bills THE PERSON RUNNING IT
		// against AgentMesh's own key, which is exactly what makes the demo
		// workflows work for a brand-new account with nothing configured.
		// Resetting it to BYOK would hand the recipient a graph that cannot
		// run until they paste a key -- the opposite of the point.
		Model:   n.Model,
		KeyMode: n.KeyMode,

		// Where the node points. Carried, because a tool node without its
		// endpoint is not a tool node -- there would be nothing left to
		// import. The Share dialog shows these back to the sharer before the
		// link is created, which is the right control for "this URL is
		// private": a person deciding, not a heuristic guessing.
		URL:      n.URL,
		Method:   n.Method,
		Endpoint: n.Endpoint,
		Price:    n.Price,
		Unit:     n.Unit,
		Provider: n.Provider,
		Source:   n.Source,

		// The email body a workflow sends is part of what it does. Who it is
		// sent to and from is not -- see the omissions at the bottom.
		EmailSubject:  n.EmailSubject,
		EmailBody:     n.EmailBody,
		EmailProvider: n.EmailProvider,

		// How the endpoint is called. DiscoveredParams is a schema read back
		// from the endpoint itself, so it is the endpoint's data rather than
		// the sharer's.
		DiscoveredParams: n.DiscoveredParams,
		ParamDefaults:    copyStringMap(n.ParamDefaults),
		CustomParams:     shareCustomParams(n.CustomParams, red),
		BodyMode:         n.BodyMode,
		BodyTemplate:     n.BodyTemplate,

		// Non-secret connector settings, minus the account-binding ones.
		Config: shareConfig(n.Config, red),

		StateOp:    n.StateOp,
		StateKey:   n.StateKey,
		StateValue: n.StateValue,

		// Tendril: what to do and how much to buy is configuration. WHICH
		// machine is currently leased is not -- TendrilNodeID is the sharer's
		// own lease row.
		TendrilAction:     n.TendrilAction,
		TendrilHours:      n.TendrilHours,
		TendrilAmount:     n.TendrilAmount,
		TendrilMinBalance: n.TendrilMinBalance,
		TendrilCoverHours: n.TendrilCoverHours,

		MaxRetries:     n.MaxRetries,
		RetryBackoffMs: n.RetryBackoffMs,

		// Deliberately not copied, listed so the omissions read as decisions
		// rather than oversights:
		//
		//   APIKey, EmailAPIKey, Secrets  the credentials themselves. Deleted
		//       rather than masked: maskNodes only masks values carrying the
		//       "enc:" prefix, encryptField falls back to storing plaintext
		//       when encryption fails, and encryptNodes is a no-op when no
		//       key is configured -- three ways a masked-only share could
		//       still carry a real value. Dropping Secrets wholesale also
		//       covers every connector added after this was written; there
		//       are already 56+ known keys and allowlisting them would rot.
		//   Wallet, Balance  the sharer's Algorand agent address. Wallets
		//       live in agent_wallets keyed (workflow_id, agent_node_id), so
		//       the recipient gets their own on deploy.
		//   EmailTo, EmailFrom  somebody's address.
		//   TendrilNodeID  the sharer's leased machine.
		//   TendrilLeaseToken  already json:"-"; never persisted at all.
	}
}

// shareCustomParams keeps a file param's identity and drops its contents.
//
// A "file" param carries the whole uploaded file base64-encoded in Value, up
// to maxParamFileBytes (2 MiB, tool402.go) -- real bytes the sharer uploaded
// to this app. Name/FileName/MIMEType stay so the recipient can see there is
// a file to supply and what shape it should be, which is exactly the trade
// redactNodesForBuildAgent already makes before sending a graph to Gemini.
func shareCustomParams(params []models.CustomParam, red *ShareRedactions) []models.CustomParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]models.CustomParam, len(params))
	copy(out, params)
	for i, p := range out {
		if p.Kind != "file" {
			continue
		}
		if p.Value != "" {
			red.UploadedFiles++
		}
		out[i].Value = ""
	}
	return out
}

// shareConfig copies the non-secret connector settings, minus the entries
// that bind a node to the sharer's own account.
func shareConfig(config map[string]string, red *ShareRedactions) map[string]string {
	if len(config) == 0 {
		return nil
	}
	out := make(map[string]string, len(config))
	for k, v := range config {
		if isSharedConfigDenied(k) {
			if v != "" {
				red.ConnectedAccounts++
			}
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSharedConfigDenied(key string) bool {
	if shareConfigDenyKeys[key] {
		return true
	}
	for _, suffix := range shareConfigDenySuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
