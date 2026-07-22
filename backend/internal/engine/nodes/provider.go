package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/agentmesh/backend/internal/models"
)

var openAIBaseURL = "https://api.openai.com"
var groqBaseURL = "https://api.groq.com/openai"
var mistralBaseURL = "https://api.mistral.ai"

func SetOpenAIBaseURL(u string) { openAIBaseURL = u }

// ExecuteAgent runs the agent node: calls the attached LLM with function calling
// support so the agent decides whether and how to invoke its attached tools.
// platformKeys maps provider template ("openai", "gemini", ...) to AgentMesh's
// own API key for that provider, used only when the Provider node's KeyMode is
// "platform". Never empty-checked against BYOK nodes — resolveAPIKey ignores it
// entirely unless KeyMode == "platform".
func ExecuteAgent(ctx context.Context, node models.WorkflowNode, attach models.AttachConfig, aw models.AgentWallet, signer WalletSigner, rc RunContexter, checkBalance BalanceChecker, platformKeys map[string]string) (any, error) {
	if attach.Provider == nil {
		return rc.UserInput(), nil
	}
	p := attach.Provider
	switch p.Template {
	case "openai", "groq", "mistral":
		return callOpenAICompat(ctx, node, *p, attach.Tools, aw, signer, rc, checkBalance, platformKeys)
	case "anthropic":
		return callAnthropic(ctx, node, *p, rc, platformKeys)
	case "gemini":
		return callGemini(ctx, node, *p, attach.Tools, aw, signer, rc, checkBalance, platformKeys)
	default:
		return callOpenAICompat(ctx, node, *p, attach.Tools, aw, signer, rc, checkBalance, platformKeys)
	}
}

// resolveAPIKey returns the API key a Provider node's call should use: its
// own APIKey for BYOK (KeyMode != "platform"), or AgentMesh's platform key
// for its Template when KeyMode == "platform". Errors rather than silently
// falling back to an empty key when platform mode is selected but no key is
// configured for that template — an empty Authorization header would just
// surface as a confusing 401 from the upstream provider instead.
func resolveAPIKey(provider models.WorkflowNode, platformKeys map[string]string) (string, error) {
	if provider.KeyMode != "platform" {
		return provider.APIKey, nil
	}
	key, ok := platformKeys[provider.Template]
	if !ok || key == "" {
		return "", fmt.Errorf("platform key not configured for provider %q", provider.Template)
	}
	return key, nil
}

// platformKeyUsageResult wraps a final agent answer with tier/usage metadata
// when the call ran on a platform key, so the Runner can bill the right
// tier fee and record usage on the debit_ledger row. BYOK calls never go
// through this — their return shape is unchanged from before this feature.
func platformKeyUsageResult(provider models.WorkflowNode, tokensIn, tokensOut int, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range extra {
		out[k] = v
	}
	out["platformKeyUsage"] = map[string]any{
		"tier":      ModelTier(provider.Template, provider.Model),
		"model":     provider.Model,
		"tokensIn":  tokensIn,
		"tokensOut": tokensOut,
	}
	return out
}

// ─── function declaration helpers ────────────────────────────────────────────

type funcDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// sanitizeFuncName converts any string into a valid LLM function name.
// Rules: starts with [a-zA-Z_], contains only [a-zA-Z0-9_-], max 64 chars.
func sanitizeFuncName(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsLetter(r) || r == '_' {
			b.WriteRune(r)
		} else if i > 0 && (unicode.IsDigit(r) || r == '-') {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteRune('_')
		}
	}
	result := b.String()
	// Trim trailing underscores
	result = strings.TrimRight(result, "_")
	if len(result) > 64 {
		result = result[:64]
	}
	if result == "" {
		result = "tool"
	}
	return result
}

func toolFuncName(t models.WorkflowNode) string {
	name := sanitizeFuncName(t.Name)
	if name == "" || name == "tool" {
		name = sanitizeFuncName(t.ID)
	}
	return name
}

