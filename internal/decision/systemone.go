package decision

import (
	"context"
	"encoding/json"
	"fmt"
)

// systemOneProvider speaks the System One protocol introduced by TypeSafe
// (POST /v1/systemone with a state and a map of typed questions). TypeSafe
// Jev, Cloudflare Clef on Workers AI, self-hosted laya-serve, and OpenRouter
// all accept this shape.
type systemOneProvider struct {
	info  Info
	http  *httpClient
	model string
}

func (p *systemOneProvider) Info() Info { return p.info }

func (p *systemOneProvider) Decide(ctx context.Context, req Request) (*Response, error) {
	var raw json.RawMessage
	if err := p.http.post(ctx, p.info.Endpoint, systemOneBody(p.model, req), &raw); err != nil {
		return nil, err
	}
	return parseSystemOne(raw)
}

type systemOneRequest struct {
	Model     string                       `json:"model,omitempty"`
	State     State                        `json:"state"`
	Questions map[string]systemOneQuestion `json:"questions"`
}

type systemOneQuestion struct {
	Type         QuestionType `json:"type"`
	Instructions string       `json:"instructions"`
	Criteria     any          `json:"criteria,omitempty"`
}

func systemOneBody(model string, req Request) systemOneRequest {
	body := systemOneRequest{Model: model, State: req.State, Questions: map[string]systemOneQuestion{}}
	for _, q := range req.Questions {
		sq := systemOneQuestion{Type: q.Type, Instructions: q.Instructions}
		switch q.Type {
		case Noul:
			if q.True != "" || q.False != "" {
				sq.Criteria = map[string]string{"true": q.True, "false": q.False}
			}
		case Choice:
			criteria := map[string]string{}
			for _, o := range q.Options {
				criteria[o.Value] = o.Description
			}
			sq.Criteria = criteria
		}
		body.Questions[q.ID] = sq
	}
	return body
}

type systemOneAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Probability   *float64           `json:"probability"`
	Choice        any                `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// parseSystemOne decodes a System One response. It also unwraps the
// envelopes used by Cloudflare ({"result": ...}) and by aggregators
// ({"data": {"result": ...}}).
func parseSystemOne(raw json.RawMessage) (*Response, error) {
	for range 3 {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("invalid System One response: %w", err)
		}
		if _, ok := probe["answers"]; ok {
			break
		}
		if inner, ok := probe["result"]; ok {
			raw = inner
			continue
		}
		if inner, ok := probe["data"]; ok {
			raw = inner
			continue
		}
		if errs, ok := probe["errors"]; ok {
			return nil, fmt.Errorf("provider returned errors: %s", snippet(errs))
		}
		return nil, fmt.Errorf("System One response has no answers: %s", snippet(raw))
	}
	var body struct {
		Model   string                     `json:"model"`
		Answers map[string]systemOneAnswer `json:"answers"`
		Usage   Usage                      `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("invalid System One response: %w", err)
	}
	resp := &Response{Model: body.Model, Usage: body.Usage, Answers: map[string]Answer{}}
	for id, a := range body.Answers {
		switch {
		case a.Noul != nil:
			resp.Answers[id] = Answer{Type: Noul, Probability: *a.Noul}
		case a.Probability != nil && a.Choice == nil:
			resp.Answers[id] = Answer{Type: Noul, Probability: *a.Probability}
		case a.Choice != nil:
			resp.Answers[id] = Answer{Type: Choice, Choice: fmt.Sprint(a.Choice), Probabilities: a.Probabilities, Confidence: a.Confidence}
		case a.Type == "refusal":
			resp.Answers[id] = Answer{Refused: true}
		}
	}
	return resp, nil
}
