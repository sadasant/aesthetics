// Command evals measures how well Aesthetics, with a given decision model,
// reproduces labeled judgments, and helps choose default thresholds.
//
//	go run ./evals -provider openai            # all suites in evals/cases
//	go run ./evals -provider openai -run kiss  # cases whose id contains "kiss"
//
// Provider responses are cached in evals/.cache, so reruns are free and
// stable; pass -no-cache to ask the provider again.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/evaluate"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"

	"go.yaml.in/yaml/v3"
)

// Case is one labeled evaluation scenario.
type Case struct {
	ID     string            `yaml:"id"`
	Kind   guideline.Kind    `yaml:"kind"`
	Target string            `yaml:"target,omitempty"` // file or directory; for diffs, a changed file
	Before map[string]string `yaml:"before,omitempty"` // diffs: committed files before the change
	Files  map[string]string `yaml:"files"`            // the working tree (after the change)
	Use    []string          `yaml:"use,omitempty"`
	// Guidelines are custom natural-language guidelines for the case.
	Guidelines []guideline.Spec `yaml:"guidelines,omitempty"`
	// Expect maps guideline IDs to pass, fail, or borderline.
	Expect map[string]string `yaml:"expect"`
	// Attribution maps guideline IDs to introduced or preexisting.
	Attribution map[string]string `yaml:"attribution,omitempty"`
	// Evidence maps guideline IDs to the [first, last] lines of the flaw.
	Evidence map[string][2]int `yaml:"evidence,omitempty"`
}

