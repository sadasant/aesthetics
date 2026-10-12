package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// chatProvider emulates a decision model with any OpenAI-compatible chat
// completions endpoint (hosted LLMs, Ollama, vLLM, LM Studio, ...). The model
// is asked to reply with System One answers in JSON.
type chatProvider struct {
	info Info
	http *httpClient
}

func (p *chatProvider) Info() Info { return p.info }

const chatSystemPrompt = `You act as a calibrated decision model. You receive a JSON object with a "state" and a map of typed "questions".
Evaluate every question independently against the state and reply with only a JSON object of this shape:
{"answers": {"<question id>": <answer>}}
For a question of type "noul", the answer is {"type": "noul", "noul": <probability from 0 to 1 that the answer is yes>}.
For a question of type "choice", the answer is {"type": "choice", "choice": "<option key>", "probabilities": {"<option key>": <probability>, ...}} with probabilities summing to 1.
Report honest, calibrated probabilities: use values near 0.5 when the evidence is inconclusive.`

func (p *chatProvider) Decide(ctx context.Context, req Request) (*Response, error) {
	user, err := json.Marshal(systemOneBody("", req))
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":           p.info.Model,
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": chatSystemPrompt},
			{"role": "user", "content": string(user)},
		},
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := p.http.post(ctx, p.info.Endpoint, body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("chat completion returned no choices")
	}
	content := extractJSONObject(out.Choices[0].Message.Content)
	resp, err := parseSystemOne(json.RawMessage(content))
	if err != nil {
		return nil, fmt.Errorf("chat model did not return decision JSON: %w", err)
	}
	resp.Model = out.Model
	resp.Usage = Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}
	return resp, nil
}

// extractJSONObject strips code fences and surrounding prose.
func extractJSONObject(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
