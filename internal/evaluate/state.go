package evaluate

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/sadasant/aesthetics/internal/artifact"
	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
)

// Location is a part of the state that evidence can point to.
type Location struct {
	Value     string
	Path      string
	StartLine int
	EndLine   int
}

// StateInfo describes what the state contains.
type StateInfo struct {
	Chars     int
	Truncated bool
	Included  []string // other files whose content was included as context
	Locations []Location
	Facts     map[string]any
	// ContentKey names the state field holding the artifact's text.
	ContentKey string
}

// budget hands out characters of the state allowance.
type budget struct{ left int }

func (b *budget) take(s string, limit int) (string, bool) {
	limit = min(limit, b.left)
	if limit < 0 {
		limit = 0
	}
	if len(s) <= limit {
		b.left -= len(s)
		return s, false
	}
	cut := limit
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 { // keep UTF-8 intact
		cut--
	}
	out := s[:cut] + "\n[… truncated]"
	b.left -= len(out)
	return out, true
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// numbered renders lines prefixed with their line numbers, up to limit
// characters. It returns the text and the last line included.
func numbered(lines []string, limit int) (string, int) {
	var b strings.Builder
	last := 0
	for i, l := range lines {
		row := fmt.Sprintf("%d| %s\n", i+1, l)
		if b.Len()+len(row) > limit {
			break
		}
		b.WriteString(row)
		last = i + 1
	}
	return b.String(), last
}

func head(content string, n int) string {
	lines := splitLines(content)
	if len(lines) > n {
		return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n[… %d more lines]", len(lines)-n)
	}
	return strings.Join(lines, "\n")
}

var keyRoles = map[string]bool{"readme": true, "package manifest": true, "module index": true, "entry point": true, "crate root": true, "module root": true}

// buildState assembles the evidence sent to the decision model.
func (e *Engine) buildState(a *artifact.Artifact, context []string) (decision.State, *StateInfo, error) {
	b := &budget{left: e.maxState()}
	info := &StateInfo{Facts: map[string]any{"kind": string(a.Kind)}}
	var state decision.State
	var err error
	switch a.Kind {
	case guideline.File:
		state, err = e.fileState(a, b, info)
	case guideline.Directory:
		state = e.dirState(a, b, info)
	case guideline.Repository:
		state = e.repoState(a, b, info)
	case guideline.Diff:
		state = e.diffState(a, b, info)
	}
	if err != nil {
		return nil, info, err
	}
	if len(context) > 0 {
		// Rule instructions apply to every guideline on this artifact.
		state = append(state[:1], append(decision.State{{Key: "context", Value: context}}, state[1:]...)...)
	}
	info.Chars = len(state.Text())
	info.Facts["state_chars"] = info.Chars
	info.Facts["state_truncated"] = info.Truncated
	return state, info, nil
}

func (e *Engine) fileState(a *artifact.Artifact, b *budget, info *StateInfo) (decision.State, error) {
	ws := e.WS
	var content string
	var err error
	if a.Change != nil {
		content, err = ws.ReadAfter(a.Change.Parent(), a.Path)
	} else {
		content, err = ws.Read(a.Path)
	}
	if err != nil {
		return nil, err
	}
	lines := splitLines(content)
	meta := map[string]any{"kind": "file", "path": a.Path, "lines": len(lines)}
	info.Facts["path"] = a.Path
	info.Facts["lines"] = len(lines)
	info.Facts["bytes"] = len(content)
	info.Facts["classes"] = a.Classes
	if a.Role != nil {
		meta["role"], meta["role_description"] = a.Role.Name, a.Role.Description
		info.Facts["role"] = a.Role.Name
	}
	if a.Language != "" {
		meta["language"] = a.Language
		info.Facts["language"] = a.Language
	}
	state := decision.State{{Key: "artifact", Value: meta}}

	if fd := a.Change; fd != nil {
		change := map[string]any{"status": fd.Status, "additions": fd.Additions, "deletions": fd.Deletions}
		if fd.OldPath != "" {
			change["old_path"] = fd.OldPath
		}
		info.Facts["change"] = change
		state = append(state, decision.Field{Key: "change", Value: change})
		text, cut := b.take(fd.Text, b.left*35/100)
		info.Truncated = info.Truncated || cut
		state = append(state, decision.Field{Key: "diff", Value: text})
	}

	text, last := numbered(lines, b.left*80/100)
	b.left -= len(text)
	if last < len(lines) {
		info.Truncated = true
		text += fmt.Sprintf("[… lines %d-%d truncated]\n", last+1, len(lines))
	}
	info.ContentKey = "content"
	state = append(state, decision.Field{Key: "content", Value: text})
	info.Locations = lineLocations(a.Path, last)

	dir := path.Dir(a.Path)
	var siblings []string
	for _, f := range ws.Files {
		if f != a.Path && path.Dir(f) == dir && len(siblings) < 50 {
			siblings = append(siblings, path.Base(f))
		}
	}
	if len(siblings) > 0 {
		state = append(state, decision.Field{Key: "siblings", Value: siblings})
	}
	var related []map[string]string
	for _, r := range relatedFiles(a.Path, ws.Files) {
		c, err := ws.Read(r.path)
		if err != nil || b.left < 400 {
			continue
		}
		excerpt, cut := b.take(head(c, 80), b.left/2)
		info.Truncated = info.Truncated || cut
		related = append(related, map[string]string{"path": r.path, "relation": r.relation, "excerpt": excerpt})
		info.Included = append(info.Included, r.path)
	}
	if len(related) > 0 {
		state = append(state, decision.Field{Key: "related", Value: related})
	}
	return state, nil
}

// lineLocations splits lines 1..n into at most ~30 segments.
func lineLocations(p string, n int) []Location {
	if n == 0 {
		return nil
	}
	size := max(10, (n+29)/30)
	var out []Location
	for start := 1; start <= n; start += size {
		end := min(n, start+size-1)
		out = append(out, Location{Value: fmt.Sprintf("lines %d-%d", start, end), Path: p, StartLine: start, EndLine: end})
	}
	return out
}

type relation struct{ path, relation string }

// relatedFiles pairs tests with the code they test.
func relatedFiles(p string, files []string) []relation {
	dir, base := path.Dir(p), path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	var cands []relation
	switch {
	case strings.HasSuffix(stem, "_test"):
		cands = append(cands, relation{strings.TrimSuffix(stem, "_test") + ext, "code under test"})
	case strings.HasSuffix(stem, ".test"), strings.HasSuffix(stem, ".spec"):
		s := strings.TrimSuffix(strings.TrimSuffix(stem, ".test"), ".spec")
		cands = append(cands, relation{s + ext, "code under test"}, relation{s + ".tsx", "code under test"})
	case strings.HasPrefix(stem, "test_"):
		cands = append(cands, relation{strings.TrimPrefix(stem, "test_") + ext, "code under test"})
	default:
		cands = append(cands,
			relation{stem + "_test" + ext, "tests"}, relation{stem + ".test" + ext, "tests"},
			relation{stem + ".spec" + ext, "tests"}, relation{"test_" + stem + ext, "tests"})
	}
	var out []relation
	for _, c := range cands {
		full := path.Join(dir, c.path)
		for _, f := range files {
			if f == full && f != p {
				out = append(out, relation{full, c.relation})
			}
		}
	}
	return out
}

// tree lists files below dir. It shows as many levels as fit in maxEntries
// entries, but at least minDepth, collapsing deeper levels into counts, so
// deep nesting stays visible whenever the listing is small enough.
// It returns the listing and the direct children of dir.
func (e *Engine) tree(dir string, minDepth, maxEntries int) (string, []string) {
	files := e.WS.FilesUnder(dir)
	prefix := ""
	if dir != "." {
		prefix = dir + "/"
	}
	type node struct {
		files int
		lines int
		isDir bool
	}
	childSet := map[string]bool{}
	build := func(depth int) map[string]*node {
		nodes := map[string]*node{}
		for _, f := range files {
			parts := strings.Split(strings.TrimPrefix(f, prefix), "/")
			for i := 1; i <= depth && i < len(parts); i++ {
				d := strings.Join(parts[:i], "/") + "/"
				if nodes[d] == nil {
					nodes[d] = &node{isDir: true}
				}
				nodes[d].files++
			}
			if len(parts) <= depth {
				nodes[strings.Join(parts, "/")] = &node{lines: e.WS.LineCount(f)}
			}
		}
		return nodes
	}
	for _, f := range files {
		parts := strings.SplitN(strings.TrimPrefix(f, prefix), "/", 2)
		child := parts[0]
		if len(parts) > 1 {
			child += "/"
		}
		childSet[child] = true
	}
	var nodes map[string]*node
	for depth := 16; depth >= minDepth; depth-- {
		if nodes = build(depth); len(nodes) <= maxEntries {
			break
		}
	}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i == maxEntries {
			fmt.Fprintf(&b, "[… %d more entries]\n", len(keys)-maxEntries)
			break
		}
		n := nodes[k]
		indent := strings.Repeat("  ", strings.Count(strings.TrimSuffix(k, "/"), "/"))
		name := path.Base(strings.TrimSuffix(k, "/"))
		switch {
		case n.isDir:
			fmt.Fprintf(&b, "%s%s/ (%d files)\n", indent, name, n.files)
		case n.lines >= 0:
			fmt.Fprintf(&b, "%s%s (%d lines)\n", indent, name, n.lines)
		default:
			fmt.Fprintf(&b, "%s%s (binary or large)\n", indent, name)
		}
	}
	children := make([]string, 0, len(childSet))
	for c := range childSet {
		children = append(children, c)
	}
	sort.Strings(children)
	return b.String(), children
}

