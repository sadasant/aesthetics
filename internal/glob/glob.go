// Package glob matches slash-separated relative paths against glob patterns.
//
// Syntax:
//   - "*" matches any run of characters within one path segment.
//   - "?" matches one character; "[abc]" matches a character class.
//   - "**" as a whole segment matches zero or more segments.
//   - "{a,b}" matches either alternative (alternatives may be empty).
//   - A pattern without "/" matches the final path segment at any depth,
//     like a .gitignore entry: "*.md" matches "docs/guide.md".
package glob

import (
	"fmt"
	"path"
	"strings"
)

// Match reports whether the relative, slash-separated path name matches pattern.
func Match(pattern, name string) bool {
	for _, p := range expandBraces(pattern) {
		if matchOne(p, name) {
			return true
		}
	}
	return false
}

// Any reports whether name matches at least one of the patterns.
func Any(patterns []string, name string) bool {
	for _, p := range patterns {
		if Match(p, name) {
			return true
		}
	}
	return false
}

// Validate reports a syntax error in pattern, if any.
func Validate(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("empty pattern")
	}
	if strings.Count(pattern, "{") != strings.Count(pattern, "}") {
		return fmt.Errorf("unbalanced braces in pattern %q", pattern)
	}
	for _, p := range expandBraces(pattern) {
		for _, seg := range strings.Split(p, "/") {
			if _, err := path.Match(seg, ""); err != nil {
				return fmt.Errorf("invalid pattern %q: %w", pattern, err)
			}
		}
	}
	return nil
}

func matchOne(pattern, name string) bool {
	pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
	if pattern == "" {
		return false
	}
	if pattern == "**" {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(name))
		return err == nil && ok
	}
	pattern = strings.TrimPrefix(pattern, "/")
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pats, segs []string) bool {
	for len(pats) > 0 {
		if pats[0] == "**" {
			rest := pats[1:]
			for i := 0; i <= len(segs); i++ {
				if matchSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		ok, err := path.Match(pats[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pats, segs = pats[1:], segs[1:]
	}
	return len(segs) == 0
}

// expandBraces expands the first top-level {a,b} group recursively.
func expandBraces(pattern string) []string {
	start := strings.IndexByte(pattern, '{')
	if start < 0 {
		return []string{pattern}
	}
	depth := 0
	var parts []string
	last := start + 1
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				parts = append(parts, pattern[last:i])
				var out []string
				for _, alt := range parts {
					out = append(out, expandBraces(pattern[:start]+alt+pattern[i+1:])...)
				}
				return out
			}
		case ',':
			if depth == 1 {
				parts = append(parts, pattern[last:i])
				last = i + 1
			}
		}
	}
	return []string{pattern} // unbalanced: treat literally
}
