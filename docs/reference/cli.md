# CLI reference

```text
aesthetics check   [paths...] [options]
aesthetics explain <path>     [options]
aesthetics rules   [set...]   [options]
aesthetics init               [options]
aesthetics version
```

Options may appear before or after paths and accept either `-name` or `--name`. Every command is non-interactive.

## `check`

Evaluates the selected artifacts and reports pass, fail, or indeterminate for every applicable guideline.

| Paths | Artifacts evaluated |
| --- | --- |
| none, or `.` | every file, every directory, and the repository |
| a file | that file, even if it is excluded by `files.exclude` |
| a directory | the directory, its subdirectories, and their files |

With `--diff`, Aesthetics evaluates Git changes instead: each changed file in the context of its change, plus the change set as a whole. Paths restrict which changes are included.

| Option | Description |
| --- | --- |
| `--diff` | Evaluate staged, unstaged, and untracked changes against `HEAD`. |
| `--staged` | With `--diff`, only staged changes. Implies `--diff`. |
| `--base REF` | With `--diff`, the changes on `HEAD` since it diverged from `REF`, as in a pull request. Implies `--diff`. |
| `--scope KINDS` | Only these artifact kinds: `file`, `directory`, `repository`, `diff`. |
| `--use SETS` | Activate presets or user-defined sets, comma-separated, in addition to the configuration. |
| `--provider TYPE` | Provider type; see [providers](providers.md). |
| `--model NAME` | Model for the provider. |
| `--config FILE` | Configuration file. By default, the nearest `.aesthetics.yaml` upward from the working directory. |
| `--fail-on SEVERITY` | Lowest severity whose failures fail the run: `error`, `warning`, `info`, or `never`. |
| `--fail-on-indeterminate` | Indeterminate results at or above the threshold also fail the run. |
| `--fail-on-preexisting` | With `--diff`, problems that predate the change also fail the run. |
| `--fail-on-incomplete=false` | Do not exit with 3 when some evaluations could not be completed. |
| `--concurrency N` | Artifacts evaluated in parallel. Default 4. |
| `--dry-run` | Show what would be evaluated without contacting the provider. |
| `--record FILE` | Write every request, answer, and resolved guideline to a JSON file. |
| `--format text\|json` | Output format. JSON is described in [results](results.md). |
| `--output FILE` | Write the output to a file instead of standard output. |
| `--color auto\|always\|never` | Colors for text output. `auto` colors terminals only and honors `NO_COLOR`. |
| `-v`, `--verbose` | Also list passing results, mechanical facts, and model details. |

## `explain`

Shows how an artifact would be evaluated, without evaluating it:

- its kind, role, content classes, and depth;
- the provider and its limits;
- the composition layers that apply;
- every applicable guideline, in precedence order, with its origin, scope, overrides, and recommendation;
- tensions, disabled guidelines, and guidelines that are active but out of scope;
- the size of the context that would be sent.

Use `.` for the repository, or `--diff` for the change set.

| Option | Description |
| --- | --- |
| `--diff [path]` | Explain the change set, or a changed file in the context of its change. |
| `--staged`, `--base REF` | As for `check`. |
| `--state` | Also print the exact state and questions that would be sent. |
| `--evaluate` | Also evaluate the artifact and print detailed results. |
| `--use`, `--provider`, `--model`, `--config`, `--format`, `--output`, `--color` | As for `check`. |

## `rules`

| Invocation | Shows |
| --- | --- |
| `aesthetics rules` | The configured composition layers: top level and rules, with what each extends, defines, specializes, and disables. |
| `aesthetics rules --catalog` | Every available set: the presets and your own. |
| `aesthetics rules unix kiss` | The guidelines of the named sets, with scopes, remedies, and tensions. |

Accepts `--use`, `--config`, `--format`, and `--output`.

## `init`

Writes a commented `.aesthetics.yaml` at the repository root.

| Option | Description |
| --- | --- |
| `--provider TYPE` | Provider to configure. Default `openai`. |
| `--model NAME` | Model. Default: the provider's default. |
| `--use SETS` | Presets to activate. Default `unix,kiss,atomic`. |
| `--force` | Overwrite an existing file. |

## Environment

| Variable | Effect |
| --- | --- |
| `AESTHETICS_PROVIDER`, `AESTHETICS_MODEL` | Provider and model, overriding the configuration file. Flags override these. |
| Provider credentials | See [providers](providers.md). |
| `NO_COLOR` | Disable colors with `--color auto`. |
| `COLUMNS` | Width for wrapping text output, between 60 and 120. Default 100. |
