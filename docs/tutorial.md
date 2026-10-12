# Tutorial: your first evaluation

In this tutorial you will install Aesthetics, evaluate a small project against two schools of thought, add a guideline of your own, and check a change the way CI would. It takes about ten minutes.

You need a terminal, Git, and an OpenAI API key. Any other [supported provider](reference/providers.md) works too; only step 2 changes.

## 1. Install

```sh
curl -fsSL https://raw.githubusercontent.com/sadasant/aesthetics/main/install.sh | sh
aesthetics version
```

You should see `aesthetics 0.1.0` or later.

## 2. Make a project to evaluate

Create a small Go project with one tidy file and one untidy entry point:

```sh
mkdir report && cd report && git init -q
mkdir -p internal/slug cmd/report
```

Save this as `internal/slug/slug.go`:

```go
// Package slug converts titles into URL-safe identifiers.
package slug

import (
	"strings"
	"unicode"
)

// Make lowercases s and joins its words with hyphens.
func Make(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.ToLower(strings.Join(words, "-"))
}
```

Save this as `cmd/report/main.go`:

```go
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	rows, _ := csv.NewReader(f).ReadAll()
	totals := map[string]float64{}
	for _, r := range rows[1:] {
		amount, _ := strconv.ParseFloat(r[2], 64)
		if r[1] == "EUR" {
			amount *= 1.08
		}
		totals[r[0]] += amount
	}
	for name, total := range totals {
		fmt.Printf("%-20s %10.2f\n", name, total)
	}
}
```

Then give Aesthetics a provider:

```sh
export OPENAI_API_KEY=sk-…
aesthetics init --provider openai --use unix,kiss
```

`init` writes `.aesthetics.yaml`. Open it: it activates the `unix` and `kiss` presets, adds an example guideline and two example rules, and sets the failure policy for CI.

## 3. Run your first check

```sh
aesthetics check
```

Aesthetics evaluates every file and directory, and the repository as a whole. The tidy `slug.go` passes everything. The entry point fails `unix/thin-entry-point`, because it computes totals instead of delegating. You will also see `kiss` and `unix` findings, each with a recommendation and the lines involved:

```text
cmd/report/main.go  entry point
  ✗ unix/thin-entry-point           warning  p=0.00
    The entry point only starts the program: it reads input and configuration, wires components
    together, and delegates, while the real work lives in reusable modules.
    → Reduce the entry point to argument parsing, dependency construction, and a single call into
      the program.
      at cmd/report/main.go:11-20
```

`p` is the probability that the guideline is met. Low values fail, high values pass, and values in between are reported as indeterminate. The command exits with status 1 because there are failures.

## 4. Add a guideline of your own

Open `.aesthetics.yaml` and add a guideline in plain language under `guidelines:`:

```yaml
guidelines:
  - id: actionable-errors
    text: Error messages say what failed and what the user can do about it.
    content: [code]
```

Run only the entry point this time:

```sh
aesthetics check cmd/report/main.go
```

`config/actionable-errors` now fails too: `panic(err)` and the ignored parse errors tell the user nothing useful. You wrote that guideline, and Aesthetics judged it like any other.

## 5. Ask why

```sh
aesthetics explain cmd/report/main.go
```

`explain` lists every guideline that applies to the file, in precedence order, and shows where each came from. It also lists what was activated but doesn't apply here: `kiss/readable-tests`, for example, only judges test files.

## 6. Check a change

Commit the project, then make a change:

```sh
git add -A && git commit -qm "first version"
```

Add this function to the end of `internal/slug/slug.go`:

```go
var Calls int

// MakeAll slugs every title and counts calls.
func MakeAll(titles []string) []string {
	var out []string
	for _, t := range titles {
		Calls++
		out = append(out, Make(t))
	}
	return out
}
```

Now evaluate only what changed:

```sh
aesthetics check --diff --use atomic
```

The global `Calls` counter fails `unix/simple-interfaces`, and the result says *introduced by the change*. `main.go` is not reported, because it didn't change. Below the files, `(diff working tree)` shows the `atomic` guidelines judging the change as a whole.

## Where to go next

You have evaluated files, directories, a repository, and a diff, with presets and with a guideline of your own. Next:

- [Write your own guidelines](how-to/write-guidelines.md): scope them to paths and file types, and build reusable sets.
- [Check changes in CI](how-to/check-changes-in-ci.md): run Aesthetics on every pull request.
- [How evaluation works](explanation/how-evaluation-works.md): what the model sees and how probabilities become outcomes.
