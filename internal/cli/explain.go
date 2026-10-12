package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/evaluate"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"
)

func runExplain(args []string, stdout, stderr io.Writer) (int, error) {
	var o common
	var diff, staged, showState, run bool
	var base string
	fs := newFlagSet("explain", "explain <path> [options]\n       aesthetics explain --diff [path] [options]", stderr)
	o.register(fs)
	fs.BoolVar(&diff, "diff", false, "explain the Git diff, or a changed file in the context of the diff")
	fs.StringVar(&base, "base", "", "with --diff: compare HEAD with this ref; implies --diff")
	fs.BoolVar(&staged, "staged", false, "with --diff: only staged changes; implies --diff")
	fs.BoolVar(&showState, "state", false, "also print the exact state and questions that would be sent")
	fs.BoolVar(&run, "evaluate", false, "also evaluate the artifact and show detailed results")
	paths, err := parse(fs, args)
	if err != nil {
		return report.ExitUsage, err
	}
	diff = diff || base != "" || staged
	if len(paths) > 1 || (len(paths) == 0 && !diff) {
		return report.ExitUsage, usageError{fmt.Errorf("explain takes exactly one path (use . for the repository)")}
	}
	s, err := o.open()
	if err != nil {
		return report.ExitUsage, err
	}

	var a *artifact.Artifact
	if diff {
		artifacts, d, err := diffArtifacts(s, nil, base, staged, nil)
		if err != nil {
			return report.ExitUsage, err
		}
		if len(paths) == 0 {
			if len(d.Files) == 0 {
				return report.ExitUsage, fmt.Errorf("no changes to explain (%s)", d.Label)
			}
			a = artifacts[len(artifacts)-1]
		} else {
			rel, err := s.ws.Rel(s.cwd, paths[0])
			if err != nil {
				return report.ExitUsage, err
			}
			for _, x := range artifacts {
				if x.Kind == guideline.File && x.Path == rel {
					a = x
				}
			}
			if a == nil {
				return report.ExitUsage, fmt.Errorf("%s has no evaluable changes in the diff (%s)", paths[0], d.Label)
			}
		}
	} else {
		rel, err := s.ws.Rel(s.cwd, paths[0])
		if err != nil {
			return report.ExitUsage, err
		}
		info, err := os.Stat(filepath.Join(s.ws.Root, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			return report.ExitUsage, err
		case rel == ".":
			a = s.ws.RepoArtifact()
		case info.IsDir():
			a = s.ws.DirArtifact(rel)
		default:
			a = s.ws.FileArtifact(rel)
		}
	}

	limits, providerErr := decision.Describe(s.cfg.Provider)
	engine := &evaluate.Engine{WS: s.ws, Lib: s.lib, Layers: s.layers, Limits: limits, Settings: evaluate.Settings{
		Thresholds: s.cfg.Thresholds(), Concurrency: 1, Evidence: s.cfg.Evidence(),
	}}
	plan, err := engine.Plan(a)
	if err != nil {
		return report.ExitUsage, err
	}

	var result *report.ArtifactResult
	if run && len(plan.Resolution.Applicable) > 0 {
		provider, err := decision.New(s.cfg.Provider)
		if err != nil {
			return report.ExitUsage, err
		}
		engine.Provider = provider
		r := engine.Evaluate(context.Background(), plan)
		result = &r
	}

	w, closeOut, err := o.out(stdout)
	if err != nil {
		return report.ExitUsage, err
	}
	defer closeOut()
	if o.format == "json" {
		return report.ExitOK, explainJSON(w, s, a, plan, limits, providerErr, showState, result)
	}
	explainText(w, s, a, plan, limits, providerErr, showState, result, report.TextOptions{Verbose: true, Color: o.colorful(w), Width: columns()})
	return report.ExitOK, nil
}

func artifactJSON(a *artifact.Artifact) map[string]any {
	m := map[string]any{"kind": a.Kind, "path": a.Path, "label": a.Label(), "classes": a.Classes}
	if a.Role != nil {
		m["role"] = a.Role
	}
	if a.Language != "" {
		m["language"] = a.Language
	}
	if a.Kind == guideline.File || a.Kind == guideline.Directory {
		m["depth"] = guideline.Depth(a.Path)
	}
	if a.Change != nil {
		m["change"] = a.Change
	}
	if a.Diff != nil {
		m["diff"] = a.Diff
	}
	return m
}

func explainJSON(w io.Writer, s *session, a *artifact.Artifact, p *evaluate.Plan, limits decision.Info, providerErr error, showState bool, result *report.ArtifactResult) error {
	out := map[string]any{
		"artifact":   artifactJSON(a),
		"config":     s.cfg.Path,
		"resolution": p.Resolution,
		"thresholds": s.cfg.Thresholds(),
	}
	if providerErr == nil {
		out["provider"] = limits
	} else {
		out["provider_error"] = providerErr.Error()
	}
	if p.Info != nil {
		out["context"] = map[string]any{"chars": p.Info.Chars, "truncated": p.Info.Truncated, "included": p.Info.Included, "facts": p.Info.Facts}
	}
	if p.Err != nil {
		out["error"] = p.Err.Error()
	}
	if showState {
		out["state"] = p.State
		out["questions"] = p.Questions
	}
	if result != nil {
		rep := &report.Report{Run: report.Run{StartedAt: time.Now().UTC()}, Artifacts: []report.ArtifactResult{*result}}
		policy, _ := s.cfg.Policy()
		rep.Run.Policy = policy
		rep.Finalize()
		out["result"] = rep.Artifacts[0]
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func explainText(w io.Writer, s *session, a *artifact.Artifact, p *evaluate.Plan, limits decision.Info, providerErr error, showState bool, result *report.ArtifactResult, topt report.TextOptions) {
	width := topt.Width
	if width <= 0 {
		width = 100
	}
	para := func(first, indent, text string) { fmt.Fprintln(w, report.Wrap(text, first, indent, width)) }
	fmt.Fprintf(w, "Artifact: %s\n", a.Label())
	details := []string{"kind: " + string(a.Kind)}
	if a.Role != nil {
		details = append(details, fmt.Sprintf("role: %s (%s)", a.Role.Name, a.Role.Description))
	}
	if a.Language != "" {
		details = append(details, "language: "+a.Language)
	}
	if len(a.Classes) > 0 {
		details = append(details, "content: "+strings.Join(a.Classes, ", "))
	}
	if a.Kind == guideline.File || a.Kind == guideline.Directory {
		details = append(details, fmt.Sprintf("depth: %d", guideline.Depth(a.Path)))
	}
	if a.Change != nil {
		details = append(details, fmt.Sprintf("change: %s (+%d -%d)", a.Change.Status, a.Change.Additions, a.Change.Deletions))
	}
	if a.Diff != nil {
		details = append(details, fmt.Sprintf("diff: %s, %d files (+%d -%d)", a.Diff.Label, len(a.Diff.Files), a.Diff.Additions, a.Diff.Deletions))
	}
	para("  ", "  ", strings.Join(details, " · "))
	fmt.Fprintln(w)

	if s.cfg.Path != "" {
		fmt.Fprintf(w, "Configuration: %s\n", s.cfg.Path)
	} else {
		fmt.Fprintln(w, "Configuration: none found (guidelines come from --use only)")
	}
	if providerErr != nil {
		fmt.Fprintf(w, "Provider: not configured (%v)\n", providerErr)
	} else {
		para("", "  ", fmt.Sprintf("Provider: %s · endpoint %s · up to %d characters of context and %d questions per request",
			limits.Label(), orDash(limits.Endpoint), limits.MaxStateChars, limits.MaxQuestions))
	}
	res := p.Resolution
	fmt.Fprintf(w, "Layers applied (in order): %s\n", orDash(strings.Join(res.Layers, "; ")))

	fmt.Fprintln(w, "\nApplicable guidelines (highest precedence first):")
	if len(res.Applicable) == 0 {
		fmt.Fprintln(w, "  none — this artifact would not be evaluated")
	}
	for _, g := range res.Applicable {
		fmt.Fprintf(w, "  %d. %s · %s · priority %d\n", g.Rank, g.ID, g.Severity, g.Priority)
		para("     ", "     ", g.Text)
		para("     ", "       ", fmt.Sprintf("from %s · introduced by %s · scope: %s", g.Origin(), strings.Join(g.Via, ", "), g.Scope.Describe()))
		if g.Instructions != "" {
			para("     ", "       ", "instructions: "+g.Instructions)
		}
		if len(g.Overridden) > 0 {
			fmt.Fprintf(w, "     specialized by: %s\n", strings.Join(g.Overridden, ", "))
		}
		para("     ", "       ", "on failure: "+g.Recommendation)
		if len(g.Remedies) > 1 {
			fmt.Fprintf(w, "     (the model picks the best of %d remedies)\n", len(g.Remedies))
		}
	}
	if len(res.Tensions) > 0 {
		fmt.Fprintln(w, "\nTensions (the higher-ranked guideline governs when they truly conflict):")
		for _, t := range res.Tensions {
			fmt.Fprintf(w, "  %s yields to %s\n", t.Yielding, t.Dominant)
		}
	}
	if len(res.Disabled) > 0 {
		fmt.Fprintln(w, "\nDisabled:")
		for _, d := range res.Disabled {
			fmt.Fprintf(w, "  %s (by %s)\n", d.ID, d.By)
		}
	}
	if len(res.OutOfScope) > 0 {
		fmt.Fprintln(w, "\nActive but out of scope for this artifact:")
		for _, g := range res.OutOfScope {
			fmt.Fprintf(w, "  %s — %s\n", g.ID, g.Scope.Describe())
		}
	}
	if len(res.Context) > 0 {
		fmt.Fprintln(w, "\nContext instructions from rules:")
		for _, c := range res.Context {
			fmt.Fprintf(w, "  - %s\n", c)
		}
	}
	for _, warn := range res.Warnings {
		fmt.Fprintf(w, "\nwarning: %s\n", warn)
	}

	if len(res.Applicable) > 0 {
		t := s.cfg.Thresholds()
		fmt.Fprintln(w, "\nEvaluation:")
		if p.Err != nil {
			fmt.Fprintf(w, "  cannot evaluate: %v\n", p.Err)
		} else {
			size := max(1, limits.MaxQuestions)
			fmt.Fprintf(w, "  %d yes/no question(s) in %d request(s): the probability that each guideline is met\n", len(p.Questions), (len(p.Questions)+size-1)/size)
			fmt.Fprintf(w, "  outcome: pass when that probability is at least %.2f, fail when at most %.2f, otherwise indeterminate\n", t.Pass, t.Fail)
			follow := []string{"the best-fitting remedy"}
			if s.cfg.Evidence() {
				follow = append(follow, "where the shortfall shows")
			}
			if a.Change != nil {
				follow = append(follow, "whether the change introduced it")
			}
			if len(res.Tensions) > 0 {
				follow = append(follow, "whether it yields to a dominant guideline")
			}
			fmt.Fprintf(w, "  failures trigger follow-up questions: %s\n", strings.Join(follow, ", "))
			trunc := "not truncated"
			if p.Info.Truncated {
				trunc = "truncated to fit the provider limit"
			}
			fmt.Fprintf(w, "  context sent: %d characters (%s)\n", p.Info.Chars, trunc)
			if len(p.Info.Included) > 0 {
				fmt.Fprintf(w, "  related files included: %s\n", strings.Join(p.Info.Included, ", "))
			}
		}
	}
	if showState && p.State != nil {
		fmt.Fprintln(w, "\nState:")
		fmt.Fprintln(w, p.State.Text())
		fmt.Fprintln(w, "\nQuestions:")
		b, _ := json.MarshalIndent(p.Questions, "", "  ")
		fmt.Fprintln(w, string(b))
	}
	if result != nil {
		rep := &report.Report{Tool: report.Tool{Name: "aesthetics", Version: Version}, Artifacts: []report.ArtifactResult{*result}}
		rep.Run.Policy, _ = s.cfg.Policy()
		if info := limits; providerErr == nil {
			rep.Run.Provider = &info
		}
		rep.Finalize()
		fmt.Fprintln(w, "\nResults:")
		report.WriteText(w, rep, topt)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
