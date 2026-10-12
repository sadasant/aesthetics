// Package evaluate turns applicable guidelines into decision questions,
// asks the configured provider, and maps the typed answers to outcomes.
//
// Each artifact is evaluated in two phases. Phase one asks, for every
// applicable guideline, the probability that the artifact meets it. Phase
// two asks follow-up questions about the failures only: where the shortfall
// shows (evidence), which of the guideline's remedies fits best, whether a
// diff introduced it, and whether it yields to a dominant guideline.
package evaluate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"
)

// Settings tune evaluation.
type Settings struct {
	Thresholds  report.Thresholds
	Concurrency int
	Evidence    bool
}

// Engine evaluates artifacts.
type Engine struct {
	WS       *artifact.Workspace
	Lib      *guideline.Library
	Layers   []guideline.Layer
	Provider decision.Provider // nil when only planning
	Limits   decision.Info     // state and question limits
	Settings Settings
}

func (e *Engine) maxState() int {
	if e.Limits.MaxStateChars > 0 {
		return e.Limits.MaxStateChars
	}
	return 60000
}

func (e *Engine) maxQuestions() int {
	if e.Limits.MaxQuestions > 0 {
		return e.Limits.MaxQuestions
	}
	return 8
}

// Plan is everything needed to evaluate one artifact.
type Plan struct {
	Artifact   *artifact.Artifact
	Resolution *guideline.Resolution
	State      decision.State
	Info       *StateInfo
	Questions  []decision.Question
	Err        error // the state could not be built
}

// Plan resolves the applicable guidelines and builds the state and
// questions. Plans without applicable guidelines have no state.
func (e *Engine) Plan(a *artifact.Artifact) (*Plan, error) {
	res, err := guideline.Resolve(e.Lib, e.Layers, a.Target())
	if err != nil {
		return nil, err
	}
	p := &Plan{Artifact: a, Resolution: res}
	if len(res.Applicable) == 0 {
		return p, nil
	}
	p.State, p.Info, p.Err = e.buildState(a, res.Context)
	if p.Err == nil {
		for _, g := range res.Applicable {
			p.Questions = append(p.Questions, metQuestion(a, g))
		}
	}
	return p, nil
}

func subject(a *artifact.Artifact) string {
	switch {
	case a.Kind == guideline.Diff:
		return "Consider the change set shown in `diff`. Judge the change itself, not code it leaves untouched."
	case a.Kind == guideline.Repository:
		return "Consider the repository as a whole, shown through `tree` and its key files."
	case a.Kind == guideline.Directory:
		return fmt.Sprintf("Consider the directory %s: its structure in `tree` and the files it contains.", a.Path)
	case a.Change != nil:
		return fmt.Sprintf("Consider the file %s as it stands after the change: `content` is the whole file after the change and `diff` shows what changed. Judge the whole file, including parts the change did not touch.", a.Path)
	}
	return fmt.Sprintf("Consider the file %s shown in `content`, keeping its role (`artifact.role`) in mind.", a.Path)
}

func metQuestion(a *artifact.Artifact, g *guideline.Resolved) decision.Question {
	var b strings.Builder
	b.WriteString(subject(a))
	b.WriteString("\nGuideline: " + g.Text)
	if g.Instructions != "" {
		b.WriteString("\nInstructions: " + g.Instructions)
	}
	b.WriteString("\nDoes the artifact meet this guideline?")
	return decision.Question{
		ID:           fmt.Sprintf("g%d", g.Rank),
		Type:         decision.Noul,
		Instructions: b.String(),
		True:         "The guideline is met.",
		False:        "The guideline is not met: there is a specific shortfall a careful reviewer would point out.",
	}
}

// Run evaluates the plans concurrently and returns results in plan order.
func (e *Engine) Run(ctx context.Context, plans []*Plan) []report.ArtifactResult {
	out := make([]report.ArtifactResult, len(plans))
	n := max(1, e.Settings.Concurrency)
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i, p := range plans {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = e.Evaluate(ctx, p)
		}()
	}
	wg.Wait()
	return out
}

