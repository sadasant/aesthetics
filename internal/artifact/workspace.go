// Package artifact discovers the artifacts Aesthetics evaluates (files,
// directories, the repository, and Git diffs) and reads them without ever
// modifying the working tree.
package artifact

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/sadasant/aesthetics/internal/glob"
	"github.com/sadasant/aesthetics/internal/guideline"
)

// DefaultExcludes skips dependencies, build output, lockfiles, and minified
// code, which are not authored artifacts.
var DefaultExcludes = []string{
	".git", "node_modules", "vendor", "dist", "build", "target", "out", ".venv", "venv", "__pycache__",
	".next", ".nuxt", "coverage", ".cache", ".idea", ".vscode",
	"*.lock", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "go.sum", "*.min.js", "*.min.css", "*.map",
}

// Options controls which files belong to the workspace.
type Options struct {
	Include      []string
	Exclude      []string
	MaxFileBytes int64
}

// Artifact is one thing to evaluate.
type Artifact struct {
	Kind     guideline.Kind
	Path     string // relative path; "." for the repository; "" for a diff
	Classes  []string
	Role     *Role
	Language string
	Change   *FileDiff // set for a file evaluated in the context of a diff
	Diff     *Diff     // set for a diff
	Explicit bool      // named directly on the command line
}

// Label names the artifact for humans.
func (a *Artifact) Label() string {
	switch a.Kind {
	case guideline.Repository:
		return "(repository)"
	case guideline.Diff:
		return "(diff " + a.Diff.Label + ")"
	}
	if a.Kind == guideline.Directory {
		return a.Path + "/"
	}
	return a.Path
}

// Target describes the artifact for guideline scope matching.
func (a *Artifact) Target() guideline.Target {
	t := guideline.Target{Kind: a.Kind, Path: a.Path, Classes: a.Classes}
	if a.Diff != nil {
		for _, f := range a.Diff.Files {
			t.Paths = append(t.Paths, f.Path)
		}
	}
	return t
}

// Workspace is a read-only view of the repository being evaluated.
type Workspace struct {
	Root  string
	Files []string // included files, sorted, slash-separated, relative to Root
	IsGit bool

	opts  Options
	mu    sync.Mutex
	cache map[string]*fileData
}

type fileData struct {
	content []byte
	binary  bool
	size    int64
	err     error
}

// FindRoot returns the Git top-level directory containing dir, or dir.
func FindRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return filepath.Clean(p)
		}
	}
	return dir
}

// Open lists the files of the workspace rooted at root.
func Open(root string, opts Options) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = 256 << 10
	}
	w := &Workspace{Root: abs, opts: opts, cache: map[string]*fileData{}}
	files, isGit := w.gitFiles()
	w.IsGit = isGit
	if !isGit {
		if files, err = w.walkFiles(); err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		if w.included(f) {
			w.Files = append(w.Files, f)
		}
	}
	sort.Strings(w.Files)
	return w, nil
}

func (w *Workspace) included(rel string) bool {
	if len(w.opts.Include) > 0 && !glob.Any(w.opts.Include, rel) {
		return false
	}
	return !w.Excluded(rel)
}

// Excluded reports whether rel or one of its parent directories is excluded.
func (w *Workspace) Excluded(rel string) bool {
	parts := strings.Split(rel, "/")
	for i := range parts {
		if glob.Any(w.opts.Exclude, strings.Join(parts[:i+1], "/")) {
			return true
		}
	}
	return false
}

