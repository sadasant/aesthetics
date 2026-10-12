package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"time"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/config"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/evaluate"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"
)

type checkOptions struct {
	common
	diff                bool
	base                string
	staged              bool
	scope               listFlag
	failOn              string
	failOnIndeterminate bool
	failOnIncomplete    bool
	failOnPreexisting   bool
	concurrency         int
	dryRun              bool
	record              string
	verbose             bool
}

func runCheck(args []string, stdout, stderr io.Writer) (int, error) {
	var o checkOptions
	fs := newFlagSet("check", "check [paths...] [options]", stderr)
	o.register(fs)
	fs.BoolVar(&o.diff, "diff", false, "evaluate Git changes (staged, unstaged, and untracked) instead of complete artifacts")
	fs.StringVar(&o.base, "base", "", "with --diff: evaluate the changes on HEAD since it diverged from this ref (e.g. origin/main); implies --diff")
	fs.BoolVar(&o.staged, "staged", false, "with --diff: evaluate only staged changes; implies --diff")
	fs.Var(&o.scope, "scope", "artifact kinds to evaluate, comma-separated: file, directory, repository, diff")
	fs.StringVar(&o.failOn, "fail-on", "", "lowest severity whose failures fail the run: error, warning, info, never (default from config, else warning)")
	fs.BoolVar(&o.failOnIndeterminate, "fail-on-indeterminate", false, "also fail the run on indeterminate results at or above the threshold")
	fs.BoolVar(&o.failOnIncomplete, "fail-on-incomplete", true, "exit 3 when some evaluations could not be completed")
	fs.BoolVar(&o.failOnPreexisting, "fail-on-preexisting", false, "with --diff: also fail on problems that predate the change")
	fs.IntVar(&o.concurrency, "concurrency", 0, "artifacts evaluated in parallel (default from config, else 4)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "show what would be evaluated without contacting the provider")
	fs.StringVar(&o.record, "record", "", "write a reproducibility record (inputs, guidelines, requests, answers) to this JSON file")
	fs.BoolVar(&o.verbose, "verbose", false, "list passing results and facts")
	fs.BoolVar(&o.verbose, "v", false, "shorthand for --verbose")
	paths, err := parse(fs, args)
	if err != nil {
		return report.ExitUsage, err
	}
	if o.base != "" || o.staged {
		o.diff = true
	}
	if o.base != "" && o.staged {
		return report.ExitUsage, usageError{fmt.Errorf("--base and --staged cannot be combined")}
	}
	s, err := o.open()
	if err != nil {
		return report.ExitUsage, err
	}
	if !s.hasGuidelines() {
		return report.ExitUsage, usageError{fmt.Errorf("no guidelines are active: activate presets with --use (for example --use unix,kiss) or define guidelines in .aesthetics.yaml (see `aesthetics init`)")}
	}

	policy, err := s.cfg.Policy()
	if err != nil {
		return report.ExitUsage, err
	}
	flagSet := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { flagSet[f.Name] = true })
	if o.failOn != "" {
		if policy.FailOn, err = config.ParseFailOn(o.failOn); err != nil {
			return report.ExitUsage, usageError{err}
		}
		policy.FailOnName = o.failOn
	}
	if flagSet["fail-on-indeterminate"] {
		policy.FailOnIndeterminate = o.failOnIndeterminate
	}
	if flagSet["fail-on-incomplete"] {
		policy.FailOnIncomplete = o.failOnIncomplete
	}
	if flagSet["fail-on-preexisting"] {
		policy.FailOnPreexisting = o.failOnPreexisting
	}

	kinds, err := parseKinds(o.scope)
	if err != nil {
		return report.ExitUsage, err
	}
	rep := &report.Report{
		Schema: report.Schema, Tool: report.Tool{Name: "aesthetics", Version: Version},
		Run: report.Run{StartedAt: time.Now().UTC(), Root: s.ws.Root, Config: s.cfg.Path, Presets: s.cfg.Presets(s.use),
			Thresholds: s.cfg.Thresholds(), Policy: policy, DryRun: o.dryRun},
	}
	if rep.Run.Presets == nil {
		rep.Run.Presets = []string{}
	}

	var artifacts []*artifact.Artifact
	if o.diff {
		var d *artifact.Diff
		artifacts, d, err = diffArtifacts(s, paths, o.base, o.staged, kinds)
		if err != nil {
			return report.ExitUsage, err
		}
		rep.Run.Diff = &report.DiffInfo{Label: d.Label, Files: len(d.Files), Additions: d.Additions, Deletions: d.Deletions}
		if len(d.Files) == 0 {
			fmt.Fprintf(stderr, "No changes to evaluate (%s).\n", d.Label)
		}
	} else if artifacts, err = s.ws.Select(s.cwd, paths, kinds); err != nil {
		return report.ExitUsage, err
	}

	limits, err := decision.Describe(s.cfg.Provider)
	if err != nil && !o.dryRun {
		return report.ExitUsage, err
	}
	engine := &evaluate.Engine{WS: s.ws, Lib: s.lib, Layers: s.layers, Limits: limits, Settings: evaluate.Settings{
		Thresholds: rep.Run.Thresholds, Concurrency: firstPositive(o.concurrency, s.cfg.Evaluation.Concurrency, 4), Evidence: s.cfg.Evidence(),
	}}
	var plans []*evaluate.Plan
	notApplicable := 0
	for _, a := range artifacts {
		p, err := engine.Plan(a)
		if err != nil {
			return report.ExitUsage, err
		}
		if len(p.Resolution.Applicable) == 0 {
			notApplicable++
			continue
		}
		plans = append(plans, p)
	}

	w, closeOut, err := o.out(stdout)
	if err != nil {
		return report.ExitUsage, err
	}
	defer closeOut()

	if o.dryRun {
		return report.ExitOK, writePlan(w, o.format, plans, notApplicable, limits)
	}
	provider, err := decision.New(s.cfg.Provider)
	if err != nil {
		return report.ExitUsage, err
	}
	info := provider.Info()
	rep.Run.Provider = &info
	var recorder *decision.Recorder
	if o.record != "" {
		recorder = &decision.Recorder{Provider: provider}
		provider = recorder
	}
	engine.Provider = provider
	engine.Limits = info

	if len(plans) > 0 {
		fmt.Fprintf(stderr, "Evaluating %d artifact(s) with %s…\n", len(plans), info.Label())
	} else if len(artifacts) > 0 {
		fmt.Fprintln(stderr, "No guideline applies to the selected artifacts.")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep.Artifacts = engine.Run(ctx, plans)
	if rep.Artifacts == nil {
		rep.Artifacts = []report.ArtifactResult{}
	}
	rep.Run.FinishedAt = time.Now().UTC()
	rep.Finalize()

	if recorder != nil {
		if err := writeRecord(o.record, rep, s, plans, recorder); err != nil {
			fmt.Fprintf(stderr, "aesthetics: writing record: %v\n", err)
		}
	}
	if o.format == "json" {
		err = report.WriteJSON(w, rep)
	} else {
		report.WriteText(w, rep, report.TextOptions{Verbose: o.verbose, Color: o.colorful(w), Width: columns()})
	}
	if o.output != "" {
		fmt.Fprintf(stderr, "Result: %s (exit %d); report written to %s\n", rep.Summary.Status, rep.Summary.ExitCode, o.output)
	}
	return rep.Summary.ExitCode, err
}

func firstPositive(v ...int) int {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 1
}

// diffArtifacts returns the changed files (evaluated in the context of the
// change) followed by the diff itself.
func diffArtifacts(s *session, paths []string, base string, staged bool, kinds []guideline.Kind) ([]*artifact.Artifact, *artifact.Diff, error) {
	var rel []string
	for _, p := range paths {
		r, err := s.ws.Rel(s.cwd, p)
		if err != nil {
			return nil, nil, err
		}
		rel = append(rel, r)
	}
	d, err := s.ws.Diff(artifact.DiffOptions{Base: base, Staged: staged, Paths: rel})
	if err != nil {
		return nil, nil, err
	}
	want := func(k guideline.Kind) bool { return len(kinds) == 0 || slices.Contains(kinds, k) }
	var out []*artifact.Artifact
	classes := map[string]bool{}
	for _, f := range d.Files {
		for _, c := range artifact.Classify(f.Path) {
			classes[c] = true
		}
		if f.Status == "deleted" || f.Binary || s.ws.Excluded(f.Path) || !want(guideline.File) {
			continue
		}
		a := s.ws.FileArtifact(f.Path)
		a.Change = f
		out = append(out, a)
	}
	if len(d.Files) > 0 && want(guideline.Diff) {
		da := &artifact.Artifact{Kind: guideline.Diff, Diff: d}
		for _, c := range guideline.Classes {
			if classes[c] {
				da.Classes = append(da.Classes, c)
			}
		}
		out = append(out, da)
	}
	return out, d, nil
}

func writePlan(w io.Writer, format string, plans []*evaluate.Plan, notApplicable int, limits decision.Info) error {
	type item struct {
		Kind       guideline.Kind `json:"kind"`
		Path       string         `json:"path"`
		Guidelines []string       `json:"guidelines"`
		Questions  int            `json:"questions"`
		StateChars int            `json:"state_chars"`
		Truncated  bool           `json:"truncated"`
		Error      string         `json:"error,omitempty"`
	}
	var items []item
	for _, p := range plans {
		it := item{Kind: p.Artifact.Kind, Path: p.Artifact.Label(), Questions: len(p.Questions)}
		for _, g := range p.Resolution.Applicable {
			it.Guidelines = append(it.Guidelines, g.ID)
		}
		if p.Info != nil {
			it.StateChars, it.Truncated = p.Info.Chars, p.Info.Truncated
		}
		if p.Err != nil {
			it.Error = p.Err.Error()
		}
		items = append(items, it)
	}
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"dry_run": true, "artifacts": items, "not_applicable": notApplicable, "limits": limits})
	}
	fmt.Fprintf(w, "Dry run: %d artifact(s) would be evaluated (%d have no applicable guideline).\n", len(items), notApplicable)
	for _, it := range items {
		fmt.Fprintf(w, "\n%s · %s · %d guideline(s), %d chars of context", it.Path, it.Kind, len(it.Guidelines), it.StateChars)
		if it.Truncated {
			fmt.Fprint(w, " (truncated)")
		}
		fmt.Fprintln(w)
		if it.Error != "" {
			fmt.Fprintf(w, "  cannot evaluate: %s\n", it.Error)
		}
		for _, g := range it.Guidelines {
			fmt.Fprintf(w, "  %s\n", g)
		}
	}
	return nil
}

func writeRecord(path string, rep *report.Report, s *session, plans []*evaluate.Plan, rec *decision.Recorder) error {
	type planRecord struct {
		Artifact   string                `json:"artifact"`
		Resolution *guideline.Resolution `json:"resolution"`
	}
	var pr []planRecord
	for _, p := range plans {
		pr = append(pr, planRecord{Artifact: p.Artifact.Label(), Resolution: p.Resolution})
	}
	data, err := json.MarshalIndent(map[string]any{
		"tool":      rep.Tool,
		"run":       rep.Run,
		"plans":     pr,
		"exchanges": rec.Exchanges(),
		"summary":   rep.Summary,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
