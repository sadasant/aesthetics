// Package config loads the Aesthetics configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"

	"go.yaml.in/yaml/v3"
)

// FileNames are searched, in order, from the working directory upward.
var FileNames = []string{".aesthetics.yaml", ".aesthetics.yml", "aesthetics.yaml", "aesthetics.yml"}

// Config is the configuration file schema.
type Config struct {
	Version  int             `yaml:"version,omitempty"`
	Provider decision.Config `yaml:"provider,omitempty"`
	// Extends activates guideline sets (presets or user sets) everywhere.
	Extends guideline.StringList `yaml:"extends,omitempty"`
	// Guidelines are natural-language guidelines that apply everywhere
	// their own scope allows.
	Guidelines []guideline.Spec `yaml:"guidelines,omitempty"`
	// Sets defines reusable, named guideline sets that can extend others.
	Sets map[string]SetSpec `yaml:"sets,omitempty"`
	// Rules specialize guidelines for paths, file types, and depths. They
	// apply in order; later rules take precedence over earlier ones.
	Rules      []Rule     `yaml:"rules,omitempty"`
	Files      Files      `yaml:"files,omitempty"`
	Evaluation Evaluation `yaml:"evaluation,omitempty"`
	CI         CI         `yaml:"ci,omitempty"`

	Path string `yaml:"-"` // the file this configuration came from
	Root string `yaml:"-"` // the directory paths are relative to
}

// SetSpec defines a user guideline set.
type SetSpec struct {
	Name        string               `yaml:"name,omitempty"`
	Description string               `yaml:"description,omitempty"`
	Extends     guideline.StringList `yaml:"extends,omitempty"`
	Guidelines  []guideline.Spec     `yaml:"guidelines,omitempty"`
}

// Rule is a scoped composition step.
type Rule struct {
	Name            string `yaml:"name,omitempty"`
	guideline.Scope `yaml:",inline"`
	Extends         guideline.StringList `yaml:"extends,omitempty"`
	Guidelines      []guideline.Spec     `yaml:"guidelines,omitempty"`
	Override        []guideline.Override `yaml:"override,omitempty"`
	Disable         guideline.StringList `yaml:"disable,omitempty"`
	Severity        *guideline.Severity  `yaml:"severity,omitempty"`
	Priority        *int                 `yaml:"priority,omitempty"`
	Instructions    string               `yaml:"instructions,omitempty"`
}

// Files selects which files belong to the evaluation.
type Files struct {
	Include         guideline.StringList `yaml:"include,omitempty"`
	Exclude         guideline.StringList `yaml:"exclude,omitempty"`
	DefaultExcludes *bool                `yaml:"default_excludes,omitempty"`
	MaxFileBytes    int64                `yaml:"max_file_bytes,omitempty"`
}

// Evaluation tunes how answers become outcomes.
type Evaluation struct {
	PassThreshold *float64 `yaml:"pass_threshold,omitempty"`
	FailThreshold *float64 `yaml:"fail_threshold,omitempty"`
	Concurrency   int      `yaml:"concurrency,omitempty"`
	Evidence      *bool    `yaml:"evidence,omitempty"`
}

// CI holds failure thresholds for automated execution.
type CI struct {
	FailOn              string `yaml:"fail_on,omitempty"` // error, warning, info, or never
	FailOnIndeterminate *bool  `yaml:"fail_on_indeterminate,omitempty"`
	FailOnIncomplete    *bool  `yaml:"fail_on_incomplete,omitempty"`
	FailOnPreexisting   *bool  `yaml:"fail_on_preexisting,omitempty"`
}

