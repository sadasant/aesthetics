# Aesthetics documentation

Aesthetics is a command-line tool that evaluates code, documentation, and changes against qualitative guidelines you choose. This documentation is organized by what you need right now, following [Diátaxis](https://diataxis.fr/).

## Learn

- [Tutorial: your first evaluation](tutorial.md). Install Aesthetics, evaluate a small project, write a guideline, and check a change, in about ten minutes.

## Do

- [Write your own guidelines](how-to/write-guidelines.md). Phrase, scope, and reuse your standards, and resolve conflicts between schools.
- [Check changes in CI](how-to/check-changes-in-ci.md). Judge every pull request, and fail only on problems it introduces.
- [Configure a provider](how-to/configure-a-provider.md). OpenAI, TypeSafe, Cloudflare, a self-hosted Laya, or any compatible endpoint.

## Look up

- [CLI](reference/cli.md). Commands, options, and environment variables.
- [Configuration](reference/configuration.md). Every key of `.aesthetics.yaml`.
- [Presets](reference/presets.md). The six built-in schools of thought and their guidelines.
- [Providers](reference/providers.md). Endpoints, credentials, defaults, and limits.
- [Results and exit codes](reference/results.md). The JSON report, exit codes, and records.

## Understand

- [How evaluation works](explanation/how-evaluation-works.md). Decision models, what they see, and how probabilities become outcomes.
- [How guidelines compose](explanation/composition.md). Layers, scope, ranking, and conflicts between schools of thought.
- [Evals](../evals/README.md). How the defaults were chosen, and how well each provider judges.