func buildFuncDecls(tools []models.WorkflowNode) []funcDecl {
	decls := make([]funcDecl, 0, len(tools))
	for _, t := range tools {
		name := toolFuncName(t)
		desc := t.Description
		if desc == "" {
			desc = "External tool: " + t.Name
			if t.Endpoint != "" {
				desc += ". Endpoint: " + t.Endpoint
			}
		}

		properties := map[string]any{}
		required := []string{}

		for _, p := range t.DiscoveredParams {
			pType := "string"
			switch strings.ToLower(p.Type) {
			case "number", "integer", "float", "int":
				pType = "number"
			case "boolean", "bool":
				pType = "boolean"
			}
			prop := map[string]any{"type": pType, "description": p.Description}
			if p.Default != "" {
				prop["default"] = p.Default
			}
			properties[p.Name] = prop
			if p.Required {
				required = append(required, p.Name)
			}
		}

		params := map[string]any{"type": "OBJECT", "properties": properties}
		if len(required) > 0 {
			params["required"] = required
		}

		decls = append(decls, funcDecl{Name: name, Description: desc, Parameters: params})
	}
	return decls
}

// ─── tool execution helper ────────────────────────────────────────────────────

func executeFunctionCall(ctx context.Context, funcName string, args map[string]any, tools []models.WorkflowNode, aw models.AgentWallet, signer WalletSigner, rc RunContexter, checkBalance BalanceChecker) (any, models.WorkflowNode, error) {
	for _, t := range tools {
		if toolFuncName(t) != funcName {
			continue
		}
		toolNode := t
		if checkBalance != nil {
			var feeAmount int64
			switch {
			case toolNode.Type == models.NodeTypeTool402:
				feeAmount = models.X402PlatformFeeUSDMicros
			case BillableFlatFee(toolNode.Type, toolNode.Template):
				feeAmount = models.ByokFlatFeeUSDMicros
			}
			if feeAmount > 0 {
				if err := checkBalance(ctx, feeAmount); err != nil {
					return nil, toolNode, &ErrBalanceBlocked{Err: err}
				}
			}
		}
		// Append LLM-chosen args as query params onto the endpoint URL
		if len(args) > 0 && toolNode.Endpoint != "" {
			u, err := url.Parse(toolNode.Endpoint)
			if err == nil {
				q := u.Query()
				for k, v := range args {
					q.Set(k, fmt.Sprintf("%v", v))
				}
				u.RawQuery = q.Encode()
				toolNode.Endpoint = u.String()
			}
		}
		if t.Type == models.NodeTypeTool402 {
			result, err := ExecuteTool402(ctx, toolNode, rc, aw, signer)
			return result, toolNode, err
		}
		result, err := ExecuteTool(ctx, toolNode, rc)
		return result, toolNode, err
	}
	return nil, models.WorkflowNode{}, fmt.Errorf("tool %q not found in attached tools", funcName)
}

// ─── low-level HTTP helper ────────────────────────────────────────────────────

// postLLMJSON posts a JSON payload to an LLM provider API and decodes the response body.
// Distinct from the connector-oriented postJSON in connector_helpers.go, which returns a
// caller-supplied sentinel instead of the decoded body.
func postLLMJSON(ctx context.Context, apiURL string, headers map[string]string, payload any) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("LLM API %d: %s", resp.StatusCode, string(b))
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ─── Gemini ───────────────────────────────────────────────────────────────────

const maxToolIterations = 15

// collectX402Receipt builds a receipt map from a successful x402 tool result.
func collectX402Receipt(funcName string, result map[string]any, tools []models.WorkflowNode) map[string]any {
	txID, _ := result["txId"].(string)
	amount, _ := result["amount"].(string)
	explorerURL, _ := result["explorerURL"].(string)
	receipt := map[string]any{"txId": txID, "amount": amount, "explorerURL": explorerURL}
	for _, t := range tools {
		if toolFuncName(t) == funcName {
			receipt["nodeId"] = t.ID
			receipt["nodeName"] = t.Name
			break
		}
	}
	return receipt
}

