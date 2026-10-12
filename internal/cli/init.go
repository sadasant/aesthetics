package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/config"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
)

const initTemplate = `# Aesthetics configuration. See README.md for the full reference.
version: 1

# Decision model used for evaluations. Credentials are read from the
# environment variable named by api_key_env (each type has a default).
provider:
  type: %s
%s
# Predefined guideline sets active everywhere. Available: unix, kiss, atomic,
# readme, diataxis, solid. Nothing is enabled implicitly.
extends: [%s]

# Your own guidelines, in natural language. A string is enough; use a map
# to add an id, severity (info, warning, error), priority, scope, and a
# recommendation.
guidelines:
  - id: intent-revealing-names
    text: Names of files, functions, and types reveal their intent without needing a comment.
    severity: warning
    recommendation: Rename unclear identifiers so they say what they mean.

# Rules specialize guidelines for paths, file types, and depths. They apply
# in order, and later rules take precedence.
rules:
  - match: "**/README.md"
    extends: [readme]
  - match: ["**/*_test.*", "**/*.test.*"]
    guidelines:
      - Each test checks one behavior and its name states that behavior.

# Failure policy for CI.
ci:
  fail_on: warning            # error, warning, info, or never
  fail_on_indeterminate: false
  fail_on_preexisting: false  # with --diff, problems that predate the change do not fail the run
`

func runInit(args []string, stdout, stderr io.Writer) error {
	var provider, model string
	var use listFlag
	var force bool
	fs := newFlagSet("init", "init [options]", stderr)
	fs.StringVar(&provider, "provider", "openai", "model provider type: "+strings.Join(decision.Types, ", "))
	fs.StringVar(&model, "model", "", "model identifier (default: the provider's default)")
	fs.Var(&use, "use", "presets to activate, comma-separated (default: unix,kiss,atomic)")
	fs.BoolVar(&force, "force", false, "overwrite an existing configuration file")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if !slices.Contains(decision.Types, provider) {
		return usageError{fmt.Errorf("unknown provider type %q (want one of %s)", provider, strings.Join(decision.Types, ", "))}
	}
	if len(use) == 0 {
		use = listFlag{"unix", "kiss", "atomic"}
	}
	for _, id := range use {
		if !slices.Contains(guideline.PresetIDs, id) {
			return usageError{fmt.Errorf("unknown preset %q (available: %s)", id, strings.Join(guideline.PresetIDs, ", "))}
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := filepath.Join(artifact.FindRoot(cwd), config.FileNames[0])
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	modelLine := ""
	if model != "" {
		modelLine = "  model: " + model + "\n"
	} else if info, err := decision.Describe(decision.Config{Type: provider}); err == nil && info.Model != "" {
		modelLine = "  model: " + info.Model + "\n"
	}
	content := fmt.Sprintf(initTemplate, provider, modelLine, strings.Join(use, ", "))
	if _, err := config.Parse([]byte(content)); err != nil {
		return fmt.Errorf("internal error: generated configuration is invalid: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote %s\n", path)
	return nil
}