// Evaluate evaluates one plan. Failures are recorded in the result and
// never affect other artifacts.
func (e *Engine) Evaluate(ctx context.Context, p *Plan) report.ArtifactResult {
	a := p.Artifact
	r := report.ArtifactResult{Kind: a.Kind, Path: a.Path, Label: a.Label(), Status: report.StatusComplete, Context: p.Resolution.Context, Facts: map[string]any{}}
	if p.Info != nil {
		for k, v := range p.Info.Facts {
			r.Facts[k] = v
		}
		if p.Info.Truncated {
			r.Notes = append(r.Notes, fmt.Sprintf("Context was truncated to fit the provider limit of %d characters; judgments may miss content.", e.maxState()))
		}
	}
	r.Facts["guidelines"] = len(p.Resolution.Applicable)
	criteria := make([]report.Criterion, len(p.Resolution.Applicable))
	for i, g := range p.Resolution.Applicable {
		criteria[i] = report.Criterion{
			Guideline: g.ID, Origin: g.Origin(), Text: g.Text, Severity: g.Severity, Priority: g.Priority, Rank: g.Rank,
			Outcome: report.Indeterminate,
		}
	}
	r.Results = criteria
	if p.Err != nil {
		r.Status, r.Error = report.StatusError, p.Err.Error()
		if errors.Is(p.Err, artifact.ErrSkipped) {
			r.Status = report.StatusSkipped
		}
		for i := range criteria {
			criteria[i].Explanation = "The artifact could not be read, so no outcome could be established."
			criteria[i].Error = p.Err.Error()
		}
		return r
	}

	answers, failed := e.ask(ctx, p.State, p.Questions)
	for i, g := range p.Resolution.Applicable {
		e.applyMet(&criteria[i], answers[fmt.Sprintf("g%d", g.Rank)], failed[fmt.Sprintf("g%d", g.Rank)])
	}
	nFailed := len(failed)
	if nFailed == len(p.Questions) {
		r.Status = report.StatusError
		r.Error = firstError(failed)
		return r
	}
	if nFailed > 0 {
		r.Status = report.StatusPartial
		r.Notes = append(r.Notes, fmt.Sprintf("%d of %d guidelines could not be evaluated: %s", nFailed, len(p.Questions), firstError(failed)))
	}

	if note := e.followUp(ctx, p, criteria); note != "" {
		r.Status = report.StatusPartial
		r.Notes = append(r.Notes, note)
	}
	return r
}

func firstError(m map[string]error) string {
	for _, err := range m {
		return err.Error()
	}
	return ""
}

// ask sends questions in batches, collecting answers and per-question errors.
func (e *Engine) ask(ctx context.Context, state decision.State, qs []decision.Question) (map[string]decision.Answer, map[string]error) {
	answers := map[string]decision.Answer{}
	failed := map[string]error{}
	size := e.maxQuestions()
	for start := 0; start < len(qs); start += size {
		batch := qs[start:min(len(qs), start+size)]
		resp, err := e.Provider.Decide(ctx, decision.Request{State: state, Questions: batch})
		for _, q := range batch {
			if err != nil {
				failed[q.ID] = err
				continue
			}
			if a, ok := resp.Answers[q.ID]; ok {
				answers[q.ID] = a
			}
		}
	}
	return answers, failed
}

func pct(p float64) string { return fmt.Sprintf("%.2f", p) }

func (e *Engine) applyMet(c *report.Criterion, a decision.Answer, err error) {
	t := e.Settings.Thresholds
	switch {
	case err != nil:
		c.Error = err.Error()
		c.Explanation = "The evaluation request failed, so no outcome could be established."
		return
	case a.Refused:
		c.Explanation = "The decision model declined to answer, so no outcome could be established."
		return
	case a.Type != decision.Noul:
		c.Explanation = "The provider returned no answer for this guideline, so no outcome could be established."
		return
	}
	p := a.Probability
	c.Probability = &p
	switch {
	case p >= t.Pass:
		c.Outcome = report.Pass
		c.Explanation = fmt.Sprintf("The decision model estimated a probability of %s that the guideline is met (pass at %s or more).", pct(p), pct(t.Pass))
	case p <= t.Fail:
		c.Outcome = report.Fail
		c.Explanation = fmt.Sprintf("The decision model estimated a probability of only %s that the guideline is met (fail at %s or less).", pct(p), pct(t.Fail))
	default:
		c.Explanation = fmt.Sprintf("The decision model estimated a probability of %s that the guideline is met, between the fail (%s) and pass (%s) thresholds, so no outcome could be established.", pct(p), pct(t.Fail), pct(t.Pass))
	}
}

