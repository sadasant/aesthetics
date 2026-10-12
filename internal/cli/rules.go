package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
)

func runRules(args []string, stdout, stderr io.Writer) error {
	var o common
	var catalog bool
	fs := newFlagSet("rules", "rules [set...] [options]", stderr)
	o.register(fs)
	fs.BoolVar(&catalog, "catalog", false, "list every available guideline set: the predefined presets and user-defined sets")
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	s, err := o.open()
	if err != nil {
		return err
	}
	w, closeOut, err := o.out(stdout)
	if err != nil {
		return err
	}
	defer closeOut()

	switch {
	case len(ids) > 0:
		var sets []setView
		for _, id := range ids {
			set, ok := s.lib.Get(id)
			if !ok {
				return usageError{fmt.Errorf("unknown guideline set %q (available: %s)", id, strings.Join(setIDs(s.lib), ", "))}
			}
			v, err := viewSet(s.lib, set)
			if err != nil {
				return err
			}
			sets = append(sets, v)
		}
		if o.format == "json" {
			return writeJSON(w, sets)
		}
		for i, v := range sets {
			if i > 0 {
				fmt.Fprintln(w)
			}
			writeSet(w, v)
		}
	case catalog:
		var sets []setView
		for _, set := range s.lib.Sets() {
			v, err := viewSet(s.lib, set)
			if err != nil {
				return err
			}
			sets = append(sets, v)
		}
		if o.format == "json" {
			return writeJSON(w, sets)
		}
		fmt.Fprintln(w, "Available guideline sets (activate with --use or extends):")
		for _, v := range sets {
			kind := "preset"
			if !v.Builtin {
				kind = "user set"
			}
			fmt.Fprintf(w, "\n  %-10s %s (%s) — %s\n", v.ID, v.Name, kind, v.Description)
			fmt.Fprintf(w, "  %-10s scopes: %s · %d guidelines\n", "", joinKinds(v.Scopes), len(v.Guidelines))
		}
		fmt.Fprintln(w, "\nRun `aesthetics rules <set>` for the guidelines of a set.")
	default:
		return writeConfigured(w, o.format, s)
	}
	return nil
}

type setView struct {
	*guideline.Set
	Scopes     []guideline.Kind      `json:"scopes"`
	Guidelines []guideline.Guideline `json:"guidelines"`
}

func viewSet(lib *guideline.Library, s *guideline.Set) (setView, error) {
	gs, err := lib.Expand(s.ID)
	if err != nil {
		return setView{}, err
	}
	v := setView{Set: s, Guidelines: gs}
	for _, k := range guideline.Kinds {
		for _, g := range gs {
			if containsKind(g.Scope.AppliesTo, k) {
				v.Scopes = append(v.Scopes, k)
				break
			}
		}
	}
	return v, nil
}

func containsKind(ks []guideline.Kind, k guideline.Kind) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}

func joinKinds(ks []guideline.Kind) string {
	var s []string
	for _, k := range ks {
		s = append(s, string(k))
	}
	return strings.Join(s, ", ")
}

func writeSet(w io.Writer, v setView) {
	fmt.Fprintf(w, "%s — %s\n", v.ID, v.Name)
	if v.Description != "" {
		fmt.Fprintf(w, "  %s\n", v.Description)
	}
	if v.Foundation != "" {
		fmt.Fprintf(w, "  foundation: %s\n", v.Foundation)
	}
	if len(v.Extends) > 0 {
		fmt.Fprintf(w, "  extends: %s\n", strings.Join(v.Extends, ", "))
	}
	fmt.Fprintf(w, "  scopes: %s\n", joinKinds(v.Scopes))
	for _, ref := range v.References {
		fmt.Fprintf(w, "  reference: %s\n", ref)
	}
	for _, g := range v.Guidelines {
		writeGuideline(w, g)
	}
}

func writeGuideline(w io.Writer, g guideline.Guideline) {
	fmt.Fprintf(w, "\n  %s · %s · priority %d\n", g.ID, g.Severity, g.Priority)
	fmt.Fprintf(w, "    %s\n", g.Text)
	fmt.Fprintf(w, "    scope: %s\n", g.Scope.Describe())
	if g.Instructions != "" {
		fmt.Fprintf(w, "    instructions: %s\n", g.Instructions)
	}
	fmt.Fprintf(w, "    on failure: %s\n", g.Recommendation)
	for _, r := range g.Remedies {
		fmt.Fprintf(w, "      - %s\n", r)
	}
	if len(g.ConflictsWith) > 0 {
		fmt.Fprintf(w, "    in tension with: %s\n", strings.Join(g.ConflictsWith, ", "))
	}
}

