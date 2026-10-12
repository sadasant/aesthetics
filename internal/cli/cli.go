// Package cli implements the aesthetics command-line interface.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/config"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
	"github.com/sadasant/aesthetics/internal/report"
)

// Version is the program version; release builds may override it with
// -ldflags "-X github.com/sadasant/aesthetics/internal/cli.Version=...".
var Version = "0.1.0"

const usage = `Aesthetics evaluates software artifacts against configurable qualitative guidelines.

Usage:
  aesthetics check [paths...]        Evaluate files, directories, or the whole repository
  aesthetics check --diff [paths...] Evaluate Git changes
  aesthetics explain <path>          Show the guidelines that apply to an artifact and how it is evaluated
  aesthetics rules [set...]          Show configured guidelines, the preset catalog, or a set in detail
  aesthetics init                    Write a starter .aesthetics.yaml
  aesthetics version                 Print the version

Run "aesthetics <command> -h" for the options of a command.

Exit codes: 0 passed, 1 failures at or above the threshold, 2 usage or
configuration error, 3 some evaluations could not be completed.
`

// Run executes the command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return report.ExitUsage
	}
	cmd, rest := args[0], args[1:]
	var err error
	code := report.ExitOK
	switch cmd {
	case "check":
		code, err = runCheck(rest, stdout, stderr)
	case "explain":
		code, err = runExplain(rest, stdout, stderr)
	case "rules":
		err = runRules(rest, stdout, stderr)
	case "init":
		err = runInit(rest, stdout, stderr)
	case "version", "--version":
		fmt.Fprintf(stdout, "aesthetics %s\n", Version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		err = usageError{fmt.Errorf("unknown command %q", cmd)}
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return report.ExitOK
		}
		fmt.Fprintf(stderr, "aesthetics: %v\n", err)
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(stderr, `Run "aesthetics help" for usage.`)
		}
		return report.ExitUsage
	}
	return code
}

type usageError struct{ error }

func (u usageError) Unwrap() error { return u.error }

// listFlag collects comma-separated values from repeated flags.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error {
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// common holds options shared by every command that reads configuration.
type common struct {
	config   string
	use      listFlag
	provider string
	model    string
	format   string
	output   string
	color    string
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.config, "config", "", "configuration file (default: search for .aesthetics.yaml upward)")
	fs.Var(&c.use, "use", "activate predefined or user-defined guideline sets, comma-separated (e.g. unix,kiss)")
	fs.StringVar(&c.provider, "provider", "", "model provider type: "+strings.Join(decision.Types, ", "))
	fs.StringVar(&c.model, "model", "", "model identifier for the provider")
	fs.StringVar(&c.format, "format", "text", "output format: text or json")
	fs.StringVar(&c.output, "output", "", "write the output to this file instead of standard output")
	fs.StringVar(&c.color, "color", "auto", "color text output: auto (terminals only, honoring NO_COLOR), always, or never")
}

// parse parses flags that may appear before, between, or after positional
// arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageError{err}
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		if args[0] == "--" {
			return append(positional, args[1:]...), nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func newFlagSet(name, synopsis string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: aesthetics %s\n\nOptions:\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

// session is the loaded configuration and workspace.
type session struct {
	cwd    string
	cfg    *config.Config
	lib    *guideline.Library
	use    []string
	layers []guideline.Layer
	ws     *artifact.Workspace
}

func (c *common) open() (*session, error) {
	if c.format != "text" && c.format != "json" {
		return nil, usageError{fmt.Errorf("unknown format %q (want text or json)", c.format)}
	}
	if c.color != "auto" && c.color != "always" && c.color != "never" {
		return nil, usageError{fmt.Errorf("unknown color mode %q (want auto, always, or never)", c.color)}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path := c.config
	if path == "" {
		path = config.Find(cwd)
	}
	cfg := &config.Config{}
	root := artifact.FindRoot(cwd)
	if path != "" {
		if cfg, err = config.Load(path); err != nil {
			return nil, err
		}
		root = cfg.Root
	}
	lib, err := cfg.Library()
	if err != nil {
		return nil, err
	}
	for _, id := range c.use {
		if _, ok := lib.Get(id); !ok {
			return nil, usageError{fmt.Errorf("unknown guideline set %q in --use (available: %s)", id, strings.Join(setIDs(lib), ", "))}
		}
	}

	// Provider precedence: flags, then environment, then the configuration file.
	for _, o := range []struct{ typ, model string }{
		{os.Getenv("AESTHETICS_PROVIDER"), os.Getenv("AESTHETICS_MODEL")},
		{c.provider, c.model},
	} {
		if o.typ != "" && !strings.EqualFold(o.typ, cfg.Provider.Type) {
			cfg.Provider = decision.Config{Type: o.typ}
		}
		if o.model != "" {
			cfg.Provider.Model = o.model
		}
	}

	excludes := cfg.Excludes()
	if cfg.Path != "" {
		// The configuration states the guidelines; it is not judged by them.
		excludes = append(excludes, "/"+filepath.Base(cfg.Path))
	}
	ws, err := artifact.Open(root, artifact.Options{Include: cfg.Files.Include, Exclude: excludes, MaxFileBytes: cfg.Files.MaxFileBytes})
	if err != nil {
		return nil, err
	}
	return &session{cwd: cwd, cfg: cfg, lib: lib, use: c.use, layers: cfg.Layers(c.use), ws: ws}, nil
}

func setIDs(lib *guideline.Library) []string {
	var ids []string
	for _, s := range lib.Sets() {
		ids = append(ids, s.ID)
	}
	return ids
}

// hasGuidelines reports whether any layer contributes guidelines.
func (s *session) hasGuidelines() bool {
	return slices.ContainsFunc(s.layers, func(l guideline.Layer) bool {
		return len(l.Extends) > 0 || len(l.Guidelines) > 0
	})
}

// output opens the destination for command output.
func (c *common) out(stdout io.Writer) (io.Writer, func() error, error) {
	if c.output == "" {
		return stdout, func() error { return nil }, nil
	}
	f, err := os.Create(c.output)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

func parseKinds(list []string) ([]guideline.Kind, error) {
	var out []guideline.Kind
	for _, k := range list {
		kind := guideline.Kind(k)
		if !slices.Contains(guideline.Kinds, kind) {
			return nil, usageError{fmt.Errorf("unknown scope %q (want file, directory, repository or diff)", k)}
		}
		out = append(out, kind)
	}
	return out, nil
}

// colorful applies --color: "auto" colors only terminals, and NO_COLOR
// (https://no-color.org) or TERM=dumb turn it off.
func (c *common) colorful(w io.Writer) bool {
	switch c.color {
	case "always":
		return true
	case "never":
		return false
	}
	f, ok := w.(*os.File)
	if !ok || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// columns returns the terminal width from $COLUMNS, clamped to a readable
// range, or 0 for the default.
func columns() int {
	n, err := strconv.Atoi(os.Getenv("COLUMNS"))
	if err != nil {
		return 0
	}
	return min(max(n, 60), 120)
}
