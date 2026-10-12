package config

import (
	"strings"
	"testing"

	"github.com/sadasant/aesthetics/internal/guideline"
)

// The configuration used to illustrate the specification must work as written.
const specExample = `
extends:
  - unix
  - kiss
  - atomic
rules:
  - match: "**/README.md"
    extends: [readme, diataxis]
  - match: "**/*.ts"
    guidelines:
      - Keep public interfaces deliberate and minimal.
  - match: "src/**/utils/**"
    guidelines:
      - Avoid unnecessary abstraction.
`

func TestSpecExample(t *testing.T) {
	c, err := Parse([]byte(specExample))
	if err != nil {
		t.Fatal(err)
	}
	lib, err := c.Library()
	if err != nil {
		t.Fatal(err)
	}
	res, err := guideline.Resolve(lib, c.Layers(nil), guideline.Target{Kind: guideline.File, Path: "src/api/utils/index.ts", Classes: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"config/keep-public-interfaces-deliberate-and-minimal", "config/avoid-unnecessary-abstraction", "unix/deliberate-public-surface", "kiss/direct"} {
		if res.Find(id) == nil {
			t.Errorf("%s not applicable to src/api/utils/index.ts", id)
		}
	}
	if res.Find("readme/usage") != nil || res.Find("atomic/single-purpose") != nil {
		t.Error("README and diff guidelines leaked onto a TypeScript file")
	}
	if got := c.Presets(nil); strings.Join(got, ",") != "unix,kiss,atomic,readme,diataxis" {
		t.Errorf("presets = %v", got)
	}
}

func TestParseRejectsMistakes(t *testing.T) {
	for name, src := range map[string]string{
		"unknown field":   "extend: [unix]\n",
		"unknown preset":  "extends: [unixx]\n",
		"bad severity":    "guidelines:\n  - text: x\n    severity: fatal\n",
		"bad thresholds":  "evaluation:\n  pass_threshold: 0.3\n  fail_threshold: 0.4\n",
		"bad fail_on":     "ci:\n  fail_on: sometimes\n",
		"bad kind":        "rules:\n  - applies_to: [module]\n",
		"empty guideline": "guidelines:\n  - id: x\n",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
