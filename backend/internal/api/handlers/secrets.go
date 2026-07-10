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
		return nil
	}
	out := make(map[string]string, len(newVals))
	for k, v := range newVals {
		out[k] = encryptField(v, existingVals[k], key)
	}
	return out
}

// encryptField encrypts a single secret field.
// If newVal is the sentinel or empty, the existing encrypted blob is preserved.
func encryptField(newVal, existingEnc, key string) string {
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
