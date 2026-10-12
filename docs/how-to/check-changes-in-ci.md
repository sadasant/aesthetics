# How to check changes in CI

This guide runs Aesthetics on every pull request, so that only the changes are judged, and only problems they introduce fail the build.

## Add a workflow

Store your provider's API key as a repository secret (here `OPENAI_API_KEY`), then add `.github/workflows/aesthetics.yml`:

```yaml
name: Aesthetics

on: pull_request

jobs:
  aesthetics:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0   # the base branch is needed to compute the diff
      - run: curl -fsSL https://raw.githubusercontent.com/sadasant/aesthetics/main/install.sh | sh
      - run: aesthetics check --diff --base origin/${{ github.base_ref }} --color always
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

`--base` compares `HEAD` with the point where it diverged from the base branch, as a pull request does.

## Choose what fails the build

Set the policy in `.aesthetics.yaml`:

```yaml
ci:
  fail_on: error                # only error-severity failures fail the build
  fail_on_indeterminate: false  # undecided judgments never fail the build
  fail_on_preexisting: false    # problems that predate the change never fail the build
  fail_on_incomplete: true      # an evaluation that could not finish fails the build (exit 3)
```

Override any of them for one run with `--fail-on`, `--fail-on-indeterminate`, `--fail-on-preexisting`, or `--fail-on-incomplete`.

## Keep the report

Write the JSON report as an artifact for later inspection:

```yaml
      - run: aesthetics check --diff --base origin/${{ github.base_ref }} --format json --output aesthetics.json
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: aesthetics
          path: aesthetics.json
```

The report's format is described in [Results and exit codes](../reference/results.md). Add `--record record.json` to also keep every request and answer.

## Other CI systems

Aesthetics needs only the binary, Git history for `--base`, and the provider's environment variable. The exit code is all your CI system needs:

| Exit code | Meaning |
| --- | --- |
| 0 | Passed |
| 1 | Failures at or above the threshold |
| 2 | Usage or configuration error |
| 3 | Some evaluations could not be completed |