func callGemini(ctx context.Context, agent models.WorkflowNode, provider models.WorkflowNode, tools []models.WorkflowNode, aw models.AgentWallet, signer WalletSigner, rc RunContexter, checkBalance BalanceChecker, platformKeys map[string]string) (any, error) {
	model := provider.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	apiKey, err := resolveAPIKey(provider, platformKeys)
	if err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	apiHeaders := map[string]string{"x-goog-api-key": apiKey}

	contents := []map[string]any{
		{"role": "user", "parts": []map[string]any{{"text": rc.UserInput()}}},
	}

	payload := map[string]any{"contents": contents}
	if agent.SystemPrompt != "" {
		payload["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": agent.SystemPrompt}},
		}
	}
	decls := buildFuncDecls(tools)
	if len(decls) > 0 {
		payload["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}

	var x402Payments []map[string]any
	var billedFlatFeeNodeIds []string
	var tokensIn, tokensOut int

	// Agentic loop — keep calling until the model returns text (no function call).
	for iter := 0; iter < maxToolIterations; iter++ {
		resp, err := postLLMJSON(ctx, apiURL, apiHeaders, payload)
		if err != nil {
			return nil, err
		}
		if usage, ok := resp["usageMetadata"].(map[string]any); ok {
			if v, ok := usage["promptTokenCount"].(float64); ok {
				tokensIn += int(v)
			}
			if v, ok := usage["candidatesTokenCount"].(float64); ok {
				tokensOut += int(v)
			}
		}

		// Collect all function calls the model wants to make in this turn
		// (Gemini can return multiple functionCall parts at once for parallel use).
		calls := extractGeminiFunctionCalls(resp)
		if len(calls) == 0 {
			text, textErr := extractGeminiText(resp)
			if textErr != nil {
				return nil, textErr
			}
			if provider.KeyMode == "platform" {
				extra := map[string]any{"message": text}
				if len(x402Payments) > 0 {
					extra["x402Payments"] = x402Payments
				}
				if len(billedFlatFeeNodeIds) > 0 {
					extra["billedFlatFeeNodeIds"] = billedFlatFeeNodeIds
				}
				return platformKeyUsageResult(provider, tokensIn, tokensOut, extra), nil
			}
			if len(x402Payments) > 0 || len(billedFlatFeeNodeIds) > 0 {
				out := map[string]any{"message": text}
				if len(x402Payments) > 0 {
					out["x402Payments"] = x402Payments
				}
				if len(billedFlatFeeNodeIds) > 0 {
					out["billedFlatFeeNodeIds"] = billedFlatFeeNodeIds
				}
				return out, nil
			}
			return text, nil
		}

		// Build the model turn (all function calls in one "model" message)
		modelParts := make([]map[string]any, len(calls))
		for i, c := range calls {
			modelParts[i] = map[string]any{"functionCall": map[string]any{"name": c.name, "args": c.args}}
		}
		contents = append(contents, map[string]any{"role": "model", "parts": modelParts})

		// Execute every requested function call and collect responses
		responseParts := make([]map[string]any, 0, len(calls))
		for _, c := range calls {
			result, toolNode, execErr := executeFunctionCall(ctx, c.name, c.args, tools, aw, signer, rc, checkBalance)
			if execErr != nil {
				var blocked *ErrBalanceBlocked
				if errors.As(execErr, &blocked) {
					return nil, execErr
				}
			}
			resultStr := ""
			if execErr != nil {
				resultStr = "error: " + execErr.Error()
			} else {
				if m, ok := result.(map[string]any); ok {
					if _, hasTx := m["txId"]; hasTx {
						x402Payments = append(x402Payments, collectX402Receipt(c.name, m, tools))
					}
				}
				if BillableFlatFee(toolNode.Type, toolNode.Template) {
					billedFlatFeeNodeIds = append(billedFlatFeeNodeIds, toolNode.ID)
				}
				b, _ := json.Marshal(result)
				resultStr = string(b)
			}
			responseParts = append(responseParts, map[string]any{
				"functionResponse": map[string]any{
					"name":     c.name,
					"response": map[string]any{"result": resultStr},
				},
			})
		}

		// Feed all results back in one "user" turn
		contents = append(contents, map[string]any{"role": "user", "parts": responseParts})
		payload["contents"] = contents
	}

	return nil, fmt.Errorf("agent exceeded maximum tool call iterations (%d)", maxToolIterations)
}

func extractGeminiText(resp map[string]any) (string, error) {
	candidates, _ := resp["candidates"].([]any)
	if len(candidates) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}
	content, _ := candidates[0].(map[string]any)["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	for _, p := range parts {
		part, _ := p.(map[string]any)
		if text, ok := part["text"].(string); ok {
			return text, nil
		}
	}
	return "", fmt.Errorf("no text part in Gemini response")
}

