# Results and exit codes reference

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | No result counts against the policy. |
| 1 | At least one result counts against the policy: a failure at or above `fail_on`, unless it yielded to a higher-ranked guideline or, by default, predates the change. With `fail_on_indeterminate`, indeterminate results count too. |
| 2 | Usage or configuration error. Nothing was evaluated. |
| 3 | Nothing counts against the policy, but some evaluations could not be completed. Disable with `fail_on_incomplete: false`. |

## JSON report

`aesthetics check --format json` writes a single object:

```json
{
  "schema": "aesthetics.report/v1",
  "tool": { "name": "aesthetics", "version": "0.1.0" },
  "run": { … },
  "artifacts": [ … ],
  "summary": { … }
}
```

### `run`

The inputs of the evaluation, for reproducibility. It never contains credentials.

| Field | Description |
| --- | --- |
| `started_at`, `finished_at` | UTC timestamps. |
| `root`, `config` | Repository root and configuration file. |
| `presets` | Presets active anywhere in the configuration. |
| `provider` | `type`, `model`, `endpoint`, `max_state_chars`, `max_questions`. |
| `diff` | With `--diff`: `label`, `files`, `additions`, `deletions`. |
| `thresholds` | `pass` and `fail`. |
| `policy` | `fail_on`, `fail_on_indeterminate`, `fail_on_incomplete`, `fail_on_preexisting`. |

### `artifacts[]`

| Field | Description |
| --- | --- |
| `kind` | `file`, `directory`, `repository`, or `diff`. |
| `path`, `label` | Path relative to the root, and a display label. |
| `status` | `complete`; `partial` (some results or details are missing); `error` (could not be evaluated); `skipped` (binary or too large). |
| `error`, `notes` | What went wrong or deserves attention, such as truncated context. |
| `facts` | Mechanically established facts: size, lines, role, language, content classes, diff statistics, context size. |
| `context` | Rule instructions sent with every judgment. |
| `results` | One entry per applicable guideline, in precedence order. |

### `artifacts[].results[]`

| Field | Kind | Description |
| --- | --- | --- |
| `guideline`, `origin`, `text` | | The guideline and the set chain it came from, such as `team > unix`. |
| `severity`, `priority`, `rank` | | Its weight and precedence for this artifact. |
| `outcome` | | `pass`, `fail`, or `indeterminate`. |
| `probability` | judgment | The model's estimate that the guideline is met. Absent on refusals and errors. |
| `explanation` | | How Aesthetics derived the outcome. |
| `recommendation` | | Present on every failure. |
| `recommendation_basis` | | `model` when the model chose among the guideline's remedies, or `guideline`. |
| `evidence[]` | judgment | `path`, `start_line`, `end_line`, `probability`, and, in diffs, `in_change`, a mechanical fact: whether the lines overlap the change. |
| `attribution` | | In diffs: `value` (`introduced` or `preexisting`) and `basis`: `model` (judged), `new file` (mechanical: the file is new), or `scope` (diff guidelines judge only the change). |
| `yielded_to` | judgment | The dominant guideline this failure yields to. Such failures do not count. |
| `counted` | | Whether the result counts against the policy. |
| `error` | | Why no outcome could be established. |

### `summary`

Counts of artifacts by status (`complete`, `partial`, `errors`, `skipped`), of results by outcome (`pass`, `fail`, `indeterminate`), of failures in diffs (`introduced`, `preexisting`), of `yielded` failures, and of `counted` results. It also holds `incomplete`, `status` (`passed`, `failed`, or `incomplete`), and `exit_code`.

## Reproducibility record

`--record FILE` writes the run, the resolved guidelines for every artifact, every request with its exact state and questions, every answer, and the summary. Outcomes may still differ between runs if the provider is not deterministic. With OpenAI's `gpt-6-luna`, the [evals](../../evals/README.md) found repeated runs identical.
