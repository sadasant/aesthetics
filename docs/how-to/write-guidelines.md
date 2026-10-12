# How to write your own guidelines

This guide shows how to express your own standards as guidelines, scope them, and reuse them. For every field, see the [configuration reference](../reference/configuration.md).

## Write a guideline

A guideline is one sentence describing a quality the artifact should have. Add it under `guidelines:` in `.aesthetics.yaml`:

```yaml
guidelines:
  - Error messages say what failed and what the user can do about it.
```

A plain string becomes a warning that applies to files, with an identifier derived from its text (`config/error-messages-say-what-failed-and-what-the-user`).

Guidelines are judged by a decision model, one yes-or-no question each. To keep the answers reliable:

- **State one observable property.** "Functions are short and well named and documented" is three guidelines.
- **Describe the desired state, not the defect.** Write "Names reveal intent", not "Avoid bad names".
- **Avoid lists of alternatives.** A list reads as "all of these". Name the property and give examples.

## Add an identifier, severity, and recommendation

```yaml
guidelines:
  - id: actionable-errors
    text: Error messages say what failed and what the user can do about it.
    severity: error          # info, warning (default), or error
    priority: 10             # wins conflicts against lower priorities
    recommendation: Name the failing input and the next step in every error message.
    remedies:                # the model picks the best fit for each failure
      - Include the failing path or value in the message.
      - Suggest the command or setting that fixes the problem.
```

Every failure gets a recommendation: the remedy the model chose, the `recommendation`, or a generic fallback. Writing remedies makes the advice specific.

## Scope a guideline

By default a guideline judges files. Narrow it or widen it with scope fields:

```yaml
guidelines:
  - id: thin-handlers
    text: HTTP handlers only decode requests, call the domain layer, and encode responses.
    match: ["internal/http/**"]
    extensions: [.go]
    exclude: ["**/*_test.go"]

  - id: package-purpose
    text: Each package's name states one purpose.
    applies_to: [directory]
    content: [code]
    min_depth: 1

  - id: coherent-change
    text: The change keeps naming consistent with the code around it.
    applies_to: [diff]
```

`applies_to` accepts `file`, `directory`, `repository`, and `diff`. `content` selects by what files contain: `code`, `test`, `docs`, `config`, `data`, or `other`.

## Apply guidelines to some paths only

Use `rules` when a group of guidelines, or a whole preset, belongs to part of the repository:

```yaml
rules:
  - match: "docs/**"
    extends: [diataxis]
  - match: "src/legacy/**"
    instructions: This code predates our current standards; judge only egregious problems.
    severity: info
    extends: [kiss]
```

Rules apply in order, and later rules win. `instructions` adds context to every judgment on matching artifacts.

## Adjust a preset without rewriting it

Specialize inherited guidelines with `override`, or remove them with `disable`. Both accept a guideline or a whole set:

```yaml
rules:
  - match: "cmd/**"
    override:
      - id: unix/thin-entry-point
        severity: error
      - id: solid
        priority: -1
    disable: [kiss/readable-tests]
```

## Reuse a set of guidelines

Define a named set once and extend it anywhere, including from other sets:

```yaml
sets:
  house-style:
    description: How we write services.
    extends: [unix, solid]
    guidelines:
      - Handlers stay thin and delegate to the domain layer.

extends: [house-style]
```

## Resolve conflicts between schools

Some guidelines pull in opposite directions; `kiss` and `solid` disagree about abstraction, for example. Declare the tension and decide who wins:

```yaml
guidelines:
  - id: no-interfaces-for-one-type
    text: Interfaces exist only where there are several implementations.
    conflicts_with: [solid/depend-on-abstractions]
    priority: 5
```

When both apply, the higher-ranked guideline governs. If the lower-ranked one fails only because meeting it would compromise the dominant one, it is reported as yielding and does not count.

## Check what you wrote

```sh
aesthetics rules                       # the layers you configured
aesthetics explain src/http/users.go   # what applies to one file, and why
aesthetics check --dry-run             # what would be evaluated, without calling the model
```