type geminiFuncCall struct {
	name string
	args map[string]any
}

// extractGeminiFunctionCalls returns ALL functionCall parts in the first candidate.
// Gemini may request multiple parallel tool calls in a single turn.
func extractGeminiFunctionCalls(resp map[string]any) []geminiFuncCall {
	candidates, _ := resp["candidates"].([]any)
	if len(candidates) == 0 {
		return nil
	}
	content, _ := candidates[0].(map[string]any)["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	var calls []geminiFuncCall
	for _, p := range parts {
		part, _ := p.(map[string]any)
		if fc, ok := part["functionCall"].(map[string]any); ok {
			name, _ := fc["name"].(string)
			args, _ := fc["args"].(map[string]any)
			calls = append(calls, geminiFuncCall{name: name, args: args})
		}
	}
	return calls
}

// ─── OpenAI / Groq / Mistral ──────────────────────────────────────────────────

func callOpenAICompat(ctx context.Context, agent models.WorkflowNode, provider models.WorkflowNode, tools []models.WorkflowNode, aw models.AgentWallet, signer WalletSigner, rc RunContexter, checkBalance BalanceChecker, platformKeys map[string]string) (any, error) {
	baseURL := openAIBaseURL
	switch provider.Template {
	case "groq":
		baseURL = groqBaseURL
	case "mistral":
		baseURL = mistralBaseURL
	}
	model := provider.Model
	if model == "" {
		model = "gpt-4o"
	}

	apiKey, err := resolveAPIKey(provider, platformKeys)
	if err != nil {
		return nil, err
	}

	messages := []map[string]any{}
	if agent.SystemPrompt != "" {
		messages = append(messages, map[string]any{"role": "system", "content": agent.SystemPrompt})
	}
	messages = append(messages, map[string]any{"role": "user", "content": rc.UserInput()})

	payload := map[string]any{"model": model, "messages": messages}

	decls := buildFuncDecls(tools)
	if len(decls) > 0 {
		oaiTools := make([]map[string]any, len(decls))
		for i, d := range decls {
			oaiTools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        d.Name,
					"description": d.Description,
					"parameters":  d.Parameters,
				},
			}
		}
		payload["tools"] = oaiTools
	}

	headers := map[string]string{"Authorization": "Bearer " + apiKey}

	var x402Payments []map[string]any
	var billedFlatFeeNodeIds []string
	var tokensIn, tokensOut int

	// Agentic loop — repeat until the model returns content with no tool calls.
	for iter := 0; iter < maxToolIterations; iter++ {
		payload["messages"] = messages
		resp, err := postLLMJSON(ctx, baseURL+"/v1/chat/completions", headers, payload)
		if err != nil {
			return nil, err
		}
		if usage, ok := resp["usage"].(map[string]any); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				tokensIn += int(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				tokensOut += int(v)
			}
		}

		choices, _ := resp["choices"].([]any)
		if len(choices) == 0 {
			return nil, fmt.Errorf("empty choices from LLM")
		}
		choice, _ := choices[0].(map[string]any)
		msg, _ := choice["message"].(map[string]any)

		toolCalls, _ := msg["tool_calls"].([]any)
		if len(toolCalls) == 0 {
			// No tool calls — return the final answer
			content, _ := msg["content"].(string)
			if provider.KeyMode == "platform" {
				extra := map[string]any{"message": content}
				if len(x402Payments) > 0 {
					extra["x402Payments"] = x402Payments
				}
				if len(billedFlatFeeNodeIds) > 0 {
					extra["billedFlatFeeNodeIds"] = billedFlatFeeNodeIds
				}
				return platformKeyUsageResult(provider, tokensIn, tokensOut, extra), nil
			}
			if len(x402Payments) > 0 || len(billedFlatFeeNodeIds) > 0 {
				out := map[string]any{"message": content}
				if len(x402Payments) > 0 {
					out["x402Payments"] = x402Payments
				}
				if len(billedFlatFeeNodeIds) > 0 {
					out["billedFlatFeeNodeIds"] = billedFlatFeeNodeIds
				}
				return out, nil
			}
			return content, nil
		}

		// Build the assistant message with all tool calls
		assistantMsg := map[string]any{"role": "assistant", "tool_calls": toolCalls}
		if content, _ := msg["content"].(string); content != "" {
			assistantMsg["content"] = content
		}
		messages = append(messages, assistantMsg)

		// Execute every tool call and append results
		for _, raw := range toolCalls {
			tc, _ := raw.(map[string]any)
			tcFunc, _ := tc["function"].(map[string]any)
			tcName, _ := tcFunc["name"].(string)
			tcArgsStr, _ := tcFunc["arguments"].(string)
			tcID, _ := tc["id"].(string)

			var tcArgs map[string]any
			json.Unmarshal([]byte(tcArgsStr), &tcArgs)

			toolResult, toolNode, toolErr := executeFunctionCall(ctx, tcName, tcArgs, tools, aw, signer, rc, checkBalance)
			if toolErr != nil {
				var blocked *ErrBalanceBlocked
				if errors.As(toolErr, &blocked) {
					return nil, toolErr
				}
			}
			resultStr := ""
			if toolErr != nil {
				resultStr = "error: " + toolErr.Error()
			} else {
				if m, ok := toolResult.(map[string]any); ok {
					if _, hasTx := m["txId"]; hasTx {
						x402Payments = append(x402Payments, collectX402Receipt(tcName, m, tools))
					}
				}
				if BillableFlatFee(toolNode.Type, toolNode.Template) {
					billedFlatFeeNodeIds = append(billedFlatFeeNodeIds, toolNode.ID)
				}
				b, _ := json.Marshal(toolResult)
				resultStr = string(b)
			}

			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": tcID,
				"content":      resultStr,
			})
		}
	}

	return nil, fmt.Errorf("agent exceeded maximum tool call iterations (%d)", maxToolIterations)
}