func entryLocations(dir string, children []string) []Location {
	if len(children) > 60 {
		children = children[:60]
	}
	var out []Location
	for _, c := range children {
		p := strings.TrimSuffix(c, "/")
		if dir != "." {
			p = dir + "/" + p
		}
		out = append(out, Location{Value: p, Path: p})
	}
	return out
}

func (e *Engine) dirState(a *artifact.Artifact, b *budget, info *StateInfo) decision.State {
	ws := e.WS
	files := ws.FilesUnder(a.Path)
	subdirs := ws.Dirs(a.Path)
	meta := map[string]any{"kind": "directory", "path": a.Path, "files": len(files), "subdirectories": len(subdirs), "classes": a.Classes}
	info.Facts["path"] = a.Path
	info.Facts["files"] = len(files)
	info.Facts["subdirectories"] = len(subdirs)
	info.Facts["classes"] = a.Classes
	tree, children := e.tree(a.Path, 3, 200)
	tree, cut := b.take(tree, b.left/4)
	info.Truncated = cut
	state := decision.State{{Key: "artifact", Value: meta}, {Key: "tree", Value: tree}}
	info.Locations = entryLocations(a.Path, children)

	var direct []string
	for _, f := range files {
		if path.Dir(f) == a.Path {
			direct = append(direct, f)
		}
	}
	key, rest := splitKeyFiles(direct)
	state = e.appendFiles(state, "key_files", key, b, info, 0, b.left/2)
	state = e.appendFiles(state, "excerpts", rest, b, info, 30, b.left/2)
	// The opening of one file per subdirectory shows what each part is for,
	// which is all a directory of subdirectories can be judged by.
	var reps []string
	for _, c := range children {
		if !strings.HasSuffix(c, "/") {
			continue
		}
		sub := path.Join(a.Path, strings.TrimSuffix(c, "/"))
		var inSub []string
		for _, f := range files {
			if path.Dir(f) == sub {
				inSub = append(inSub, f)
			}
		}
		if k, r := splitKeyFiles(inSub); len(k) > 0 {
			reps = append(reps, k[0])
		} else if len(r) > 0 {
			reps = append(reps, r[0])
		}
	}
	return e.appendFiles(state, "subdirectory_samples", reps, b, info, 12, b.left)
}

