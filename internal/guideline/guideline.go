// Package guideline defines qualitative guidelines, the preset catalog, and
// the algorithm that selects the guidelines applicable to an artifact.
package guideline

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/sadasant/aesthetics/internal/glob"

	"go.yaml.in/yaml/v3"
)

// Kind is the kind of artifact a guideline can evaluate.
type Kind string

const (
	File       Kind = "file"
	Directory  Kind = "directory"
	Repository Kind = "repository"
	Diff       Kind = "diff"
)

// Kinds lists every artifact kind in display order.
var Kinds = []Kind{File, Directory, Repository, Diff}

// Classes lists the content classes an artifact can belong to.
var Classes = []string{"code", "test", "docs", "config", "data", "other"}

// Severity expresses how serious a failed guideline is.
type Severity int

const (
	Info Severity = iota + 1
	Warning
	Error
)

// ParseSeverity converts "info", "warning" or "error" into a Severity.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return Info, nil
	case "warning", "warn":
		return Warning, nil
	case "error":
		return Error, nil
	}
	return 0, fmt.Errorf("unknown severity %q (want info, warning or error)", s)
}

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Error:
		return "error"
	}
	return "unknown"
}

func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

func (s *Severity) UnmarshalText(b []byte) error {
	v, err := ParseSeverity(string(b))
	*s = v
	return err
}

// StringList accepts either a single YAML string or a list of strings.
type StringList []string

func (l *StringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*l = splitList(s)
		return nil
	}
	var items []string
	if err := n.Decode(&items); err != nil {
		return err
	}
	*l = items
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Scope restricts where a guideline or rule applies.
type Scope struct {
	// AppliesTo lists artifact kinds. Empty means every kind for rules and
	// "file" for guidelines.
	AppliesTo []Kind `yaml:"applies_to,omitempty" json:"applies_to,omitempty"`
	// Content lists content classes (code, test, docs, config, data, other).
	Content StringList `yaml:"content,omitempty" json:"content,omitempty"`
	// Match lists glob patterns relative to the repository root.
	Match StringList `yaml:"match,omitempty" json:"match,omitempty"`
	// Exclude lists glob patterns that remove paths matched otherwise.
	Exclude StringList `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	// Extensions lists filename extensions such as ".ts".
	Extensions StringList `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	// MinDepth and MaxDepth bound the number of directories above a path:
	// "README.md" and "src" have depth 0, "src/main.go" has depth 1.
	MinDepth *int `yaml:"min_depth,omitempty" json:"min_depth,omitempty"`
	MaxDepth *int `yaml:"max_depth,omitempty" json:"max_depth,omitempty"`
}

// Target describes an artifact for scope matching.
type Target struct {
	Kind    Kind
	Path    string   // relative path for files and directories
	Paths   []string // changed paths for diffs
	Classes []string // content classes of the artifact or its constituents
}

// Depth returns the scope depth of a relative path.
func Depth(p string) int { return strings.Count(p, "/") }

func (s Scope) hasPathFilters() bool {
	return len(s.Match) > 0 || len(s.Extensions) > 0 || s.MinDepth != nil || s.MaxDepth != nil
}

func (s Scope) pathMatches(p string) bool {
	if len(s.Match) > 0 && !glob.Any(s.Match, p) {
		return false
	}
	if glob.Any(s.Exclude, p) {
		return false
	}
	if len(s.Extensions) > 0 {
		ext := strings.ToLower(path.Ext(p))
		if !slices.ContainsFunc(s.Extensions, func(e string) bool { return normalizeExt(e) == ext }) {
			return false
		}
	}
	d := Depth(p)
	if s.MinDepth != nil && d < *s.MinDepth {
		return false
	}
	if s.MaxDepth != nil && d > *s.MaxDepth {
		return false
	}
	return true
}

func normalizeExt(e string) string {
	e = strings.ToLower(strings.TrimSpace(e))
	if !strings.HasPrefix(e, ".") {
		e = "." + e
	}
	return e
}

// Matches reports whether the scope covers the target.
//
// Path filters (match, extensions, depth) never cover the repository as a
// whole. A diff is covered when at least one changed path is covered.
func (s Scope) Matches(t Target) bool {
	if len(s.AppliesTo) > 0 && !slices.Contains(s.AppliesTo, t.Kind) {
		return false
	}
	if len(s.Content) > 0 && !slices.ContainsFunc(s.Content, func(c string) bool { return slices.Contains(t.Classes, c) }) {
		return false
	}
	switch t.Kind {
	case Repository:
		return !s.hasPathFilters()
	case Diff:
		if !s.hasPathFilters() && len(s.Exclude) == 0 {
			return true
		}
		return slices.ContainsFunc(t.Paths, s.pathMatches)
	default:
		return s.pathMatches(t.Path)
	}
}

// Validate checks the scope's values.
func (s Scope) Validate() error {
	for _, k := range s.AppliesTo {
		if !slices.Contains(Kinds, k) {
			return fmt.Errorf("unknown artifact kind %q (want file, directory, repository or diff)", k)
		}
	}
	for _, c := range s.Content {
		if !slices.Contains(Classes, c) {
			return fmt.Errorf("unknown content class %q (want one of %s)", c, strings.Join(Classes, ", "))
		}
	}
	for _, p := range append(slices.Clone(s.Match), s.Exclude...) {
		if err := glob.Validate(p); err != nil {
			return err
		}
	}
	if s.MinDepth != nil && s.MaxDepth != nil && *s.MinDepth > *s.MaxDepth {
		return fmt.Errorf("min_depth %d exceeds max_depth %d", *s.MinDepth, *s.MaxDepth)
	}
	return nil
}