// ─── Anthropic ────────────────────────────────────────────────────────────────

func callAnthropic(ctx context.Context, agent models.WorkflowNode, provider models.WorkflowNode, rc RunContexter, platformKeys map[string]string) (any, error) {
	model := provider.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	apiKey, err := resolveAPIKey(provider, platformKeys)
	if err != nil {
		return nil, err
	}

	type anthMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages":   []anthMsg{{Role: "user", Content: rc.UserInput()}},
	}
	if agent.SystemPrompt != "" {
		payload["system"] = agent.SystemPrompt
	}

	headers := map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	}
	resp, err := postLLMJSON(ctx, "https://api.anthropic.com/v1/messages", headers, payload)
	if err != nil {
		return nil, err
	}

	var tokensIn, tokensOut int
	if usage, ok := resp["usage"].(map[string]any); ok {
		if v, ok := usage["input_tokens"].(float64); ok {
			tokensIn = int(v)
		}
		if v, ok := usage["output_tokens"].(float64); ok {
			tokensOut = int(v)
		}
	}

	contentArr, _ := resp["content"].([]any)
	for _, c := range contentArr {
		cm, _ := c.(map[string]any)
		if cm["type"] == "text" {
			text, _ := cm["text"].(string)
			if provider.KeyMode == "platform" {
				return platformKeyUsageResult(provider, tokensIn, tokensOut, map[string]any{"message": text}), nil
			}
			return text, nil
		}
	}
	return nil, fmt.Errorf("no text block in Anthropic response")
}
