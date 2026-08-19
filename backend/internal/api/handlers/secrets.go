package handlers

import (
	"strings"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/wallet"
)

const encPrefix = "enc:"

// EncSentinel is returned to the frontend in place of an encrypted key value.
// The frontend sends it back unchanged when the user hasn't touched the field.
const EncSentinel = "__enc__"

// ClearSentinel is what the frontend sends to explicitly blank out a secret
// field -- distinct from "", which encryptField (below) treats as "no
// change, keep whatever's already saved." Without a distinct sentinel,
// removing every entry from a multi-value secret (e.g. the HTTP tool
// node's custom-headers editor) serializes to "" and silently fails to
// revoke the previously-saved value server-side.
const ClearSentinel = "__clear__"

// encryptNodes returns a copy of nodes with apiKey/emailApiKey encrypted.
// existing is the prior DB state — used to preserve the encrypted blob when
// the frontend sends back the sentinel (meaning "don't change this key").
func encryptNodes(nodes []models.WorkflowNode, key string, existing []models.WorkflowNode) []models.WorkflowNode {
	if key == "" {
		return nodes
	}
	byID := make(map[string]models.WorkflowNode, len(existing))
	for _, n := range existing {
		byID[n.ID] = n
	}
	out := make([]models.WorkflowNode, len(nodes))
	copy(out, nodes)
	for i, n := range out {
		prev := byID[n.ID]
		out[i].APIKey = encryptField(n.APIKey, prev.APIKey, key)
		out[i].EmailAPIKey = encryptField(n.EmailAPIKey, prev.EmailAPIKey, key)
		out[i].Secrets = encryptSecretsMap(n.Secrets, prev.Secrets, key)
	}
	return out
}

// encryptSecretsMap encrypts every value in a per-connector secrets map, keyed by
// the same secret name so sentinel-preservation works per key, not per node.
func encryptSecretsMap(newVals, existingVals map[string]string, key string) map[string]string {
	if newVals == nil {
		return existingVals
	}
	out := make(map[string]string, len(existingVals)+len(newVals))
	for k, v := range existingVals {
		out[k] = v
	}
	for k, v := range newVals {
		out[k] = encryptField(v, existingVals[k], key)
	}
	return out
}

// encryptField encrypts a single secret field.
// If newVal is the sentinel or empty, the existing encrypted blob is preserved.
// If newVal is ClearSentinel, the field is explicitly wiped instead.
func encryptField(newVal, existingEnc, key string) string {
	if newVal == ClearSentinel {
		return ""
	}
	if newVal == EncSentinel || newVal == "" {
		return existingEnc // keep whatever was already there
	}
	if strings.HasPrefix(newVal, encPrefix) {
		return newVal // already encrypted
	}
	enc, err := wallet.Encrypt(newVal, key)
	if err != nil {
		return newVal // fallback: store plaintext (shouldn't happen with valid key)
	}
	return encPrefix + enc
}

// maskNodes returns a copy of nodes with encrypted blobs replaced by the sentinel.
// This is what gets returned to the frontend on GET/list — never the raw ciphertext.
func maskNodes(nodes []models.WorkflowNode) []models.WorkflowNode {
	out := make([]models.WorkflowNode, len(nodes))
	copy(out, nodes)
	for i, n := range out {
		if strings.HasPrefix(n.APIKey, encPrefix) {
			out[i].APIKey = EncSentinel
		}
		if strings.HasPrefix(n.EmailAPIKey, encPrefix) {
			out[i].EmailAPIKey = EncSentinel
		}
		out[i].Secrets = maskSecretsMap(n.Secrets)
	}
	return out
}

func maskSecretsMap(vals map[string]string) map[string]string {
	if vals == nil {
		return nil
	}
	out := make(map[string]string, len(vals))
	for k, v := range vals {
		if strings.HasPrefix(v, encPrefix) {
			out[k] = EncSentinel
		} else {
			out[k] = v
		}
	}
	return out
}

// redactNodesForBuildAgent returns nodes safe to hand to a third-party LLM
// (the chat workflow builder's Gemini calls): maskNodes's usual sentinel
// redaction, plus stripping CustomParam file bytes. maskNodes alone only
// covers APIKey/EmailAPIKey/Secrets -- a CustomParam with Kind "file" carries
// its content base64-encoded in Value, up to maxParamFileBytes (2 MiB, see
// tool402.go), which maskNodes leaves untouched. That's a real file the user
// uploaded to THIS app, not Google -- sending it on every build-mode chat
// message both risks the request blowing past size limits and ships file
// content to a third party with no user-visible consent. File metadata
// (name, MIME type) is kept so the agent can still reason about "there's an
// uploaded file here" without ever seeing its bytes.
func redactNodesForBuildAgent(nodes []models.WorkflowNode) []models.WorkflowNode {
	out := maskNodes(nodes)
	for i, n := range out {
		if len(n.CustomParams) == 0 {
			continue
		}
		params := make([]models.CustomParam, len(n.CustomParams))
		copy(params, n.CustomParams)
		for j, p := range params {
			if p.Kind == "file" {
				params[j].Value = ""
			}
		}
		out[i].CustomParams = params
	}
	return out
}

// decryptNodes returns a copy of nodes with encrypted blobs decrypted in-memory.
// Call this just before passing nodes to the engine for execution.
func decryptNodes(nodes []models.WorkflowNode, key string) []models.WorkflowNode {
	if key == "" {
		return nodes
	}
	out := make([]models.WorkflowNode, len(nodes))
	copy(out, nodes)
	for i, n := range out {
		out[i].APIKey = decryptField(n.APIKey, key)
		out[i].EmailAPIKey = decryptField(n.EmailAPIKey, key)
		out[i].Secrets = decryptSecretsMap(n.Secrets, key)
	}
	return out
}

func decryptSecretsMap(vals map[string]string, key string) map[string]string {
	if vals == nil {
		return nil
	}
	out := make(map[string]string, len(vals))
	for k, v := range vals {
		out[k] = decryptField(v, key)
	}
	return out
}

func decryptField(val, key string) string {
	if !strings.HasPrefix(val, encPrefix) {
		return val
	}
	plain, err := wallet.Decrypt(strings.TrimPrefix(val, encPrefix), key)
	if err != nil {
		return val
	}
	return plain
}
