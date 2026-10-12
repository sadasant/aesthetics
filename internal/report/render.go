package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sadasant/aesthetics/internal/guideline"
)

// WriteJSON writes the machine-readable report.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

func prob(p *float64) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("p=%.2f", *p)
}

// TextOptions controls the human-readable report.
type TextOptions struct {
	Verbose bool // list passing results, facts, and model details
	Color   bool // use ANSI colors
	Width   int  // wrap prose at this many columns; 0 means 100
}

// wrap breaks s into lines of at most width columns, each prefixed by
// indent, with first replacing indent on the first line.
func wrap(s, first, indent string, width int) string {
	var b strings.Builder
	line := first
	empty := true
	for _, word := range strings.Fields(s) {
		if !empty && len([]rune(line))+1+len([]rune(word)) > width {
			b.WriteString(line + "\n")
			line, empty = indent, true
		}
		if !empty {
			line += " "
		}
		line += word
		empty = false
	}
	b.WriteString(line)
	return b.String()
}

type painter bool

// paint colors each line separately, so colors survive renderers that
// reset styles at line breaks.
func (c painter) paint(code, s string) string {
	if !c || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "\x1b[" + code + "m" + l + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

func (c painter) bold(s string) string  { return c.paint("1", s) }
func (c painter) dim(s string) string   { return c.paint("90", s) }
func (c painter) red(s string) string   { return c.paint("31", s) }
func (c painter) green(s string) string { return c.paint("32", s) }
func (c painter) amber(s string) string { return c.paint("33", s) }

func (c painter) mark(o Outcome) string {
	switch o {
	case Pass:
		return c.green("✓")
	case Fail:
		return c.red("✗")
	}
	return c.amber("?")
}

// WriteText writes the human-readable report. Passing results are counted,
// and listed individually only in verbose mode.
func WriteText(w io.Writer, r *Report, opt TextOptions) {
	c := painter(opt.Color)
	if opt.Width <= 0 {
		opt.Width = 100
	}
	header := []string{c.bold(r.Tool.Name + " " + r.Tool.Version)}
	if p := r.Run.Provider; p != nil {
		header = append(header, p.Label())
	}
	if len(r.Run.Presets) > 0 {
		header = append(header, strings.Join(r.Run.Presets, ", "))
	}
	if d := r.Run.Diff; d != nil {
		files := "files"
		if d.Files == 1 {
			files = "file"
		}
		header = append(header, fmt.Sprintf("diff %s, %d %s, +%d −%d", d.Label, d.Files, files, d.Additions, d.Deletions))
	}
	fmt.Fprintln(w, strings.Join(header, c.dim(" · ")))

	width := 0
	for _, a := range r.Artifacts {
		for _, x := range a.Results {
			width = max(width, len(x.Guideline))
		}
	}
	for _, a := range r.Artifacts {
		fmt.Fprintln(w)
		kind := string(a.Kind)
		if role, ok := a.Facts["role"].(string); ok {
			kind = role
		}
		fmt.Fprintf(w, "%s  %s\n", c.bold(a.Label), c.dim(kind))
		switch a.Status {
		case StatusError:
			fmt.Fprintf(w, "  %s could not be evaluated: %s\n", c.red("!"), a.Error)
		case StatusSkipped:
			fmt.Fprintf(w, "  %s skipped: %s\n", c.amber("-"), a.Error)
		}
		for _, n := range a.Notes {
			fmt.Fprintln(w, wrap(n, "  "+c.amber("note")+" ", "       ", opt.Width))
		}
		if opt.Verbose && len(a.Facts) > 0 {
			fmt.Fprintf(w, "  %s\n", c.dim("facts "+facts(a.Facts)))
		}
		if a.Status == StatusError || a.Status == StatusSkipped {
			continue
		}
		passed := 0
		for _, x := range a.Results {
			if x.Outcome == Pass && !opt.Verbose {
				passed++
				continue
			}
			writeCriterion(w, c, x, width, opt)
		}
		if passed > 0 {
			fmt.Fprintf(w, "  %s %s\n", c.mark(Pass), c.dim(fmt.Sprintf("%d passed", passed)))
		}
	}

	s := r.Summary
	fmt.Fprintln(w)
	tint := func(n int, color func(string) string, label string) string {
		if n == 0 {
			return fmt.Sprintf("%d %s", n, label)
		}
		return color(fmt.Sprintf("%d %s", n, label))
	}
	counts := []string{
		fmt.Sprintf("%d artifacts", s.Artifacts),
		fmt.Sprintf("%d criteria", s.Criteria),
		tint(s.Pass, c.green, "passed"),
		tint(s.Fail, c.red, "failed"),
		tint(s.Indeterminate, c.amber, "indeterminate"),
	}
	fmt.Fprintln(w, strings.Join(counts, c.dim(" · ")))
	var extra []string
	if n := s.Partial + s.Errors; n > 0 {
		extra = append(extra, fmt.Sprintf("%d incomplete", n))
	}
	if s.Skipped > 0 {
		extra = append(extra, fmt.Sprintf("%d skipped", s.Skipped))
	}
	if s.Introduced+s.Preexisting > 0 {
		extra = append(extra, fmt.Sprintf("%d introduced by the change, %d preexisting", s.Introduced, s.Preexisting))
	}
	if s.Yielded > 0 {
		extra = append(extra, fmt.Sprintf("%d yielded to higher-ranked guidelines", s.Yielded))
	}
	if len(extra) > 0 {
		fmt.Fprintln(w, c.dim(strings.Join(extra, " · ")))
	}
	switch s.Status {
	case "failed":
		fmt.Fprintf(w, "%s %d result(s) at or above %q %s\n", c.paint("1;31", "Failed:"), s.Counted, r.Run.Policy.FailOnName, c.dim(fmt.Sprintf("(exit %d)", s.ExitCode)))
	case "incomplete":
		fmt.Fprintf(w, "%s some evaluations could not be completed %s\n", c.paint("1;33", "Incomplete:"), c.dim(fmt.Sprintf("(exit %d)", s.ExitCode)))
	default:
		fmt.Fprintf(w, "%s %s\n", c.paint("1;32", "Passed"), c.dim(fmt.Sprintf("(exit %d)", s.ExitCode)))
	}
}

func writeCriterion(w io.Writer, c painter, x Criterion, width int, opt TextOptions) {
	verbose := opt.Verbose
	sev := fmt.Sprintf("%-7s", x.Severity)
	switch x.Severity {
	case guideline.Error:
		sev = c.red(sev)
	case guideline.Warning:
	default:
		sev = c.dim(sev)
	}
	fmt.Fprintf(w, "  %s %-*s  %s  %s\n", c.mark(x.Outcome), width, x.Guideline, sev, c.dim(prob(x.Probability)))
	pad := "    "
	fmt.Fprintln(w, wrap(x.Text, pad, pad, opt.Width))
	// Without a probability, the explanation says why there is no outcome.
	if verbose || x.Probability == nil {
		fmt.Fprintln(w, wrap(x.Explanation, pad, pad, opt.Width))
	}
	if x.Error != "" {
		fmt.Fprintf(w, "%s%s %s\n", pad, c.red("error"), x.Error)
	}
	if x.Recommendation != "" {
		basis := ""
		if verbose && x.RecommendationBasis == "model" {
			basis = c.dim(fmt.Sprintf(" (remedy chosen by the model, %s)", prob(x.RecommendationProbability)))
		}
		rec := wrap(x.Recommendation, pad+"→ ", pad+"  ", opt.Width)
		fmt.Fprintf(w, "%s%s\n", strings.Replace(rec, "→", c.paint("36", "→"), 1), basis)
	}
	for _, e := range x.Evidence {
		where := ""
		if e.InChange != nil {
			where = ", outside the changed lines"
			if *e.InChange {
				where = ", in the changed lines"
			}
		}
		detail := ""
		if verbose || where != "" {
			detail = c.dim(fmt.Sprintf(" (p=%.2f%s)", e.Probability, where))
		}
		fmt.Fprintf(w, "%s  at %s%s\n", pad, e, detail)
	}
	if at := x.Attribution; at != nil && at.Basis != "scope" {
		verb := map[string]string{"introduced": "introduced by the change", "preexisting": "predates the change"}[at.Value]
		detail := at.Basis
		if at.Probability != nil {
			detail += ", " + prob(at.Probability)
		}
		fmt.Fprintf(w, "%s  %s %s\n", pad, verb, c.dim("("+detail+")"))
	}
	if x.YieldedTo != "" {
		fmt.Fprintf(w, "%s  yields to %s %s\n", pad, x.YieldedTo, c.dim("("+prob(x.YieldProbability)+")"))
	}
	if x.Outcome == Fail && !x.Counted {
		reason := "below the failure threshold"
		switch {
		case x.YieldedTo != "":
			reason = "yields to a higher-ranked guideline"
		case x.Attribution != nil && x.Attribution.Value == "preexisting":
			reason = "predates the change"
		}
		fmt.Fprintf(w, "%s  %s\n", pad, c.dim("not counted: "+reason))
	}
}

func facts(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		b, _ := json.Marshal(m[k])
		parts = append(parts, k+"="+string(b))
	}
	return strings.Join(parts, " ")
}
