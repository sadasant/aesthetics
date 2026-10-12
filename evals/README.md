# Aesthetics evals

Labeled cases, and a harness that measures how often Aesthetics reaches the expected judgment with a given decision model. Contributors use them to choose defaults (outcome thresholds, the wording of preset guidelines, and the context sent to models) and to show that a change doesn't make judgments worse.

The spec makes agreement with human judgment a guide, not a release requirement. The evals therefore run on demand and are not part of `go test ./...`: they cost money and depend on the provider.

## Run

```sh
go run ./evals -provider openai                         # all cases
go run ./evals -provider openai -run kiss               # cases whose id contains "kiss"
go run ./evals -provider cloudflare -model clef-flash   # another provider
go run ./evals -pass 0.6 -fail 0.3 -json out.json       # other thresholds, raw judgments
```

Responses are cached in `evals/.cache`, which is ignored by Git, so reruns are free. `-no-cache` asks the provider again.

## Cases

`cases/*.yaml` hold 65 cases with 104 labeled judgments. Each case is a small workspace with an artifact (a file, directory, repository, or diff), the guidelines to apply, and the expected outcome for each guideline:

- **pass or fail:** a clear case, where the artifact plainly meets or plainly violates the guideline;
- **borderline:** reasonable reviewers could disagree, so the result is reported but not scored.

Diff cases can also expect an attribution (introduced or preexisting), and failing cases can name the lines of the flaw to check evidence.

The cases cover:

- the guidelines of all six presets;
- custom natural-language guidelines;
- introduced and preexisting problems in diffs;
- Go, Python, TypeScript, JavaScript, Java, JSON, and YAML artifacts.

The harness runs the real pipeline (workspace, guideline resolution, state building, questions, follow-ups) and scores only the labeled guidelines.

Caveat: the cases and their labels were written by the same author as the presets. They are deliberately clear-cut, so they test whether the pipeline works, not whether its taste matches yours. Add cases from your own code when you add or change guidelines.

## Metrics

- **Correct:** the share of clear judgments whose outcome matches the label.
- **False fail:** something good was failed. This is the costliest error in CI.
- **Missed fail:** something bad was passed.
- **Indeterminate:** on clear cases, the evaluator could not decide.
- **AUC:** the probability that a pass-labeled judgment gets a higher probability than a fail-labeled one. It is independent of thresholds, so it measures prompt and context quality.
- **Threshold sweep:** scores each threshold pair by correct − 3·false fail − 1.5·missed fail − 0.5·indeterminate.

## Results

OpenAI Decisions API, `gpt-6-luna`, October 2026:

| | Baseline | Current |
| --- | --- | --- |
| Clear judgments | 99 | 100 |
| Correct | 94% | 97% |
| False fail | 2% (2) | 0% |
| Missed fail | 2% (2) | 1% (1) |
| Indeterminate | 2% | 2% |
| AUC | 0.99 | 1.00 |
| Attribution correct | 5 of 5 | 5 of 5 |
| Evidence on the flaw | 3 of 4 | 3 of 4 |

What the evals changed:

1. **Directory trees hid deep nesting.** Trees were cut at three levels, so `src/main/java/com/acme/...` looked shallow and `kiss/shallow-structure` passed it. Trees now show as many levels as fit.
2. **Guidelines that list conditions or examples read as requirements.** `unix/manifest-interface` failed a clean `package.json` (p=0.24). `diataxis/mode-conventions` listed the conventions of each mode, so a correct reference page failed (p=0.28) because it wasn't a tutorial. Both now state one property. The diataxis guideline moved its context into `instructions`, which made it 100% correct on its cases.
3. **Container directories failed `unix/focused-directories`.** A directory such as `internal/` that holds only subpackages failed (p=0.09). The guideline now covers container directories, and directory context includes the opening of one file per subdirectory.
4. **Running Aesthetics on its own documentation** found the last two problems: the `diataxis` wording in item 2, and `readme/orientation` asking nested READMEs for license and contributing sections. Orientation now applies to the root README only.

**Thresholds are kept at pass ≥ 0.65 and fail ≤ 0.35.** They sit on the sweep's plateau: every pass threshold from 0.50 to 0.65 with a fail threshold of 0.30 or 0.35 scores the same. The model is decisive on clear cases (mean p 0.93 for pass-labeled, 0.04 for fail-labeled). Real code lands between 0.35 and 0.65 far more often (36 of 244 judgments on this repository), so a lower pass threshold would turn genuine uncertainty into passes.

**Determinism.** Two uncached runs gave identical probabilities for all 104 judgments.

**Cost.** A full uncached run uses about 78,000 input tokens, under one US cent at $0.10 per million.

Remaining misjudgments:

- `atomic/no-unrelated-modifications` passes a change with several purposes (p=0.85). `atomic/single-purpose` does catch it.
- `atomic/reviewable` is indeterminate on a 50-line change that bundles three features (p=0.46).
- `kiss/simple-configuration` is indeterminate on a minimal `tsconfig.json` (p=0.39).

None of these is a false fail.

Cloudflare Clef and Clef-flash: not yet measured; see the provider table in the main README for configuration.