// Find searches dir and its parents for a configuration file, stopping at
// the repository root (a directory containing .git).
func Find(dir string) string {
	for {
		for _, n := range FileNames {
			p := filepath.Join(dir, n)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Load reads and validates a configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.Path, c.Root = abs, filepath.Dir(abs)
	return c, nil
}

// Parse decodes and validates configuration YAML. Unknown fields are errors.
func Parse(data []byte) (*Config, error) {
	c := &Config{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return c, c.Validate()
}

// Validate checks values that the YAML schema cannot.
func (c *Config) Validate() error {
	if c.Version != 0 && c.Version != 1 {
		return fmt.Errorf("unsupported configuration version %d (want 1)", c.Version)
	}
	if _, err := c.Policy(); err != nil {
		return err
	}
	t := c.Thresholds()
	if t.Fail < 0 || t.Pass > 1 || t.Fail >= t.Pass {
		return fmt.Errorf("evaluation thresholds must satisfy 0 <= fail_threshold < pass_threshold <= 1 (got %.2f and %.2f)", t.Fail, t.Pass)
	}
	for i, r := range c.Rules {
		if err := r.Scope.Validate(); err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
		for _, o := range r.Override {
			if o.ID == "" {
				return fmt.Errorf("rules[%d]: override without id", i)
			}
		}
	}
	lib, err := c.Library()
	if err != nil {
		return err
	}
	for _, l := range c.Layers(nil) {
		for _, ext := range l.Extends {
			if _, err := lib.Expand(ext); err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
		}
		for _, s := range l.Guidelines {
			if _, err := s.Build(guideline.ConfigSet); err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
		}
	}
	return nil
}

// Library returns the presets plus the user-defined sets.
func (c *Config) Library() (*guideline.Library, error) {
	presets, err := guideline.Presets()
	if err != nil {
		return nil, err
	}
	lib, err := guideline.NewLibrary(presets...)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(c.Sets))
	for id := range c.Sets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := c.Sets[id]
		if err := lib.Add(&guideline.Set{ID: id, Name: s.Name, Description: s.Description, Extends: s.Extends, Guidelines: s.Guidelines}); err != nil {
			return nil, err
		}
	}
	return lib, nil
}

// Layers returns the composition layers: the top level (plus presets
// activated on the command line) followed by each rule.
func (c *Config) Layers(use []string) []guideline.Layer {
	base := guideline.Layer{Name: "base", Extends: append(append([]string{}, c.Extends...), use...), Guidelines: c.Guidelines}
	layers := []guideline.Layer{base}
	for i, r := range c.Rules {
		scope := r.Scope
		name := fmt.Sprintf("rules[%d]", i)
		if r.Name != "" {
			name += " " + r.Name
		} else if len(r.Match) > 0 {
			name += " (" + strings.Join(r.Match, ", ") + ")"
		}
		layers = append(layers, guideline.Layer{
			Name: name, Scope: &scope, Extends: r.Extends, Guidelines: r.Guidelines,
			Overrides: r.Override, Disable: r.Disable, Severity: r.Severity, Priority: r.Priority,
			Instructions: r.Instructions,
		})
	}
	return layers
}

// Thresholds returns the outcome thresholds.
func (c *Config) Thresholds() report.Thresholds {
	t := report.Thresholds{Pass: 0.65, Fail: 0.35}
	if c.Evaluation.PassThreshold != nil {
		t.Pass = *c.Evaluation.PassThreshold
	}
	if c.Evaluation.FailThreshold != nil {
		t.Fail = *c.Evaluation.FailThreshold
	}
	return t
}

// ParseFailOn converts a fail_on value into a severity; "never" is zero.
func ParseFailOn(s string) (guideline.Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "warning", "warn":
		return guideline.Warning, nil
	case "never", "none":
		return 0, nil
	}
	sev, err := guideline.ParseSeverity(s)
	if err != nil {
		return 0, fmt.Errorf("invalid fail_on %q (want error, warning, info or never)", s)
	}
	return sev, nil
}

// Policy returns the CI failure policy. By default failures of severity
// warning or above fail the run, incomplete evaluations fail the run, and
// indeterminate results and preexisting problems in diffs do not.
func (c *Config) Policy() (report.Policy, error) {
	sev, err := ParseFailOn(c.CI.FailOn)
	if err != nil {
		return report.Policy{}, err
	}
	p := report.Policy{FailOn: sev, FailOnName: failOnName(sev), FailOnIncomplete: true}
	if c.CI.FailOnIndeterminate != nil {
		p.FailOnIndeterminate = *c.CI.FailOnIndeterminate
	}
	if c.CI.FailOnIncomplete != nil {
		p.FailOnIncomplete = *c.CI.FailOnIncomplete
	}
	if c.CI.FailOnPreexisting != nil {
		p.FailOnPreexisting = *c.CI.FailOnPreexisting
	}
	return p, nil
}

func failOnName(s guideline.Severity) string {
	if s == 0 {
		return "never"
	}
	return s.String()
}

// Excludes returns the effective exclude patterns.
func (c *Config) Excludes() []string {
	var out []string
	if c.Files.DefaultExcludes == nil || *c.Files.DefaultExcludes {
		out = append(out, artifact.DefaultExcludes...)
	}
	return append(out, c.Files.Exclude...)
}

// Evidence reports whether evidence questions are enabled (default true).
func (c *Config) Evidence() bool {
	return c.Evaluation.Evidence == nil || *c.Evaluation.Evidence
}

// Presets lists the predefined presets activated anywhere in the
// configuration or on the command line, in catalog order.
func (c *Config) Presets(use []string) []string {
	lib, err := c.Library()
	if err != nil {
		return nil
	}
	active := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		s, ok := lib.Get(id)
		if !ok || active[id] {
			return
		}
		active[id] = true
		for _, p := range s.Extends {
			visit(p)
		}
	}
	for _, l := range c.Layers(use) {
		for _, e := range l.Extends {
			visit(e)
		}
	}
	var out []string
	for _, id := range guideline.PresetIDs {
		if active[id] {
			out = append(out, id)
		}
	}
	return out
}