// writeConfigured shows the composition layers of the configuration.
func writeConfigured(w io.Writer, format string, s *session) error {
	type layerView struct {
		Name         string                `json:"name"`
		Scope        string                `json:"scope"`
		Extends      []string              `json:"extends,omitempty"`
		Guidelines   []guideline.Guideline `json:"guidelines,omitempty"`
		Overrides    []guideline.Override  `json:"overrides,omitempty"`
		Disable      []string              `json:"disable,omitempty"`
		Severity     string                `json:"severity,omitempty"`
		Priority     *int                  `json:"priority,omitempty"`
		Instructions string                `json:"instructions,omitempty"`
	}
	var layers []layerView
	for _, l := range s.layers {
		v := layerView{Name: l.Name, Scope: "everywhere", Extends: l.Extends, Overrides: l.Overrides, Disable: l.Disable, Priority: l.Priority, Instructions: l.Instructions}
		if l.Scope != nil {
			v.Scope = l.Scope.Describe()
		}
		if l.Severity != nil {
			v.Severity = l.Severity.String()
		}
		for _, spec := range l.Guidelines {
			g, err := spec.Build(guideline.ConfigSet)
			if err != nil {
				return err
			}
			v.Guidelines = append(v.Guidelines, g)
		}
		layers = append(layers, v)
	}
	provider, perr := decision.Describe(s.cfg.Provider)
	if format == "json" {
		out := map[string]any{"config": s.cfg.Path, "presets": s.cfg.Presets(s.use), "layers": layers}
		if perr == nil {
			out["provider"] = provider
		}
		return writeJSON(w, out)
	}
	if s.cfg.Path != "" {
		fmt.Fprintf(w, "Configuration: %s\n", s.cfg.Path)
	} else {
		fmt.Fprintln(w, "Configuration: none found")
	}
	if perr == nil {
		fmt.Fprintf(w, "Provider: %s\n", provider.Label())
	} else {
		fmt.Fprintf(w, "Provider: not configured\n")
	}
	presets := s.cfg.Presets(s.use)
	fmt.Fprintf(w, "Active presets: %s\n", orDash(strings.Join(presets, ", ")))
	empty := true
	for _, l := range layers {
		if len(l.Extends)+len(l.Guidelines)+len(l.Overrides)+len(l.Disable) == 0 && l.Instructions == "" && l.Severity == "" && l.Priority == nil {
			continue
		}
		empty = false
		fmt.Fprintf(w, "\n%s · applies to %s\n", l.Name, l.Scope)
		if len(l.Extends) > 0 {
			fmt.Fprintf(w, "  extends: %s\n", strings.Join(l.Extends, ", "))
		}
		if l.Severity != "" {
			fmt.Fprintf(w, "  severity of guidelines it introduces: %s\n", l.Severity)
		}
		if l.Priority != nil {
			fmt.Fprintf(w, "  priority of guidelines it introduces: %d\n", *l.Priority)
		}
		for _, g := range l.Guidelines {
			fmt.Fprintf(w, "  guideline %s · %s · priority %d · scope: %s\n    %s\n", g.ID, g.Severity, g.Priority, g.Scope.Describe(), g.Text)
		}
		for _, ov := range l.Overrides {
			var parts []string
			if ov.Text != "" {
				parts = append(parts, "text")
			}
			if ov.Severity != nil {
				parts = append(parts, "severity "+ov.Severity.String())
			}
			if ov.Priority != nil {
				parts = append(parts, fmt.Sprintf("priority %d", *ov.Priority))
			}
			if ov.Instructions != "" {
				parts = append(parts, "instructions")
			}
			if ov.Recommendation != "" || len(ov.Remedies) > 0 {
				parts = append(parts, "recommendation")
			}
			fmt.Fprintf(w, "  specializes %s: %s\n", ov.ID, strings.Join(parts, ", "))
		}
		if len(l.Disable) > 0 {
			fmt.Fprintf(w, "  disables: %s\n", strings.Join(l.Disable, ", "))
		}
		if l.Instructions != "" {
			fmt.Fprintf(w, "  context: %s\n", l.Instructions)
		}
	}
	if empty {
		fmt.Fprintln(w, "\nNo guidelines configured. Use --use, `aesthetics init`, or `aesthetics rules --catalog`.")
	} else {
		fmt.Fprintln(w, "\nRun `aesthetics rules <set>` to see a set's guidelines and `aesthetics explain <path>` for what applies to a path.")
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
