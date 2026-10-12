# Configuration reference

Aesthetics reads the first of `.aesthetics.yaml`, `.aesthetics.yml`, `aesthetics.yaml`, or `aesthetics.yml` that it finds in the working directory or its parents, stopping at the repository root. `--config` names a file explicitly.

Paths in the configuration are relative to the directory that contains it. Unknown keys are errors, so typos fail loudly.

## Top level

| Key | Type | Description |
| --- | --- | --- |
| `version` | `1` | Optional schema version. |
| `provider` | [provider](#provider) or string | The decision model. A string is shorthand for `type`. |
| `extends` | list of set IDs | Presets or user sets active everywhere. |
| `guidelines` | list of [guidelines](#guideline) | Guidelines active everywhere their own scope allows. |
| `sets` | map of [sets](#set) | Reusable named sets. |
| `rules` | list of [rules](#rule) | Scoped composition steps, applied in order. |
| `files` | [files](#files) | Which files belong to the evaluation. |
| `evaluation` | [evaluation](#evaluation) | How answers become outcomes. |
| `ci` | [ci](#ci) | Which results fail the run. |

## Guideline

A guideline is a string, or a map with these keys:

| Key | Default | Description |
| --- | --- | --- |
| `text` | required | The quality, in natural language. |
| `id` | derived from `text` | A stable identifier. Without a `/`, it is prefixed with `config/`. |
| `severity` | `warning` | `info`, `warning`, or `error`. |
| `priority` | `0` | Higher priorities win ranking and conflicts. |
| `applies_to` | `[file]` | Artifact kinds: `file`, `directory`, `repository`, `diff`. |
| `content` | any | Content classes: `code`, `test`, `docs`, `config`, `data`, `other`. A directory, repository, or diff matches if it contains such files. |
| `match` | any path | Glob patterns. |
| `exclude` | none | Glob patterns removed from `match`. |
| `extensions` | any | File extensions, such as `.ts`. |
| `min_depth`, `max_depth` | none | Directories above a path: `README.md` and `src/` have depth 0, and `src/main.go` has depth 1. |
| `instructions` | none | Extra context for the evaluator. |
| `recommendation` | generic | Advice shown when the guideline fails. |
| `remedies` | none | Specific fixes. For each failure, the model picks the best one. |
| `conflicts_with` | none | Guidelines or sets in tension with this one. |

Path filters (`match`, `extensions`, depths) never match the repository as a whole. A diff matches when any changed path matches.

### Glob patterns

| Pattern | Matches |
| --- | --- |
| `*.md` | Without a `/`, the file name at any depth: `README.md`, `docs/guide.md`. |
| `docs/*.md` | Markdown files directly in `docs/`. |
| `**/README.md` | `README.md` at any depth, including the root. |
| `src/**` | `src` and everything below it. |
| `index.{ts,js}` | Either alternative. |
| `?`, `[abc]` | One character; a character class. |

## Set

| Key | Description |
| --- | --- |
| `name`, `description` | For display. |
| `extends` | Other sets to inherit. |
| `guidelines` | The set's own guidelines. Their IDs are prefixed with the set's ID. |

Set IDs may not reuse a preset's ID.

## Rule

A rule has the scope keys of a guideline (`applies_to`, `content`, `match`, `exclude`, `extensions`, `min_depth`, `max_depth`), and applies its other keys to artifacts in that scope.

| Key | Description |
| --- | --- |
| `name` | For display in `rules` and `explain`. |
| `extends` | Sets to activate. |
| `guidelines` | Guidelines to add. An existing ID redefines that guideline. |
| `override` | Specializations: maps with `id` (a guideline or set) and any of `text`, `severity`, `priority`, `instructions`, `recommendation`, `remedies`. |
| `disable` | Guidelines or sets to remove. |
| `severity`, `priority` | Applied to the guidelines this rule introduces. |
| `instructions` | Context sent with every judgment on matching artifacts. |

How layers combine is explained in [How guidelines compose](../explanation/composition.md).

## Provider

| Key | Description |
| --- | --- |
| `type` | `openai`, `typesafe`, `cloudflare`, `laya`, `systemone`, `chat`, or `command`. |
| `model` | Model identifier. Defaults depend on the type. |
| `base_url` | Endpoint override. |
| `api_key_env` | Environment variable holding the API key. |
| `account_id`, `account_id_env` | Cloudflare account. |
| `command` | For `command`: the program and its arguments. |
| `headers` | Extra HTTP headers. |
| `timeout` | Per request, such as `90s`. Default `2m`. |
| `max_state_chars` | Characters of context per request. |
| `max_questions` | Questions per request. |

Defaults for each type are listed in the [providers reference](providers.md).

## Files

| Key | Default | Description |
| --- | --- | --- |
| `include` | all files | Glob patterns of files to consider. |
| `exclude` | none | Extra patterns to skip, added to the defaults. |
| `default_excludes` | `true` | Skip dependencies, build output, lockfiles, and minified files. |
| `max_file_bytes` | `262144` | Larger files are reported as skipped. |

In a Git repository, ignored files are skipped. The configuration file itself is never evaluated.

## Evaluation

| Key | Default | Description |
| --- | --- | --- |
| `pass_threshold` | `0.65` | Pass when the probability that the guideline is met is at least this. |
| `fail_threshold` | `0.35` | Fail when it is at most this. In between, the result is indeterminate. |
| `concurrency` | `4` | Artifacts evaluated in parallel. |
| `evidence` | `true` | Ask where each failure shows. |

## CI

| Key | Default | Description |
| --- | --- | --- |
| `fail_on` | `warning` | Lowest severity whose failures fail the run, or `never`. |
| `fail_on_indeterminate` | `false` | Indeterminate results at or above `fail_on` also fail the run. |
| `fail_on_incomplete` | `true` | Exit with 3 when some evaluations could not be completed. |
| `fail_on_preexisting` | `false` | With `--diff`, problems that predate the change also fail the run. |

## A complete example

```yaml
version: 1

provider:
  type: typesafe

extends: [team, kiss, atomic]

guidelines:
  - Names reveal intent without needing a comment.
  - id: errors-explain
    text: Error messages say what failed and what the user can do about it.
    severity: error
    priority: 10
    applies_to: [file, diff]
    content: [code]
    match: ["src/**"]
    remedies:
      - Include the failing input in the message.
      - Suggest the command or setting that fixes the problem.

sets:
  team:
    description: How we like our services.
    extends: [unix, solid]
    guidelines:
      - Handlers stay thin and delegate to the domain layer.

rules:
  - match: "**/README.md"
    extends: [readme, diataxis]
  - match: "src/**/utils/**"
    guidelines:
      - Avoid unnecessary abstraction.
    override:
      - id: kiss/direct
        severity: error
    disable: [unix/composable]
    instructions: These utilities are shared by every service; favor stability.
  - name: legacy
    match: "src/legacy/**"
    extends: [kiss]
    severity: info
    disable: [solid]

files:
  exclude: ["fixtures/**"]

ci:
  fail_on: warning
```