// gitFiles lists tracked and untracked, non-ignored files.
func (w *Workspace) gitFiles() ([]string, bool) {
	cmd := exec.Command("git", "-C", w.Root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	// Paths are relative to Root, even when Root is below the Git top level.
	var files []string
	seen := map[string]bool{}
	for _, f := range bytes.Split(out, []byte{0}) {
		rel := string(f)
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		if info, err := os.Lstat(filepath.Join(w.Root, filepath.FromSlash(rel))); err == nil && info.Mode().IsRegular() {
			files = append(files, rel)
		}
	}
	return files, true
}

func (w *Workspace) walkFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(w.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		rel, _ := filepath.Rel(w.Root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || w.Excluded(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func (w *Workspace) load(rel string) *fileData {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fd, ok := w.cache[rel]; ok {
		return fd
	}
	fd := &fileData{}
	p := filepath.Join(w.Root, filepath.FromSlash(rel))
	if info, err := os.Stat(p); err != nil {
		fd.err = err
	} else {
		fd.size = info.Size()
		if fd.size <= w.opts.MaxFileBytes {
			fd.content, fd.err = os.ReadFile(p)
			fd.binary = isBinary(fd.content)
		}
	}
	w.cache[rel] = fd
	return fd
}

func isBinary(b []byte) bool {
	head := b[:min(len(b), 8000)]
	return bytes.IndexByte(head, 0) >= 0
}

// ErrSkipped explains why a file cannot be evaluated.
var ErrSkipped = errors.New("skipped")

// Read returns the text content of a file in the working tree.
func (w *Workspace) Read(rel string) (string, error) {
	fd := w.load(rel)
	switch {
	case fd.err != nil:
		return "", fd.err
	case fd.size > w.opts.MaxFileBytes:
		return "", fmt.Errorf("%w: file is %d bytes, larger than the %d-byte limit (files.max_file_bytes)", ErrSkipped, fd.size, w.opts.MaxFileBytes)
	case fd.binary:
		return "", fmt.Errorf("%w: binary file", ErrSkipped)
	}
	return string(fd.content), nil
}

// LineCount returns the number of lines of a readable text file, or -1.
func (w *Workspace) LineCount(rel string) int {
	s, err := w.Read(rel)
	if err != nil {
		return -1
	}
	return countLines(s)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// FilesUnder returns included files below dir ("." for all files).
func (w *Workspace) FilesUnder(dir string) []string {
	if dir == "." || dir == "" {
		return w.Files
	}
	var out []string
	for _, f := range w.Files {
		if strings.HasPrefix(f, dir+"/") {
			out = append(out, f)
		}
	}
	return out
}

// Dirs returns every directory that contains included files, below dir.
func (w *Workspace) Dirs(dir string) []string {
	seen := map[string]bool{}
	for _, f := range w.FilesUnder(dir) {
		for d := path.Dir(f); d != "." && d != dir; d = path.Dir(d) {
			seen[d] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ClassesUnder returns the union of content classes of files below dir.
func (w *Workspace) ClassesUnder(dir string) []string {
	set := map[string]bool{}
	for _, f := range w.FilesUnder(dir) {
		for _, c := range Classify(f) {
			set[c] = true
		}
	}
	var out []string
	for _, c := range guideline.Classes {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

// FileArtifact describes a file.
func (w *Workspace) FileArtifact(rel string) *Artifact {
	classes := Classify(rel)
	role := RoleOf(rel, classes)
	return &Artifact{Kind: guideline.File, Path: rel, Classes: classes, Role: &role, Language: Language(rel)}
}

// DirArtifact describes a directory.
func (w *Workspace) DirArtifact(rel string) *Artifact {
	return &Artifact{Kind: guideline.Directory, Path: rel, Classes: w.ClassesUnder(rel)}
}

// RepoArtifact describes the repository as a whole.
func (w *Workspace) RepoArtifact() *Artifact {
	return &Artifact{Kind: guideline.Repository, Path: ".", Classes: w.ClassesUnder(".")}
}

// Rel converts a command-line path into a root-relative, slash-separated path.
func (w *Workspace) Rel(cwd, arg string) (string, error) {
	p := arg
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	rel, err := filepath.Rel(w.Root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository root %s", arg, w.Root)
	}
	return filepath.ToSlash(rel), nil
}

// Select expands command-line paths into artifacts: a file selects itself;
// a directory selects itself, its subdirectories, and its files; no paths
// (or the root) select the repository, every directory, and every file.
// The result lists files, then directories, then the repository.
func (w *Workspace) Select(cwd string, args []string, kinds []guideline.Kind) ([]*Artifact, error) {
	if len(args) == 0 {
		args = []string{w.Root}
	}
	var files, dirs []string
	repo := false
	explicit := map[string]bool{}
	for _, arg := range args {
		rel, err := w.Rel(cwd, arg)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(filepath.Join(w.Root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("cannot evaluate %s: %w", arg, err)
		}
		switch {
		case rel == ".":
			repo = true
			files = append(files, w.Files...)
			dirs = append(dirs, w.Dirs(".")...)
		case info.IsDir():
			dirs = append(dirs, rel)
			dirs = append(dirs, w.Dirs(rel)...)
			files = append(files, w.FilesUnder(rel)...)
			explicit[rel+"/"] = true
		default:
			files = append(files, rel)
			explicit[rel] = true
		}
	}
	want := func(k guideline.Kind) bool { return len(kinds) == 0 || slices.Contains(kinds, k) }
	var out []*Artifact
	if want(guideline.File) {
		for _, f := range dedupe(files) {
			a := w.FileArtifact(f)
			a.Explicit = explicit[f]
			out = append(out, a)
		}
	}
	if want(guideline.Directory) {
		for _, d := range dedupe(dirs) {
			a := w.DirArtifact(d)
			a.Explicit = explicit[d+"/"]
			out = append(out, a)
		}
	}
	if repo && want(guideline.Repository) {
		out = append(out, w.RepoArtifact())
	}
	return out, nil
}

func dedupe(items []string) []string {
	sort.Strings(items)
	return slices.Compact(items)
}