// Describe renders the scope for humans.
func (s Scope) Describe() string {
	var parts []string
	if len(s.AppliesTo) > 0 {
		var ks []string
		for _, k := range s.AppliesTo {
			ks = append(ks, string(k))
		}
		parts = append(parts, strings.Join(ks, ", "))
	}
	if len(s.Content) > 0 {
		parts = append(parts, "content "+strings.Join(s.Content, ", "))
	}
	if len(s.Match) > 0 {
		parts = append(parts, "match "+strings.Join(s.Match, ", "))
	}
	if len(s.Exclude) > 0 {
		parts = append(parts, "exclude "+strings.Join(s.Exclude, ", "))
	}
	if len(s.Extensions) > 0 {
		parts = append(parts, "extensions "+strings.Join(s.Extensions, ", "))
	}
	if s.MinDepth != nil {
		parts = append(parts, fmt.Sprintf("depth >= %d", *s.MinDepth))
	}
	if s.MaxDepth != nil {
		parts = append(parts, fmt.Sprintf("depth <= %d", *s.MaxDepth))
	}
	if len(parts) == 0 {
		return "everywhere"
	}
	return strings.Join(parts, " · ")
}

// Spec is a guideline as written in a preset or configuration file. A plain
// YAML string is shorthand for a spec with only Text.
type Spec struct {
	ID             string `yaml:"id,omitempty"`
	Text           string `yaml:"text"`
	Scope          `yaml:",inline"`
	Severity       *Severity  `yaml:"severity,omitempty"`
	Priority       *int       `yaml:"priority,omitempty"`
	Instructions   string     `yaml:"instructions,omitempty"`
	Recommendation string     `yaml:"recommendation,omitempty"`
	Remedies       []string   `yaml:"remedies,omitempty"`
	ConflictsWith  StringList `yaml:"conflicts_with,omitempty"`
}

func (s *Spec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&s.Text)
	}
	type plain Spec
	return n.Decode((*plain)(s))
}

// Set is a named, composable collection of guidelines: a preset from the
// catalog or a user-defined set from the configuration.
type Set struct {
	ID          string     `yaml:"id" json:"id"`
	Name        string     `yaml:"name,omitempty" json:"name,omitempty"`
	Foundation  string     `yaml:"foundation,omitempty" json:"foundation,omitempty"`
	Description string     `yaml:"description,omitempty" json:"description,omitempty"`
	References  []string   `yaml:"references,omitempty" json:"references,omitempty"`
	Extends     StringList `yaml:"extends,omitempty" json:"extends,omitempty"`
	Guidelines  []Spec     `yaml:"guidelines" json:"-"`
	Builtin     bool       `yaml:"-" json:"builtin"`
}

// Guideline is a fully identified guideline with defaults applied.
type Guideline struct {
	ID             string   `json:"id"`
	Set            string   `json:"set"` // owning set, or "config"
	Text           string   `json:"text"`
	Scope          Scope    `json:"scope"`
	Severity       Severity `json:"severity"`
	Priority       int      `json:"priority"`
	Instructions   string   `json:"instructions,omitempty"`
	Recommendation string   `json:"recommendation"`
	Remedies       []string `json:"remedies,omitempty"`
	ConflictsWith  []string `json:"conflicts_with,omitempty"`
	// Chain lists the sets through which the guideline was inherited, from
	// the outermost (the one named in extends) to its owner.
	Chain []string `json:"chain,omitempty"`
}

// ConfigSet is the pseudo-set that owns guidelines written inline in the
// configuration.
const ConfigSet = "config"

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slug derives a stable identifier from guideline text.
func Slug(text string) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if len(s) > 48 {
		cut := s[:48]
		if s[48] != '-' {
			if i := strings.LastIndex(cut, "-"); i > 0 {
				cut = cut[:i]
			}
		}
		s = cut
	}
	if s == "" {
		s = "guideline"
	}
	return s
}

// Build converts a spec into a guideline owned by set.
func (s Spec) Build(set string) (Guideline, error) {
	text := strings.TrimSpace(s.Text)
	if text == "" {
		return Guideline{}, fmt.Errorf("guideline %q in %s has no text", s.ID, set)
	}
	if err := s.Scope.Validate(); err != nil {
		return Guideline{}, fmt.Errorf("guideline %q in %s: %w", s.ID, set, err)
	}
	id := s.ID
	if id == "" {
		id = Slug(text)
	}
	if !strings.Contains(id, "/") {
		id = set + "/" + id
	}
	g := Guideline{
		ID:             id,
		Set:            set,
		Text:           text,
		Scope:          s.Scope,
		Severity:       Warning,
		Instructions:   strings.TrimSpace(s.Instructions),
		Recommendation: strings.TrimSpace(s.Recommendation),
		Remedies:       s.Remedies,
		ConflictsWith:  s.ConflictsWith,
	}
	if len(g.Scope.AppliesTo) == 0 {
		g.Scope.AppliesTo = []Kind{File}
	}
	if s.Severity != nil {
		g.Severity = *s.Severity
	}
	if s.Priority != nil {
		g.Priority = *s.Priority
	}
	if g.Recommendation == "" {
		g.Recommendation = "Revise the artifact so that it meets this guideline."
	}
	return g, nil
}

// Refers reports whether ref names this guideline or one of the sets it was
// inherited through.
func (g Guideline) Refers(ref string) bool {
	return ref == g.ID || ref == g.Set || slices.Contains(g.Chain, ref)
}

// Kinds returns the union of artifact kinds covered by the set's guidelines.
func (s *Set) Kinds() []Kind {
	var out []Kind
	for _, k := range Kinds {
		for _, g := range s.Guidelines {
			kinds := g.AppliesTo
			if len(kinds) == 0 {
				kinds = []Kind{File}
			}
			if slices.Contains(kinds, k) {
				out = append(out, k)
				break
			}
		}
	}
	return out
}