// followUp enriches failures with recommendations, evidence, attribution,
// and tension resolution. It returns a note when follow-up questions fail.
func (e *Engine) followUp(ctx context.Context, p *Plan, criteria []report.Criterion) string {
	a := p.Artifact
	var qs []decision.Question
	for i, g := range p.Resolution.Applicable {
		c := &criteria[i]
		if c.Outcome != report.Fail {
			continue
		}
		c.Recommendation, c.RecommendationBasis = g.Recommendation, "guideline"
		if a.Kind == guideline.Diff {
			c.Attribution = &report.Attribution{Value: "introduced", Basis: "scope"}
		}
		intro := subject(a) + "\nThe artifact does not meet this guideline: " + g.Text
		if len(g.Remedies) == 1 {
			c.Recommendation = g.Remedies[0]
		}
		if len(g.Remedies) > 1 {
			q := decision.Question{ID: fmt.Sprintf("r%d", g.Rank), Type: decision.Choice,
				Instructions: intro + "\nWhich change would most directly bring the artifact in line with the guideline?"}
			for j, rem := range g.Remedies {
				q.Options = append(q.Options, decision.Option{Value: fmt.Sprintf("remedy_%d", j+1), Description: rem})
			}
			qs = append(qs, q)
		}
		if e.Settings.Evidence && len(p.Info.Locations) > 1 {
			q := decision.Question{ID: fmt.Sprintf("e%d", g.Rank), Type: decision.Choice,
				Instructions: intro + "\nWhich part of the artifact most clearly shows the shortfall?"}
			for _, l := range p.Info.Locations {
				q.Options = append(q.Options, decision.Option{Value: l.Value, Description: locationDescription(l, p.Info.ContentKey)})
			}
			q.Options = append(q.Options, decision.Option{Value: "none", Description: "No single part; the shortfall concerns the artifact as a whole."})
			qs = append(qs, q)
		}
		if a.Change != nil && a.Change.Status == "added" {
			// Everything in a new file comes from the change: a mechanical fact.
			c.Attribution = &report.Attribution{Value: "introduced", Basis: "new file"}
		} else if a.Change != nil {
			qs = append(qs, decision.Question{ID: fmt.Sprintf("a%d", g.Rank), Type: decision.Choice,
				Instructions: intro + "\nDid the change shown in `diff` introduce or worsen this shortfall, or did it already exist before the change?",
				Options: []decision.Option{
					{Value: "introduced", Description: "Lines added or modified by the change introduce or worsen the shortfall."},
					{Value: "preexisting", Description: "The shortfall already existed before the change; the change neither introduced nor worsened it."},
				}})
		}
		for _, t := range p.Resolution.Tensions {
			if t.Yielding != g.ID {
				continue
			}
			dom := p.Resolution.Find(t.Dominant)
			qs = append(qs, decision.Question{ID: fmt.Sprintf("t%d_%d", g.Rank, dom.Rank), Type: decision.Noul,
				Instructions: subject(a) + "\nGuideline A: " + dom.Text + "\nGuideline B: " + g.Text +
					"\nFor this artifact, would fully meeting guideline B require compromising guideline A?",
				True:  "Meeting B here would require compromising A.",
				False: "B could be met here without compromising A.",
			})
		}
	}
	if len(qs) == 0 {
		return ""
	}
	answers, failed := e.ask(ctx, p.State, qs)
	for i, g := range p.Resolution.Applicable {
		c := &criteria[i]
		if c.Outcome != report.Fail {
			continue
		}
		if ans, ok := answers[fmt.Sprintf("r%d", g.Rank)]; ok && ans.Type == decision.Choice {
			var j int
			if _, err := fmt.Sscanf(ans.Choice, "remedy_%d", &j); err == nil && j >= 1 && j <= len(g.Remedies) {
				prob := choiceProbability(ans)
				c.Recommendation, c.RecommendationBasis, c.RecommendationProbability = g.Remedies[j-1], "model", &prob
			}
		}
		if ans, ok := answers[fmt.Sprintf("e%d", g.Rank)]; ok && ans.Type == decision.Choice && ans.Choice != "none" {
			if prob := choiceProbability(ans); prob >= 0.4 {
				for _, l := range p.Info.Locations {
					if l.Value != ans.Choice {
						continue
					}
					ev := report.Evidence{Path: l.Path, StartLine: l.StartLine, EndLine: l.EndLine, Probability: prob}
					if a.Change != nil && l.StartLine > 0 {
						in := a.Change.AddedIn(l.StartLine, l.EndLine)
						ev.InChange = &in
					}
					c.Evidence = append(c.Evidence, ev)
				}
			}
		}
		if ans, ok := answers[fmt.Sprintf("a%d", g.Rank)]; ok && ans.Type == decision.Choice && (ans.Choice == "introduced" || ans.Choice == "preexisting") {
			prob := choiceProbability(ans)
			c.Attribution = &report.Attribution{Value: ans.Choice, Basis: "model", Probability: &prob}
		}
		for _, t := range p.Resolution.Tensions {
			if t.Yielding != g.ID || c.YieldedTo != "" {
				continue
			}
			dom := p.Resolution.Find(t.Dominant)
			if ans, ok := answers[fmt.Sprintf("t%d_%d", g.Rank, dom.Rank)]; ok && ans.Type == decision.Noul && ans.Probability >= 0.5 {
				prob := ans.Probability
				c.YieldedTo, c.YieldProbability = dom.ID, &prob
				c.Explanation += fmt.Sprintf(" Meeting it would likely (p=%s) compromise the higher-ranked guideline %s, which governs, so this failure does not count.", pct(prob), dom.ID)
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Sprintf("Follow-up questions about failures could not be answered (%s); recommendations fall back to the guidelines' own and attribution is unknown.", firstError(failed))
	}
	return ""
}

func choiceProbability(a decision.Answer) float64 {
	if p, ok := a.Probabilities[a.Choice]; ok {
		return p
	}
	return a.Confidence
}

func locationDescription(l Location, contentKey string) string {
	if l.StartLine > 0 {
		return fmt.Sprintf("Lines %d to %d of `%s`.", l.StartLine, l.EndLine, contentKey)
	}
	return "The entry " + l.Path + "."
}
