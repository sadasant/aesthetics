package artifact

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// DiffOptions selects which Git changes to evaluate.
type DiffOptions struct {
	Base   string   // compare HEAD with the merge base of Base and HEAD
	Staged bool     // compare the index with HEAD
	Paths  []string // restrict to these root-relative paths
}

// Diff is a set of Git changes.
type Diff struct {
	Label     string      `json:"label"`
	Base      string      `json:"base,omitempty"`
	Staged    bool        `json:"staged,omitempty"`
	Files     []*FileDiff `json:"files"`
	Text      string      `json:"-"`
	Additions int         `json:"additions"`
	Deletions int         `json:"deletions"`

	revision string // where post-change content lives: "" (working tree), ":" (index), or a commit
}

// FileDiff is the change to one file.
type FileDiff struct {
	Path      string       `json:"path"`
	OldPath   string       `json:"old_path,omitempty"`
	Status    string       `json:"status"` // added, modified, deleted, renamed
	Binary    bool         `json:"binary,omitempty"`
	Additions int          `json:"additions"`
	Deletions int          `json:"deletions"`
	Added     map[int]bool `json:"-"` // line numbers added or modified in the new file
	Text      string       `json:"-"`

	parent *Diff
}

// Parent returns the diff this file change belongs to.
func (f *FileDiff) Parent() *Diff { return f.parent }

// AddedIn reports whether any line in [start, end] was added or modified.
func (f *FileDiff) AddedIn(start, end int) bool {
	for l := start; l <= end; l++ {
		if f.Added[l] {
			return true
		}
	}
	return false
}

func (w *Workspace) git(args ...string) ([]byte, error) {
	full := append([]string{"-C", w.Root, "-c", "core.quotepath=off"}, args...)
	cmd := exec.Command("git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Diff collects Git changes. Without options it covers staged and unstaged
// changes plus untracked files, relative to HEAD.
func (w *Workspace) Diff(opts DiffOptions) (*Diff, error) {
	if !w.IsGit {
		return nil, fmt.Errorf("%s is not inside a Git repository", w.Root)
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "-U5", "-M", "--relative", "--src-prefix=a/", "--dst-prefix=b/"}
	d := &Diff{Base: opts.Base, Staged: opts.Staged}
	head := "HEAD"
	if _, err := w.git("rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		head = emptyTree // repository without commits
	}
	switch {
	case opts.Base != "":
		args = append(args, opts.Base+"...HEAD")
		d.Label = opts.Base + "...HEAD"
		d.revision = "HEAD"
	case opts.Staged:
		args = append(args, "--cached", head)
		d.Label = "staged"
		d.revision = ":"
	default:
		args = append(args, head)
		d.Label = "working tree"
	}
	args = append(args, "--")
	args = append(args, opts.Paths...)
	out, err := w.git(args...)
	if err != nil {
		return nil, err
	}
	d.Text = string(out)
	d.Files = parseDiff(d.Text)
	if opts.Base == "" && !opts.Staged {
		if err := w.addUntracked(d, opts.Paths); err != nil {
			return nil, err
		}
	}
	for _, f := range d.Files {
		f.parent = d
		d.Additions += f.Additions
		d.Deletions += f.Deletions
	}
	return d, nil
}

func (w *Workspace) addUntracked(d *Diff, paths []string) error {
	out, err := w.git(append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, paths...)...)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(d.Text)
	for _, raw := range bytes.Split(out, []byte{0}) {
		rel := string(raw)
		if rel == "" || w.Excluded(rel) {
			continue
		}
		f := &FileDiff{Path: rel, Status: "added", Added: map[int]bool{}}
		content, err := w.Read(rel)
		if err != nil {
			f.Binary = true
			f.Text = fmt.Sprintf("diff --git a/%s b/%s\nnew file\nBinary or oversized file %s\n", rel, rel, rel)
		} else {
			var t strings.Builder
			lines := splitLines(content)
			fmt.Fprintf(&t, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", rel, rel, rel, len(lines))
			for i, l := range lines {
				t.WriteString("+" + l + "\n")
				f.Added[i+1] = true
			}
			f.Additions = len(lines)
			f.Text = t.String()
		}
		b.WriteString(f.Text)
		d.Files = append(d.Files, f)
	}
	d.Text = b.String()
	return nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// ReadAfter returns a file's content after the change.
func (w *Workspace) ReadAfter(d *Diff, rel string) (string, error) {
	if d == nil || d.revision == "" {
		return w.Read(rel)
	}
	out, err := w.git("show", d.revision+":./"+rel)
	if err != nil {
		return "", err
	}
	if isBinary(out) {
		return "", fmt.Errorf("%w: binary file", ErrSkipped)
	}
	if int64(len(out)) > w.opts.MaxFileBytes {
		return "", fmt.Errorf("%w: file is larger than the %d-byte limit", ErrSkipped, w.opts.MaxFileBytes)
	}
	return string(out), nil
}

// parseDiff parses `git diff` output into per-file changes.
func parseDiff(text string) []*FileDiff {
	var files []*FileDiff
	var cur *FileDiff
	var b strings.Builder
	newLine := 0
	inHunk := false
	flush := func() {
		if cur != nil {
			cur.Text = b.String()
			files = append(files, cur)
		}
		b.Reset()
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		l := strings.TrimRight(line, "\n")
		if strings.HasPrefix(l, "diff --git ") {
			flush()
			cur = &FileDiff{Status: "modified", Added: map[int]bool{}}
			if i := strings.LastIndex(l, " b/"); i >= 0 {
				cur.Path = l[i+3:]
			}
			inHunk = false
			b.WriteString(line)
			continue
		}
		if cur == nil {
			continue
		}
		b.WriteString(line)
		if !inHunk {
			switch {
			case strings.HasPrefix(l, "new file mode"):
				cur.Status = "added"
			case strings.HasPrefix(l, "deleted file mode"):
				cur.Status = "deleted"
			case strings.HasPrefix(l, "rename from "):
				cur.Status, cur.OldPath = "renamed", strings.TrimPrefix(l, "rename from ")
			case strings.HasPrefix(l, "rename to "):
				cur.Path = strings.TrimPrefix(l, "rename to ")
			case strings.HasPrefix(l, "Binary files "):
				cur.Binary = true
			case strings.HasPrefix(l, "+++ "):
				if p := strings.TrimRight(strings.TrimPrefix(l, "+++ "), "\t"); p != "/dev/null" {
					cur.Path = strings.TrimPrefix(p, "b/")
				}
			case strings.HasPrefix(l, "--- "):
				if p := strings.TrimRight(strings.TrimPrefix(l, "--- "), "\t"); p != "/dev/null" && cur.Path == "" {
					cur.Path = strings.TrimPrefix(p, "a/")
				}
			}
		}
		if strings.HasPrefix(l, "@@ ") {
			inHunk = true
			newLine = hunkStart(l)
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			cur.Added[newLine] = true
			cur.Additions++
			newLine++
		case strings.HasPrefix(l, "-"):
			cur.Deletions++
		case strings.HasPrefix(l, " "), l == "":
			newLine++
		}
	}
	flush()
	return files
}

// hunkStart parses the new-file start line from "@@ -a,b +c,d @@".
func hunkStart(l string) int {
	i := strings.Index(l, "+")
	if i < 0 {
		return 1
	}
	rest := l[i+1:]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 1
	}
	return n
}
