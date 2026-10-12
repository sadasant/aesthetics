package artifact

import (
	"path"
	"strings"

	"github.com/sadasant/aesthetics/internal/glob"
)

var languages = map[string]string{
	".go": "Go", ".rs": "Rust", ".ts": "TypeScript", ".tsx": "TypeScript", ".mts": "TypeScript", ".cts": "TypeScript",
	".js": "JavaScript", ".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".py": "Python", ".rb": "Ruby", ".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin", ".scala": "Scala",
	".swift": "Swift", ".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".cxx": "C++", ".hpp": "C++", ".hh": "C++",
	".cs": "C#", ".fs": "F#", ".php": "PHP", ".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".hs": "Haskell",
	".ml": "OCaml", ".clj": "Clojure", ".lua": "Lua", ".dart": "Dart", ".sh": "Shell", ".bash": "Shell", ".zsh": "Shell",
	".fish": "Shell", ".ps1": "PowerShell", ".sql": "SQL", ".r": "R", ".jl": "Julia", ".zig": "Zig", ".nim": "Nim",
	".vue": "Vue", ".svelte": "Svelte", ".m": "Objective-C", ".pl": "Perl", ".groovy": "Groovy", ".elm": "Elm",
	".gleam": "Gleam", ".v": "V", ".sol": "Solidity",
}

var docExts = map[string]bool{".md": true, ".markdown": true, ".mdx": true, ".rst": true, ".adoc": true, ".asciidoc": true, ".org": true, ".txt": true}
var configExts = map[string]bool{".json": true, ".jsonc": true, ".json5": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".cfg": true, ".conf": true, ".properties": true, ".xml": true, ".tf": true, ".hcl": true, ".nix": true, ".gradle": true, ".cmake": true, ".mk": true}
var dataExts = map[string]bool{".csv": true, ".tsv": true, ".jsonl": true, ".ndjson": true, ".parquet": true, ".sqlite": true, ".db": true}

var configNames = map[string]bool{
	"Makefile": true, "makefile": true, "GNUmakefile": true, "Dockerfile": true, "Containerfile": true, "Justfile": true, "justfile": true,
	"Procfile": true, "CMakeLists.txt": true, "go.mod": true, "go.work": true, "Gemfile": true, "Pipfile": true, "BUILD": true,
	"WORKSPACE": true, ".gitignore": true, ".gitattributes": true, ".editorconfig": true, ".dockerignore": true, ".npmrc": true,
	".nvmrc": true, ".prettierrc": true, ".eslintrc": true, "requirements.txt": true, "CODEOWNERS": true,
}

var docNames = []string{"README", "LICENSE", "LICENCE", "CHANGELOG", "CONTRIBUTING", "AUTHORS", "NOTICE", "COPYING", "SECURITY", "CODE_OF_CONDUCT"}

var testPatterns = []string{
	"*_test.*", "*.test.*", "*.spec.*", "test_*.py", "*_spec.rb", "*Test.java", "*Tests.java", "*Test.kt", "*Tests.cs",
	"**/test/**", "**/tests/**", "**/__tests__/**", "**/spec/**", "test/**", "tests/**", "spec/**",
}

// Language returns the programming language of a file, if recognized.
func Language(p string) string {
	return languages[strings.ToLower(path.Ext(p))]
}

// Classify returns the content classes of a file path.
func Classify(p string) []string {
	base := path.Base(p)
	ext := strings.ToLower(path.Ext(p))
	switch {
	case configNames[base]:
		return []string{"config"}
	case isDocName(base):
		return []string{"docs"}
	case languages[ext] != "":
		if glob.Any(testPatterns, p) {
			return []string{"code", "test"}
		}
		return []string{"code"}
	case docExts[ext]:
		return []string{"docs"}
	case configExts[ext] || strings.HasPrefix(base, ".") && ext == "":
		return []string{"config"}
	case dataExts[ext]:
		return []string{"data"}
	}
	return []string{"other"}
}

func isDocName(base string) bool {
	upper := strings.ToUpper(strings.TrimSuffix(base, path.Ext(base)))
	for _, n := range docNames {
		if upper == n {
			return true
		}
	}
	return false
}

// Role describes the purpose a file plays, which shapes how it is judged.
type Role struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type roleRule struct {
	patterns []string
	role     Role
}

var roleRules = []roleRule{
	{[]string{"{README,Readme,readme}{,.*}"}, Role{"readme", "Introduces the project or directory: explains its purpose and orients readers."}},
	{[]string{"package.json", "Cargo.toml", "pyproject.toml", "setup.py", "setup.cfg", "go.mod", "composer.json", "*.gemspec", "pom.xml", "build.gradle", "build.gradle.kts", "deno.json", "*.csproj", "mix.exs"},
		Role{"package manifest", "Declares the package's identity, public interface, scripts, and dependencies."}},
	{[]string{"index.{ts,tsx,js,jsx,mjs,cjs}", "mod.ts"}, Role{"module index", "Defines the module's exports and its interface boundary."}},
	{[]string{"main.*", "__main__.py", "Main.java", "Program.cs"}, Role{"entry point", "Starts the program and wires its components together."}},
	{[]string{"lib.rs"}, Role{"crate root", "Organizes the crate and declares its public API."}},
	{[]string{"mod.rs", "__init__.py"}, Role{"module root", "Declares the module's contents and what it exposes."}},
	{[]string{"Dockerfile", "Containerfile", "Makefile", "Justfile", ".github/workflows/*", ".gitlab-ci.yml", "Jenkinsfile"}, Role{"build or CI configuration", "Defines how the project is built, packaged, tested, or deployed."}},
	{[]string{"{CHANGELOG,HISTORY}{,.*}"}, Role{"changelog", "Records notable changes between releases."}},
	{[]string{"{CONTRIBUTING}{,.*}"}, Role{"contributing guide", "Explains how to contribute to the project."}},
	{[]string{"{LICENSE,LICENCE,COPYING}{,.*}"}, Role{"license", "States the legal terms of use."}},
}

// RoleOf returns the role a file plays based on its name, location, and class.
func RoleOf(p string, classes []string) Role {
	for _, r := range roleRules {
		if glob.Any(r.patterns, p) {
			return r.role
		}
	}
	has := func(c string) bool {
		for _, x := range classes {
			if x == c {
				return true
			}
		}
		return false
	}
	switch {
	case has("test"):
		return Role{"test", "Verifies behavior; its organization, readability, and testing approach matter."}
	case has("code") && strings.Contains("/"+p, "/cmd/"):
		return Role{"entry point", "Starts the program and wires its components together."}
	case has("code"):
		return Role{"source", "Implements part of the program's behavior."}
	case has("docs"):
		return Role{"documentation", "Explains the project to its readers."}
	case has("config"):
		return Role{"configuration", "Configures tools, builds, or runtime behavior."}
	case has("data"):
		return Role{"data", "Holds data used by the project."}
	}
	return Role{"other", "Supporting file."}
}