// Judgment is the outcome for one labeled guideline.
type Judgment struct {
	Case        string   `json:"case"`
	Guideline   string   `json:"guideline"`
	Expect      string   `json:"expect"`
	Probability *float64 `json:"probability"`
	Outcome     string   `json:"outcome"`
	Attribution string   `json:"attribution,omitempty"`
	WantAttrib  string   `json:"want_attribution,omitempty"`
	EvidenceHit *bool    `json:"evidence_hit,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func main() {
	providerType := flag.String("provider", "openai", "provider type")
	model := flag.String("model", "", "model (default: the provider's default)")
	dir := flag.String("cases", "evals/cases", "directory of case files")
	filter := flag.String("run", "", "only cases whose id contains this string")
	passT := flag.Float64("pass", 0.65, "pass threshold")
	failT := flag.Float64("fail", 0.35, "fail threshold")
	noCache := flag.Bool("no-cache", false, "ignore cached responses")
	out := flag.String("json", "", "write judgments as JSON to this file")
	flag.Parse()

	cases, err := loadCases(*dir, *filter)
	check(err)
	prov, err := decision.New(decision.Config{Type: *providerType, Model: *model})
	check(err)
	cached := &cache{Provider: prov, dir: "evals/.cache", skip: *noCache}
	presets, err := guideline.Presets()
	check(err)
	lib, err := guideline.NewLibrary(presets...)
	check(err)

	var mu sync.Mutex
	var all []Judgment
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			js := runCase(c, lib, cached, report.Thresholds{Pass: *passT, Fail: *failT})
			mu.Lock()
			all = append(all, js...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(all, func(i, j int) bool {
		return all[i].Case+all[i].Guideline < all[j].Case+all[j].Guideline
	})
	if *out != "" {
		data, _ := json.MarshalIndent(all, "", "  ")
		check(os.WriteFile(*out, data, 0o644))
	}
	printReport(all, *passT, *failT, prov.Info(), cached)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "evals:", err)
		os.Exit(1)
	}
}

func loadCases(dir, filter string) ([]Case, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var out []Case
	seen := map[string]bool{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var cs []Case
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		dec.KnownFields(true)
		if err := dec.Decode(&cs); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, c := range cs {
			if seen[c.ID] {
				return nil, fmt.Errorf("%s: duplicate case id %s", p, c.ID)
			}
			seen[c.ID] = true
			if filter == "" || strings.Contains(c.ID, filter) {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func writeFiles(root string, files map[string]string) error {
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func git(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-c", "user.email=e@e", "-c", "user.name=e", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %v: %s", args, err, out)
	}
	return nil
}

func fail(c Case, err error) []Judgment {
	var js []Judgment
	for id, e := range c.Expect {
		js = append(js, Judgment{Case: c.ID, Guideline: id, Expect: e, Outcome: "error", Error: err.Error()})
	}
	return js
}

// runCase materializes the case, evaluates its labeled guidelines with the
// real pipeline, and compares the results with the labels.
func runCase(c Case, lib *guideline.Library, prov decision.Provider, t report.Thresholds) []Judgment {
	root, err := os.MkdirTemp("", "aesthetics-eval-")
	if err != nil {
		return fail(c, err)
	}
	defer os.RemoveAll(root)

	if c.Kind == guideline.Diff || c.Before != nil {
		if err := writeFiles(root, c.Before); err != nil {
			return fail(c, err)
		}
		if err := git(root, "init", "-q"); err != nil {
			return fail(c, err)
		}
		if err := git(root, "add", "-A"); err != nil {
			return fail(c, err)
		}
		if err := git(root, "commit", "-q", "--allow-empty", "-m", "before"); err != nil {
			return fail(c, err)
		}
	}
	if err := writeFiles(root, c.Files); err != nil {
		return fail(c, err)
	}
	ws, err := artifact.Open(root, artifact.Options{Exclude: artifact.DefaultExcludes})
	if err != nil {
		return fail(c, err)
	}

	var a *artifact.Artifact
	switch c.Kind {
	case guideline.File:
		a = ws.FileArtifact(c.Target)
	case guideline.Directory:
		a = ws.DirArtifact(c.Target)
	case guideline.Repository:
		a = ws.RepoArtifact()
	case guideline.Diff:
		d, err := ws.Diff(artifact.DiffOptions{})
		if err != nil {
			return fail(c, err)
		}
		if c.Target == "" {
			a = &artifact.Artifact{Kind: guideline.Diff, Diff: d}
			seen := map[string]bool{}
			for _, f := range d.Files {
				for _, cl := range artifact.Classify(f.Path) {
					if !seen[cl] {
						seen[cl] = true
						a.Classes = append(a.Classes, cl)
					}
				}
			}
		} else {
			for _, f := range d.Files {
				if f.Path == c.Target {
					a = ws.FileArtifact(f.Path)
					a.Change = f
				}
			}
			if a == nil {
				return fail(c, fmt.Errorf("%s is not changed in the diff", c.Target))
			}
		}
	default:
		return fail(c, fmt.Errorf("unknown kind %q", c.Kind))
	}

	layers := []guideline.Layer{{Name: "base", Extends: c.Use, Guidelines: c.Guidelines}}
	e := &evaluate.Engine{WS: ws, Lib: lib, Layers: layers, Provider: prov, Limits: prov.Info(),
		Settings: evaluate.Settings{Thresholds: t, Concurrency: 1, Evidence: true}}
	plan, err := e.Plan(a)
	if err != nil {
		return fail(c, err)
	}
	keepOnlyLabeled(plan, c.Expect)

	var js []Judgment
	res := plan.Resolution
	for id, want := range c.Expect {
		if res.Find(id) == nil {
			js = append(js, Judgment{Case: c.ID, Guideline: id, Expect: want, Outcome: "error", Error: "guideline not applicable to the artifact"})
		}
	}
	if len(res.Applicable) == 0 {
		return js
	}
	result := e.Evaluate(context.Background(), plan)
	for _, r := range result.Results {
		j := Judgment{Case: c.ID, Guideline: r.Guideline, Expect: c.Expect[r.Guideline], Probability: r.Probability, Outcome: string(r.Outcome), Error: r.Error, WantAttrib: c.Attribution[r.Guideline]}
		if r.Attribution != nil {
			j.Attribution = r.Attribution.Value
		}
		if want, ok := c.Evidence[r.Guideline]; ok && r.Outcome == report.Fail {
			hit := false
			for _, ev := range r.Evidence {
				if ev.StartLine <= want[1] && ev.EndLine >= want[0] {
					hit = true
				}
			}
			j.EvidenceHit = &hit
		}
		js = append(js, j)
	}
	if result.Error != "" {
		for i := range js {
			if js[i].Error == "" {
				js[i].Error = result.Error
			}
		}
	}
	return js
}

// keepOnlyLabeled restricts a plan to the labeled guidelines so that every
// request measures something.
func keepOnlyLabeled(p *evaluate.Plan, expect map[string]string) {
	res := p.Resolution
	var applicable []*guideline.Resolved
	var questions []decision.Question
	for i, g := range res.Applicable {
		if _, ok := expect[g.ID]; ok {
			applicable = append(applicable, g)
			if i < len(p.Questions) {
				questions = append(questions, p.Questions[i])
			}
		}
	}
	var tensions []guideline.Tension
	for _, t := range res.Tensions {
		_, a := expect[t.Dominant]
		_, b := expect[t.Yielding]
		if a && b {
			tensions = append(tensions, t)
		}
	}
	res.Applicable, res.Tensions, p.Questions = applicable, tensions, questions
}

// cache stores provider responses on disk, keyed by model and request.
type cache struct {
	decision.Provider
	dir         string
	skip        bool
	mu          sync.Mutex
	hits, calls int
	tokens      int
}

func (c *cache) Decide(ctx context.Context, req decision.Request) (*decision.Response, error) {
	body, _ := json.Marshal(struct {
		Info decision.Info
		Req  decision.Request
	}{c.Info(), req})
	sum := sha256.Sum256(body)
	path := filepath.Join(c.dir, hex.EncodeToString(sum[:16])+".json")
	if !c.skip {
		if data, err := os.ReadFile(path); err == nil {
			var resp decision.Response
			if json.Unmarshal(data, &resp) == nil {
				c.mu.Lock()
				c.hits++
				c.mu.Unlock()
				return &resp, nil
			}
		}
	}
	resp, err := c.Provider.Decide(ctx, req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.calls++
	c.tokens += resp.Usage.InputTokens
	c.mu.Unlock()
	if data, err := json.Marshal(resp); err == nil {
		os.MkdirAll(c.dir, 0o755)
		os.WriteFile(path, data, 0o644)
	}
	return resp, nil
}
