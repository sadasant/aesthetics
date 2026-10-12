package evaluate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"
)

// scripted answers noul questions by matching guideline text.
type scripted struct {
	met  map[string]float64 // guideline text fragment -> P(met)
	seen []decision.Question
}

func (s *scripted) Info() decision.Info { return decision.Info{Type: "test", MaxQuestions: 3} }

func (s *scripted) Decide(_ context.Context, req decision.Request) (*decision.Response, error) {
	resp := &decision.Response{Answers: map[string]decision.Answer{}}
	for _, q := range req.Questions {
		s.seen = append(s.seen, q)
		switch {
		case q.ID[0] == 't': // tension: meeting the yielding guideline would compromise the dominant one
			resp.Answers[q.ID] = decision.Answer{Type: decision.Noul, Probability: 0.8}
		case q.Type == decision.Noul:
			for frag, p := range s.met {
				if strings.Contains(q.Instructions, frag) {
					resp.Answers[q.ID] = decision.Answer{Type: decision.Noul, Probability: p}
				}
			}
		case q.ID[0] == 'r':
			last := q.Options[len(q.Options)-1].Value
			resp.Answers[q.ID] = decision.Answer{Type: decision.Choice, Choice: last, Probabilities: map[string]float64{last: 0.6}}
		}
	}
	return resp, nil
}

func TestEvaluateOutcomesAndTensions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	ws, err := artifact.Open(dir, artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	presets, _ := guideline.Presets()
	lib, _ := guideline.NewLibrary(presets...)
	layers := []guideline.Layer{{
		Name:    "base",
		Extends: []string{"kiss", "solid"},
		Guidelines: []guideline.Spec{
			{ID: "at-pass", Text: "BOUNDARY-PASS"},
			{ID: "at-fail", Text: "BOUNDARY-FAIL"},
			{ID: "unanswered", Text: "NO-ANSWER"},
		},
	}}
	prov := &scripted{met: map[string]float64{
		"BOUNDARY-PASS": 0.65, "BOUNDARY-FAIL": 0.35,
		"no abstractions, parameters": 0.1, // kiss/no-speculative-generality fails
		"extended by adding code":     0.9, // solid/open-closed passes
	}}
	e := &Engine{WS: ws, Lib: lib, Layers: layers, Provider: prov, Limits: prov.Info(),
		Settings: Settings{Thresholds: report.Thresholds{Pass: 0.65, Fail: 0.35}, Concurrency: 1}}
	p, err := e.Plan(ws.FileArtifact("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	rep := &report.Report{Run: report.Run{Policy: report.Policy{FailOn: guideline.Warning}}}
	rep.Artifacts = e.Run(context.Background(), []*Plan{p})
	rep.Finalize()

	got := map[string]report.Criterion{}
	for _, c := range rep.Artifacts[0].Results {
		got[c.Guideline] = c
	}
	if c := got["config/at-pass"]; c.Outcome != report.Pass {
		t.Errorf("P(met) at the pass threshold: %s", c.Outcome)
	}
	if c := got["config/at-fail"]; c.Outcome != report.Fail || !c.Counted || c.Recommendation == "" {
		t.Errorf("P(met) at the fail threshold: %+v", c)
	}
	if c := got["config/unanswered"]; c.Outcome != report.Indeterminate || c.Probability != nil {
		t.Errorf("missing answer: %+v", c)
	}
	nsg := got["kiss/no-speculative-generality"]
	// Both solid guidelines it conflicts with outrank it (declared later);
	// it yields to the highest-ranked one.
	if nsg.Outcome != report.Fail || nsg.YieldedTo != "solid/depend-on-abstractions" || nsg.Counted {
		t.Errorf("yielding failure: %+v", nsg)
	}
	if nsg.RecommendationBasis != "model" || nsg.Recommendation != "Delete extension points, hooks, and registries that nothing uses." {
		t.Errorf("remedy: %q (%s)", nsg.Recommendation, nsg.RecommendationBasis)
	}
	for _, q := range prov.seen[:len(p.Questions)] {
		if q.Type != decision.Noul {
			t.Errorf("phase one asked a %s question", q.Type)
		}
	}
	if rep.Summary.Yielded != 1 || rep.Artifacts[0].Status != report.StatusComplete {
		t.Errorf("summary = %+v, status %s", rep.Summary, rep.Artifacts[0].Status)
	}
}
