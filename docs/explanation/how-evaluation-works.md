# How evaluation works

Aesthetics turns qualitative standards into decisions a program can act on. This page explains what it sends to a model, what it asks, and how answers become outcomes.

## Why decision models

A guideline like "the entry point only wires components together" can't be checked with a parser, but it isn't an essay question either. It has an answer, and the useful output is that answer plus how sure the model is.

Decision models are built for exactly that: given a *state* and typed *questions*, they return probabilities rather than prose. This shapes the design:

- **Judgments are numbers.** Each guideline gets a probability that it is met. Aesthetics decides what that probability means, not the model.
- **Questions are independent.** Each guideline is evaluated in isolation, so adding guidelines doesn't blur the others.
- **Advice is authored.** The model doesn't write prose, so recommendations come from the guideline itself. When a guideline offers several remedies, the model chooses the one that fits.

A chat model can stand in through the `chat` provider. It is asked to answer in the same form.

## What the model sees

Aesthetics builds a state for each artifact. Its contents depend on the artifact's kind, because purpose matters more than file extension:

| Artifact | State |
| --- | --- |
| File | The content with line numbers, its role (README, package manifest, module index, entry point, crate root, test, …), language, sibling names, and related files such as the code under test. |
| Directory | The tree, its key files (README, manifest, index), excerpts of the files in it, and the opening of one file per subdirectory. |
| Repository | The tree, counts of files by language and content class, the root's key files, entry points, and component READMEs. |
| Diff | The changed files with their statistics, and the unified diff with surrounding context. |
| File in a diff | The whole file after the change, plus its diff. |

Each provider has a context budget. When an artifact doesn't fit, the state is truncated and the result says so, so a judgment never silently rests on partial evidence. `aesthetics explain --state` prints the exact state.

## Two rounds of questions

**Round one: is each guideline met?** Every applicable guideline becomes one yes-or-no question:

> Consider the file cmd/report/main.go shown in `content`, keeping its role (`artifact.role`) in mind.
> Guideline: The entry point only starts the program: it reads input and configuration, wires components together, and delegates, while the real work lives in reusable modules.
> Does the artifact meet this guideline?

The answer is the probability *p* that it does:

```text
0            0.35                    0.65            1
├──── fail ────┤──── indeterminate ────┤──── pass ────┤
```

Indeterminate is a real outcome. It means the evidence doesn't settle the question, and it doesn't fail CI unless you ask it to. Refusals and failed requests are indeterminate too, with the reason recorded.

**Round two: what about the failures?** For each failure, Aesthetics asks follow-up choice questions:

- **Which remedy fits best?** The guideline's remedies are the options. The model's choice becomes the recommendation.
- **Where does it show?** The options are line ranges, directory entries, or changed files. The answer becomes evidence when the model is reasonably sure.
- **Did the change cause it?** For files in a diff, the options are *introduced* or *preexisting*.
- **Does it yield?** If the guideline is in tension with a higher-ranked one, the model is asked whether meeting it would compromise the other.

Asking these only about failures keeps requests small, and passing results need no advice.

## Judgment and fact, kept apart

Some things Aesthetics knows; others the model believes. The results keep them separate:

| Mechanical facts | Qualitative judgments |
| --- | --- |
| Sizes, line counts, roles, languages, content classes | The probability that a guideline is met |
| Diff statistics, and whether cited lines overlap the change | Which remedy fits, and where a problem shows |
| A new file's problems are introduced by definition | Whether a change introduced a problem |
| Whether context was truncated | Whether a failure yields to a dominant guideline |

When a fact settles a question, Aesthetics doesn't ask: everything in a new file is introduced, whatever a model might say.

## Choosing the defaults

The thresholds, the wording of the presets, and the context sent to models were chosen with [evals](../../evals/README.md): 104 labeled judgments run through the full pipeline. With OpenAI's `gpt-6-luna`, clear cases come out 97% correct, with no false failures, and repeated runs are identical. The evals also exposed problems that are now fixed: a directory tree that hid deep nesting, and guidelines worded as lists that models read as "all of these".

Agreement with labels is a guide, not a guarantee. Aesthetics makes judgments explicit and inspectable; it doesn't make them objectively right.
