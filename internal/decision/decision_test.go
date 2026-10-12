package decision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testRequest = Request{
	State: State{{Key: "artifact", Value: map[string]string{"path": "a.go"}}, {Key: "content", Value: "1| package a"}},
	Questions: []Question{
		{ID: "g1", Type: Noul, Instructions: "Is it simple?", True: "met", False: "not met"},
		{ID: "e1", Type: Choice, Instructions: "Where?", Options: []Option{{"lines 1-10", "first"}, {"none", "whole"}}},
	},
}

func TestSystemOneProtocol(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		// Cloudflare wraps results in an envelope.
		io.WriteString(w, `{"success":true,"result":{"model":"clef","answers":{
			"g1":{"type":"noul","noul":0.25},
			"e1":{"type":"choice","choice":"lines 1-10","probabilities":{"lines 1-10":0.8,"none":0.2},"confidence":0.7}},
			"usage":{"input_tokens":10,"output_tokens":2}}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", "k")
	p, err := New(Config{Type: "systemone", BaseURL: srv.URL + "/v1", Model: "jev-latest", APIKeyEnv: "TEST_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Decide(context.Background(), testRequest)
	if err != nil {
		t.Fatal(err)
	}
	q := got["questions"].(map[string]any)
	noul := q["g1"].(map[string]any)
	if noul["type"] != "noul" || noul["criteria"].(map[string]any)["false"] != "not met" {
		t.Errorf("noul question sent as %v", noul)
	}
	if q["e1"].(map[string]any)["criteria"].(map[string]any)["none"] != "whole" {
		t.Errorf("choice criteria sent as %v", q["e1"])
	}
	if state := got["state"].(map[string]any); state["content"] != "1| package a" {
		t.Errorf("state sent as %v", state)
	}
	if a := resp.Answers["g1"]; a.Type != Noul || a.Probability != 0.25 {
		t.Errorf("noul answer = %+v", a)
	}
	if a := resp.Answers["e1"]; a.Choice != "lines 1-10" || a.Probabilities["none"] != 0.2 {
		t.Errorf("choice answer = %+v", a)
	}
}

func TestOpenAIDecisionsProtocol(t *testing.T) {
	var calls atomic.Int32
	var got struct {
		Model     string           `json:"model"`
		Input     string           `json:"input"`
		Questions []map[string]any `json:"questions"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // retried
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"model":"gpt-6-luna","answers":[
			{"type":"refusal","name":"g1"},
			{"type":"choice","name":"e1","choice":"none","probabilities":[{"value":"lines 1-10","probability":0.1},{"value":"none","probability":0.9}],"confidence":0.8}]}`)
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "k")
	p, err := New(Config{Type: "openai", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	p.(*openAIProvider).http.retryDelay = time.Millisecond
	resp, err := p.Decide(context.Background(), testRequest)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want a retry after 429", calls.Load())
	}
	if got.Model != "gpt-6-luna" || !strings.Contains(got.Input, `"content": "1| package a"`) {
		t.Errorf("model %q, input %q", got.Model, got.Input)
	}
	if got.Questions[0]["type"] != "predicate" || !strings.Contains(got.Questions[0]["instructions"].(string), "False means: not met") {
		t.Errorf("predicate sent as %v", got.Questions[0])
	}
	if !resp.Answers["g1"].Refused || resp.Answers["e1"].Probabilities["none"] != 0.9 {
		t.Errorf("answers = %+v", resp.Answers)
	}
}

func TestNewRequiresCredentialsAndModel(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	if _, err := New(Config{Type: "typesafe"}); err == nil || !strings.Contains(err.Error(), "TYPESAFE_API_KEY") {
		t.Errorf("missing key: %v", err)
	}
	if _, err := New(Config{Type: "chat"}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Errorf("missing model: %v", err)
	}
	t.Setenv("CLOUDFLARE_API_TOKEN", "k")
	p, err := New(Config{Type: "cloudflare", AccountID: "acct", Model: "clef-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://api.cloudflare.com/client/v4/accounts/acct/ai/run/@cf/cloudflare/clef-flash"; p.Info().Endpoint != want {
		t.Errorf("endpoint = %s", p.Info().Endpoint)
	}
	if p.(*systemOneProvider).model != "clef-flash" {
		t.Errorf("model selector = %s", p.(*systemOneProvider).model)
	}
}

func TestChatEmulatesDecisions(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		content := "```json\n{\"answers\": {\"g1\": {\"type\": \"noul\", \"noul\": 0.7}, \"e1\": {\"type\": \"choice\", \"choice\": \"none\", \"probabilities\": {\"none\": 1}}}}\n```"
		json.NewEncoder(w).Encode(map[string]any{"model": "llama", "choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "")
	p, err := New(Config{Type: "chat", BaseURL: srv.URL, Model: "llama"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Decide(context.Background(), testRequest)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answers["g1"].Probability != 0.7 || resp.Answers["e1"].Choice != "none" {
		t.Errorf("answers = %+v", resp.Answers)
	}
	if got["model"] != "llama" || len(got["messages"].([]any)) != 2 {
		t.Errorf("request = %v", got)
	}
}
