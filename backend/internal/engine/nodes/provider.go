package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/agentmesh/backend/internal/models"
)

var openAIBaseURL  = "https://api.openai.com"
var groqBaseURL    = "https://api.groq.com/openai"
var mistralBaseURL = "https://api.mistral.ai"

func SetOpenAIBaseURL(u string) { openAIBaseURL = u }

func ExecuteAgent(ctx context.Context, node models.WorkflowNode, attach models.AttachConfig, rc RunContexter) (any, error) {
	if attach.Provider == nil {
		return rc.Message(), nil
	}
	p := attach.Provider
	switch p.Template {
	case "openai", "groq", "mistral":
		return callOpenAICompat(ctx, node, *p, rc)
	case "anthropic":
		return callAnthropic(ctx, node, *p, rc)
	case "gemini":
		return callGemini(ctx, node, *p, rc)
	default:
		return callOpenAICompat(ctx, node, *p, rc)
	}
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func callOpenAICompat(ctx context.Context, agent models.WorkflowNode, provider models.WorkflowNode, rc RunContexter) (string, error) {
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
	messages := []openAIMessage{}
	if agent.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: agent.SystemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: rc.Message()})
	payload := map[string]any{"model": model, "messages": messages}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM API %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty choices from LLM")
	}
	return result.Choices[0].Message.Content, nil
}

func callAnthropic(ctx context.Context, agent models.WorkflowNode, provider models.WorkflowNode, rc RunContexter) (string, error) {
	model := provider.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages":   []msg{{Role: "user", Content: rc.Message()}},
	}
	if agent.SystemPrompt != "" {
		payload["system"] = agent.SystemPrompt
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", provider.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Anthropic API %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	for _, c := range result.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("no text block in Anthropic response")
}

func callGemini(ctx context.Context, agent models.WorkflowNode, provider models.WorkflowNode, rc RunContexter) (string, error) {
	model := provider.Model
	if model == "" {
		model = "gemini-1.5-pro"
	}
	payload := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": rc.Message()}}},
		},
	}
	if agent.SystemPrompt != "" {
		payload["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": agent.SystemPrompt}},
		}
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, provider.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Gemini API %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct{ Text string `json:"text"` } `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}
