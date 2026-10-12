# How guidelines compose

Real taste is layered. You might hold a general standard everywhere, a stricter one for public APIs, a gentler one for legacy code, and a different school entirely for documentation. This page explains how Aesthetics combines those layers into the set of guidelines for one artifact, and what happens when they disagree.

## Layers

The top level of the configuration is the first layer. Each rule is another layer, applied in the order it is written, but only for artifacts inside its scope.

```text
base layer           extends: [unix, kiss]       guidelines: [Names reveal intent.]
rules[0] **/*.ts     guidelines: [Keep public interfaces minimal.]
rules[1] src/legacy  override: kiss → severity info · disable: unix/composable
```

For `src/legacy/old.ts`, all three layers apply. For `src/app.go`, only the base does.

Within a layer, Aesthetics does three things in order:

1. **Adds** the guidelines of the sets the layer extends, including what those sets inherit, and the layer's own guidelines.
2. **Removes** what the layer disables. A disable can name a guideline or a whole set.
3. **Specializes** what remains, as the layer's overrides say: severity, priority, text, instructions, or remedies.

Inline guidelines with an existing ID redefine that guideline. Extending a set again only moves its guidelines to the later layer. A guideline disabled by one rule and extended again by a later rule is active.

## Scope filters last

After the layers, every guideline is kept only if its own scope covers the artifact. `atomic` guidelines only judge diffs, `readme` guidelines only judge README files and the repository, and `kiss/readable-tests` only judges tests. Activating a preset therefore never evaluates unrelated artifacts, and `explain` lists what was filtered out.

Path filters never cover the repository as a whole. A diff is covered when any changed path is.

## Ranking

The guidelines that remain are ranked:

1. **Priority**, highest first.
2. **Severity**: error, then warning, then info.
3. **Layer**: later layers first, because they are more specific.
4. **Declaration order**: later first. In `extends: [readme, diataxis]`, diataxis outranks readme.

The rank orders the report, and it decides conflicts.

## When schools of thought disagree

Aesthetics doesn't assume schools of thought agree. `kiss` warns against abstractions built for needs that don't exist yet. `solid` asks for extension points where variation is expected. Both can be right about different code, and they can collide on the same code.

Guidelines declare such tensions with `conflicts_with`. The presets declare the obvious ones:

- `kiss/no-speculative-generality` against `solid/open-closed` and `solid/depend-on-abstractions`;
- `diataxis/one-mode` against `readme/usage` and `readme/orientation`, because a README necessarily mixes modes.

When both guidelines in a tension apply to an artifact, the higher-ranked one is dominant. If the other one fails, Aesthetics asks whether meeting it here would compromise the dominant guideline. If the model thinks so, the failure is reported as *yielding*. It stays visible but doesn't count against CI.

Nothing is suppressed in advance: a tension only matters when the guidelines actually collide on a specific artifact. To decide which school governs, raise its priority or declare it later.

## Inspecting the result

```sh
aesthetics explain src/legacy/old.ts
```

For any artifact, `explain` prints:

- the layers that applied;
- each guideline in rank order, with the set chain it came from (such as `team > kiss`) and the layers that introduced or specialized it;
- what was disabled, and by which layer;
- the tensions, and which side governs;
- what was active but out of scope.

Nothing about composition is hidden from you.
