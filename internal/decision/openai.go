package decision

import (
	"context"
	"fmt"
	"strings"
)

// openAIProvider speaks the OpenAI Decisions API (POST /v1/decisions).
type openAIProvider struct {
	info Info
	http *httpClient
}

func (p *openAIProvider) Info() Info { return p.info }

type openAIQuestion struct {
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Instructions string         `json:"instructions"`
	Choices      []openAIOption `json:"choices,omitempty"`
}

type openAIOption struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type openAIAnswer struct {
	Type          string  `json:"type"`
	Name          string  `json:"name"`
	Probability   float64 `json:"probability"`
	Choice        any     `json:"choice"`
	Confidence    float64 `json:"confidence"`
	Probabilities []struct {
		Value       any     `json:"value"`
		Probability float64 `json:"probability"`
	} `json:"probabilities"`
}

func (p *openAIProvider) Decide(ctx context.Context, req Request) (*Response, error) {
	questions := make([]openAIQuestion, 0, len(req.Questions))
	for _, q := range req.Questions {
		oq := openAIQuestion{Name: q.ID, Instructions: q.Instructions}
		switch q.Type {
		case Noul:
			// Predicates have no criteria field; state what yes and no mean inline.
			oq.Type = "predicate"
			var b strings.Builder
			b.WriteString(q.Instructions)
			if q.True != "" {
				b.WriteString("\nTrue means: " + q.True)
			}
			if q.False != "" {
				b.WriteString("\nFalse means: " + q.False)
			}
			oq.Instructions = b.String()
		case Choice:
			oq.Type = "choice"
			for _, o := range q.Options {
				oq.Choices = append(oq.Choices, openAIOption(o))
			}
		}
		questions = append(questions, oq)
	}
	body := map[string]any{"model": p.info.Model, "input": req.State.Text(), "questions": questions}
	var out struct {
		Model   string         `json:"model"`
		Answers []openAIAnswer `json:"answers"`
		Usage   Usage          `json:"usage"`
	}
	if err := p.http.post(ctx, p.info.Endpoint, body, &out); err != nil {
		return nil, err
	}
	resp := &Response{Model: out.Model, Usage: out.Usage, Answers: map[string]Answer{}}
	for i, a := range out.Answers {
		name := a.Name
		if name == "" && i < len(req.Questions) {
			name = req.Questions[i].ID // answers come back in question order
		}
		switch a.Type {
		case "predicate":
			resp.Answers[name] = Answer{Type: Noul, Probability: a.Probability}
		case "choice":
			ans := Answer{Type: Choice, Choice: fmt.Sprint(a.Choice), Confidence: a.Confidence, Probabilities: map[string]float64{}}
			for _, pr := range a.Probabilities {
				ans.Probabilities[fmt.Sprint(pr.Value)] = pr.Probability
			}
			resp.Answers[name] = ans
		case "refusal":
			resp.Answers[name] = Answer{Refused: true}
		}
	}
	return resp, nil
}
