package guideline

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Library holds every guideline set that can be named in extends: the
// preset catalog plus user-defined sets.
type Library struct {
	sets  map[string]*Set
	order []string
}

// NewLibrary indexes sets by identifier. Identifiers must be unique.
func NewLibrary(sets ...*Set) (*Library, error) {
	l := &Library{sets: map[string]*Set{}}
	for _, s := range sets {
		if err := l.Add(s); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// Add registers a set. A set may not reuse an existing identifier, so
// preset identities cannot be shadowed.
func (l *Library) Add(s *Set) error {
	if s.ID == "" || strings.ContainsAny(s.ID, "/ ,") || s.ID == ConfigSet {
		return fmt.Errorf("invalid guideline set identifier %q", s.ID)
	}
	if prev, ok := l.sets[s.ID]; ok {
		if prev.Builtin {
			return fmt.Errorf("guideline set %q conflicts with the predefined preset of the same name", s.ID)
		}
		return fmt.Errorf("guideline set %q is defined twice", s.ID)
	}
	l.sets[s.ID] = s
	l.order = append(l.order, s.ID)
	return nil
}

// Get returns the set with the given identifier.
func (l *Library) Get(id string) (*Set, bool) {
	s, ok := l.sets[id]
	return s, ok
}

// Sets returns every set in registration order.
func (l *Library) Sets() []*Set {
	out := make([]*Set, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.sets[id])
	}
	return out
}

// Expand returns the guidelines of a set, including inherited ones.
// Inherited guidelines come first so that a set's own guidelines can
// redefine them by identifier.
func (l *Library) Expand(id string) ([]Guideline, error) {
	return l.expand(id, nil)
}

func (l *Library) expand(id string, stack []string) ([]Guideline, error) {
	if slices.Contains(stack, id) {
		return nil, fmt.Errorf("guideline set inheritance cycle: %s", strings.Join(append(stack, id), " -> "))
	}
	s, ok := l.sets[id]
	if !ok {
		return nil, fmt.Errorf("unknown guideline set %q (run `aesthetics rules --catalog` to list presets)", id)
	}
	stack = append(stack, id)
	var out []Guideline
	index := map[string]int{}
	add := func(g Guideline) {
		if i, ok := index[g.ID]; ok {
			out[i] = g
			return
		}
		index[g.ID] = len(out)
		out = append(out, g)
	}
	for _, parent := range s.Extends {
		inherited, err := l.expand(parent, stack)
		if err != nil {
			return nil, err
		}
		for _, g := range inherited {
			g.Chain = append([]string{id}, g.Chain...)
			add(g)
		}
	}
	for _, spec := range s.Guidelines {
		g, err := spec.Build(id)
		if err != nil {
			return nil, err
		}
		g.Chain = []string{id}
		add(g)
	}
	return out, nil
}

// Override specializes an active guideline, or every guideline of a set,
// without redefining it.
type Override struct {
	ID             string    `yaml:"id"`
	Text           string    `yaml:"text,omitempty"`
	Severity       *Severity `yaml:"severity,omitempty"`
	Priority       *int      `yaml:"priority,omitempty"`
	Instructions   string    `yaml:"instructions,omitempty"`
	Recommendation string    `yaml:"recommendation,omitempty"`
	Remedies       []string  `yaml:"remedies,omitempty"`
}

// Layer is one step of guideline composition: the configuration's top
// level, or one rule. Layers apply in order; later layers take precedence.
type Layer struct {
	Name         string
	Scope        *Scope // nil applies everywhere
	Extends      []string
	Guidelines   []Spec
	Overrides    []Override
	Disable      []string
	Severity     *Severity
	Priority     *int
	Instructions string
}

// Resolved is a guideline selected for an artifact, with its provenance.
type Resolved struct {
	Guideline
	Rank       int      `json:"rank"`
	Layer      string   `json:"layer"`
	Via        []string `json:"introduced_by"`
	Overridden []string `json:"overridden_by,omitempty"`

	layer int
	order int
}

// Origin names where the guideline came from, such as "unix" or
// "team > kiss".
func (r *Resolved) Origin() string {
	if len(r.Chain) == 0 {
		return ConfigSet
	}
	return strings.Join(r.Chain, " > ")
}

// Disabled records a guideline removed by a layer.
type Disabled struct {
	ID string `json:"id"`
	By string `json:"by"`
}

// Tension records two applicable guidelines declared to conflict. When
// meeting the yielding guideline would compromise the dominant one, the
// dominant guideline governs.
type Tension struct {
	Dominant string `json:"dominant"`
	Yielding string `json:"yielding"`
}

// Resolution is the outcome of guideline selection for one artifact.
type Resolution struct {
	Layers     []string    `json:"layers"`
	Applicable []*Resolved `json:"applicable"`
	OutOfScope []*Resolved `json:"out_of_scope,omitempty"`
	Disabled   []Disabled  `json:"disabled,omitempty"`
	Tensions   []Tension   `json:"tensions,omitempty"`
	Context    []string    `json:"context,omitempty"`
	Warnings   []string    `json:"warnings,omitempty"`
}

// Find returns the applicable guideline with the given identifier.
func (r *Resolution) Find(id string) *Resolved {
	for _, g := range r.Applicable {
		if g.ID == id {
			return g
		}
	}
	return nil
}

// Resolve selects the guidelines applicable to target.
//
// Algorithm:
//  1. Layers whose scope covers the target apply in declaration order.
//  2. Each layer adds the guidelines of the sets it extends and its inline
//     guidelines, then removes what it disables, then applies overrides.
//     Inline guidelines with an existing identifier replace it; re-extending
//     an active guideline only moves it to the later layer.
//  3. Each remaining guideline is kept only if its own scope covers the
//     target, so activating a preset never evaluates unrelated artifacts.
//  4. Applicable guidelines are ranked by priority, then severity, then
//     layer, then declaration order; on ties, what is declared later wins,
//     as later layers are more specific.
//  5. Declared conflicts between applicable guidelines become tensions in
//     which the higher-ranked guideline dominates.
func Resolve(lib *Library, layers []Layer, t Target) (*Resolution, error) {
	res := &Resolution{}
	var active []*Resolved
	find := func(id string) int {
		return slices.IndexFunc(active, func(r *Resolved) bool { return r.ID == id })
	}
	order := 0
	for li, layer := range layers {
		if layer.Scope != nil && !layer.Scope.Matches(t) {
			continue
		}
		res.Layers = append(res.Layers, layer.Name)
		if s := strings.TrimSpace(layer.Instructions); s != "" {
			res.Context = append(res.Context, s)
		}
		applyDefaults := func(g *Guideline) {
			if layer.Severity != nil {
				g.Severity = *layer.Severity
			}
			if layer.Priority != nil {
				g.Priority = *layer.Priority
			}
		}
		for _, ext := range layer.Extends {
			gs, err := lib.Expand(ext)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", layer.Name, err)
			}
			for _, g := range gs {
				if i := find(g.ID); i >= 0 {
					active[i].layer, active[i].Layer = li, layer.Name
					active[i].Via = append(active[i].Via, layer.Name)
					applyDefaults(&active[i].Guideline)
					continue
				}
				applyDefaults(&g)
				order++
				active = append(active, &Resolved{Guideline: g, Layer: layer.Name, Via: []string{layer.Name}, layer: li, order: order})
			}
		}
		for _, spec := range layer.Guidelines {
			g, err := spec.Build(ConfigSet)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", layer.Name, err)
			}
			if spec.Severity == nil && layer.Severity != nil {
				g.Severity = *layer.Severity
			}
			if spec.Priority == nil && layer.Priority != nil {
				g.Priority = *layer.Priority
			}
			order++
			r := &Resolved{Guideline: g, Layer: layer.Name, Via: []string{layer.Name}, layer: li, order: order}
			if i := find(g.ID); i >= 0 {
				r.Via = append(active[i].Via, layer.Name)
				r.order = active[i].order
				active[i] = r
				continue
			}
			active = append(active, r)
		}
		for _, ref := range layer.Disable {
			n := len(active)
			active = slices.DeleteFunc(active, func(r *Resolved) bool {
				if r.Refers(ref) {
					res.Disabled = append(res.Disabled, Disabled{ID: r.ID, By: layer.Name})
					return true
				}
				return false
			})
			if len(active) == n {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: disable %q matched no active guideline", layer.Name, ref))
			}
		}
		for _, ov := range layer.Overrides {
			matched := false
			for _, r := range active {
				if !r.Refers(ov.ID) {
					continue
				}
				matched = true
				applyOverride(&r.Guideline, ov)
				r.Overridden = append(r.Overridden, layer.Name)
			}
			if !matched {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: override %q matched no active guideline", layer.Name, ov.ID))
			}
		}
	}
	// A guideline disabled by one layer and re-introduced by a later one is active.
	res.Disabled = slices.DeleteFunc(res.Disabled, func(d Disabled) bool { return find(d.ID) >= 0 })

	for _, r := range active {
		if r.Scope.Matches(t) {
			res.Applicable = append(res.Applicable, r)
		} else {
			res.OutOfScope = append(res.OutOfScope, r)
		}
	}
	slices.SortStableFunc(res.Applicable, compareRank)
	for i, r := range res.Applicable {
		r.Rank = i + 1
	}
	for i, a := range res.Applicable {
		for _, b := range res.Applicable[i+1:] {
			if refersAny(a.ConflictsWith, b) || refersAny(b.ConflictsWith, a) {
				res.Tensions = append(res.Tensions, Tension{Dominant: a.ID, Yielding: b.ID})
			}
		}
	}
	return res, nil
}

func compareRank(a, b *Resolved) int {
	return cmp.Or(
		cmp.Compare(b.Priority, a.Priority),
		cmp.Compare(b.Severity, a.Severity),
		cmp.Compare(b.layer, a.layer),
		cmp.Compare(b.order, a.order),
	)
}

func refersAny(refs []string, r *Resolved) bool {
	return slices.ContainsFunc(refs, r.Refers)
}

func applyOverride(g *Guideline, ov Override) {
	if s := strings.TrimSpace(ov.Text); s != "" {
		g.Text = s
	}
	if ov.Severity != nil {
		g.Severity = *ov.Severity
	}
	if ov.Priority != nil {
		g.Priority = *ov.Priority
	}
	if s := strings.TrimSpace(ov.Instructions); s != "" {
		g.Instructions = strings.TrimSpace(g.Instructions + "\n" + s)
	}
	if s := strings.TrimSpace(ov.Recommendation); s != "" {
		g.Recommendation = s
	}
	if len(ov.Remedies) > 0 {
		g.Remedies = ov.Remedies
	}
}
