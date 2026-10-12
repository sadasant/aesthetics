package guideline

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

func testLibrary(t *testing.T, extra ...*Set) *Library {
	t.Helper()
	presets, err := Presets()
	if err != nil {
		t.Fatal(err)
	}
	lib, err := NewLibrary(append(presets, extra...)...)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func ids(rs []*Resolved) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func sev(s Severity) *Severity { return &s }
func intp(i int) *int          { return &i }

func TestPresetsAreComplete(t *testing.T) {
	presets, err := Presets()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 6 {
		t.Fatalf("got %d presets, want 6", len(presets))
	}
	for _, p := range presets {
		if p.Description == "" || len(p.Guidelines) == 0 || len(p.Kinds()) == 0 {
			t.Errorf("preset %s lacks a description, guidelines, or scopes", p.ID)
		}
	}
}

func TestResolveKeepsPresetsToTheirScopes(t *testing.T) {
	lib := testLibrary(t)
	layers := []Layer{{Name: "base", Extends: []string{"atomic", "readme"}}}
	res, err := Resolve(lib, layers, Target{Kind: File, Path: "src/main.go", Classes: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applicable) != 0 {
		t.Errorf("diff-only and README-only guidelines applied to a Go file: %v", ids(res.Applicable))
	}
	res, err = Resolve(lib, layers, Target{Kind: File, Path: "docs/README.md", Classes: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(res.Applicable)
	if !slices.Contains(got, "readme/usage") || slices.Contains(got, "readme/installation") {
		t.Errorf("nested README: got %v, want usage but not root-only installation", got)
	}
}

func TestResolveComposition(t *testing.T) {
	team := &Set{ID: "team", Extends: []string{"kiss"}, Guidelines: []Spec{{ID: "naming", Text: "Names reveal intent.", Scope: Scope{AppliesTo: []Kind{File}}}}}
	lib := testLibrary(t, team)
	utils := Scope{Match: []string{"src/**/utils/**"}}
	legacy := Scope{Match: []string{"src/legacy/**"}}
	layers := []Layer{
		{Name: "base", Extends: []string{"team"}},
		{Name: "utils", Scope: &utils,
			Guidelines:   []Spec{{ID: "kiss/direct", Text: "Utilities are boringly direct."}}, // redefinition
			Overrides:    []Override{{ID: "team/naming", Severity: sev(Error), Priority: intp(5)}},
			Instructions: "Shared by every service."},
		{Name: "legacy", Scope: &legacy, Disable: []string{"kiss"}},
	}
	target := func(p string) Target { return Target{Kind: File, Path: p, Classes: []string{"code"}} }

	res, err := Resolve(lib, layers, target("src/utils/str.go"))
	if err != nil {
		t.Fatal(err)
	}
	naming := res.Find("team/naming")
	if naming == nil || naming.Severity != Error || naming.Rank != 1 {
		t.Fatalf("override not applied or not ranked first: %+v", naming)
	}
	direct := res.Find("kiss/direct")
	if direct == nil || direct.Text != "Utilities are boringly direct." || direct.Set != ConfigSet {
		t.Errorf("inline guideline did not redefine kiss/direct: %+v", direct)
	}
	if res.Find("kiss/no-speculative-generality").Origin() != "team > kiss" {
		t.Errorf("inheritance chain not recorded")
	}
	if !slices.Equal(res.Context, []string{"Shared by every service."}) {
		t.Errorf("context = %v", res.Context)
	}

	res, err = Resolve(lib, layers, target("src/legacy/old.go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Applicable); !slices.Equal(got, []string{"team/naming"}) {
		t.Errorf("disabling a set inherited through another: got %v", got)
	}
	if len(res.Disabled) == 0 || res.Disabled[0].By != "legacy" {
		t.Errorf("disabled guidelines not recorded: %v", res.Disabled)
	}
}

func TestResolveTensionFollowsRank(t *testing.T) {
	lib := testLibrary(t)
	target := Target{Kind: File, Path: "a.go", Classes: []string{"code"}}
	res, err := Resolve(lib, []Layer{{Name: "base", Extends: []string{"kiss", "solid"}}}, target)
	if err != nil {
		t.Fatal(err)
	}
	// Equal priority and severity: the later-declared set (solid) dominates.
	want := Tension{Dominant: "solid/open-closed", Yielding: "kiss/no-speculative-generality"}
	if !slices.Contains(res.Tensions, want) {
		t.Errorf("tensions = %v, want %v", res.Tensions, want)
	}
	// Raising kiss's priority reverses who governs.
	layers := []Layer{{Name: "base", Extends: []string{"kiss", "solid"}, Overrides: []Override{{ID: "kiss", Priority: intp(1)}}}}
	res, err = Resolve(lib, layers, target)
	if err != nil {
		t.Fatal(err)
	}
	want = Tension{Dominant: "kiss/no-speculative-generality", Yielding: "solid/open-closed"}
	if !slices.Contains(res.Tensions, want) {
		t.Errorf("after priority override, tensions = %v, want %v", res.Tensions, want)
	}
}

func TestDisabledThenReintroduced(t *testing.T) {
	lib := testLibrary(t)
	docs := Scope{Match: []string{"docs/**"}}
	guide := Scope{Match: []string{"docs/guide/**"}}
	layers := []Layer{
		{Name: "base", Extends: []string{"kiss"}},
		{Name: "docs", Scope: &docs, Disable: []string{"kiss/direct"}},
		{Name: "guide", Scope: &guide, Extends: []string{"kiss"}},
	}
	res, err := Resolve(lib, layers, Target{Kind: File, Path: "docs/guide/x.go", Classes: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Find("kiss/direct") == nil || len(res.Disabled) != 0 {
		t.Errorf("guideline re-extended by a later rule should be active: %v, disabled %v", ids(res.Applicable), res.Disabled)
	}
}

func TestInheritanceCycle(t *testing.T) {
	lib := testLibrary(t, &Set{ID: "a", Extends: []string{"b"}}, &Set{ID: "b", Extends: []string{"a"}})
	if _, err := lib.Expand("a"); err == nil {
		t.Fatal("expected a cycle error")
	}
}

func TestPresetsCannotBeShadowed(t *testing.T) {
	presets, _ := Presets()
	if _, err := NewLibrary(append(presets, &Set{ID: "unix"})...); err == nil {
		t.Fatal("expected an error for a user set named like a preset")
	}
}

func TestSpecShorthandAndSeverity(t *testing.T) {
	var specs []Spec
	src := "- Keep it small.\n- text: Be clear.\n  severity: error\n  applies_to: [file, diff]\n"
	if err := yaml.Unmarshal([]byte(src), &specs); err != nil {
		t.Fatal(err)
	}
	g, err := specs[0].Build(ConfigSet)
	if err != nil || g.ID != "config/keep-it-small" || g.Severity != Warning || !slices.Equal(g.Scope.AppliesTo, []Kind{File}) {
		t.Errorf("shorthand: %+v %v", g, err)
	}
	g, err = specs[1].Build(ConfigSet)
	if err != nil || g.Severity != Error || len(g.Scope.AppliesTo) != 2 || g.Recommendation == "" {
		t.Errorf("full spec: %+v %v", g, err)
	}
}