func splitKeyFiles(files []string) (key, rest []string) {
	for _, f := range files {
		if keyRoles[artifact.RoleOf(f, artifact.Classify(f)).Name] {
			key = append(key, f)
		} else {
			rest = append(rest, f)
		}
	}
	return key, rest
}

// appendFiles adds file contents (or their first headLines lines) to the
// state within limit characters.
func (e *Engine) appendFiles(state decision.State, keyName string, files []string, b *budget, info *StateInfo, headLines, limit int) decision.State {
	var items []map[string]string
	used := 0
	for _, f := range files {
		c, err := e.WS.Read(f)
		if err != nil {
			continue
		}
		if headLines > 0 {
			c = head(c, headLines)
		}
		room := min(limit-used, b.left)
		if room < 200 {
			info.Truncated = true
			break
		}
		before := b.left
		text, cut := b.take(c, room)
		used += before - b.left
		info.Truncated = info.Truncated || cut
		items = append(items, map[string]string{"path": f, "role": artifact.RoleOf(f, artifact.Classify(f)).Name, "content": text})
		info.Included = append(info.Included, f)
	}
	if len(items) == 0 {
		return state
	}
	return append(state, decision.Field{Key: keyName, Value: items})
}

func (e *Engine) repoState(a *artifact.Artifact, b *budget, info *StateInfo) decision.State {
	ws := e.WS
	languages := map[string]int{}
	classes := map[string]int{}
	var entries []string
	for _, f := range ws.Files {
		if l := artifact.Language(f); l != "" {
			languages[l]++
		}
		for _, c := range artifact.Classify(f) {
			classes[c]++
		}
		role := artifact.RoleOf(f, artifact.Classify(f)).Name
		if (role == "entry point" || role == "module index" || role == "crate root") && len(entries) < 6 {
			entries = append(entries, f)
		}
	}
	meta := map[string]any{"kind": "repository", "files": len(ws.Files), "directories": len(ws.Dirs(".")), "classes": classes, "languages": languages}
	for k, v := range meta {
		if k != "kind" {
			info.Facts[k] = v
		}
	}
	tree, children := e.tree(".", 2, 300)
	tree, cut := b.take(tree, b.left/4)
	info.Truncated = cut
	state := decision.State{{Key: "artifact", Value: meta}, {Key: "tree", Value: tree}}
	info.Locations = entryLocations(".", children)

	var root []string
	for _, f := range ws.Files {
		if !strings.Contains(f, "/") {
			root = append(root, f)
		}
	}
	key, _ := splitKeyFiles(root)
	state = e.appendFiles(state, "key_files", key, b, info, 0, b.left*45/100)
	var nested []string
	for _, f := range entries {
		if strings.Contains(f, "/") {
			nested = append(nested, f)
		}
	}
	state = e.appendFiles(state, "entry_points", nested, b, info, 40, b.left/2)
	var readmes []string
	for _, f := range ws.Files {
		if strings.Count(f, "/") == 1 && artifact.RoleOf(f, artifact.Classify(f)).Name == "readme" {
			readmes = append(readmes, f)
		}
	}
	return e.appendFiles(state, "component_readmes", readmes, b, info, 25, b.left)
}

func (e *Engine) diffState(a *artifact.Artifact, b *budget, info *StateInfo) decision.State {
	d := a.Diff
	meta := map[string]any{"kind": "diff", "compared": d.Label, "files_changed": len(d.Files), "additions": d.Additions, "deletions": d.Deletions}
	info.Facts["compared"] = d.Label
	info.Facts["files_changed"] = len(d.Files)
	info.Facts["additions"] = d.Additions
	info.Facts["deletions"] = d.Deletions
	var changed []string
	for _, f := range d.Files {
		changed = append(changed, fmt.Sprintf("%s %s (+%d -%d)", f.Status, f.Path, f.Additions, f.Deletions))
		if len(info.Locations) < 60 {
			info.Locations = append(info.Locations, Location{Value: f.Path, Path: f.Path})
		}
	}
	text, cut := b.take(d.Text, b.left-2000)
	info.Truncated = cut
	info.ContentKey = "diff"
	return decision.State{{Key: "artifact", Value: meta}, {Key: "changed_files", Value: changed}, {Key: "diff", Value: text}}
}
