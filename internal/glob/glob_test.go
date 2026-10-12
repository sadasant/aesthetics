package glob

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"*.md", "docs/guide.md", true}, // no slash: basename at any depth
		{"*.md", "docs/guide.mdx", false},
		{"docs/*.md", "docs/guide.md", true},
		{"docs/*.md", "docs/a/guide.md", false},
		{"**/README.md", "README.md", true}, // ** matches zero segments
		{"**/README.md", "a/b/README.md", true},
		{"src/**/utils/**", "src/utils", true},
		{"src/**/utils/**", "src/a/utils/x/y.ts", true},
		{"src/**/utils/**", "lib/utils/x.ts", false},
		{"{README,readme}{,.md}", "pkg/readme", true},
		{"{README,readme}{,.md}", "pkg/README.md", true},
		{"{README,readme}{,.md}", "pkg/README.rst", false},
		{"index.{ts,js}", "src/index.js", true},
		{"/src/*.go", "src/main.go", true},
		{"**", "anything/at/all", true},
	}
	for _, tt := range tests {
		if got := Match(tt.pattern, tt.name); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, bad := range []string{"", "[a", "{a,b"} {
		if Validate(bad) == nil {
			t.Errorf("Validate(%q) = nil, want error", bad)
		}
	}
}
