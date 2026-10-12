# How to configure a provider

Aesthetics asks a decision model for its judgments. This guide sets up each supported provider. Credentials always come from environment variables, never from the configuration file. For every option, see the [providers reference](../reference/providers.md).

## OpenAI

```sh
export OPENAI_API_KEY=sk-…
```

```yaml
provider:
  type: openai        # uses gpt-6-luna on the Decisions API
```

## TypeSafe Jev

```sh
export TYPESAFE_API_KEY=…
```

```yaml
provider:
  type: typesafe      # uses jev-latest
```

## Cloudflare Clef

Workers AI runs models per account, so you need your account ID as well as an API token with Workers AI access. The account ID is the 32-character hex string in your dashboard URL (`dash.cloudflare.com/<account-id>`), and `npx wrangler whoami` prints it too.

```sh
export CLOUDFLARE_API_TOKEN=…
export CLOUDFLARE_ACCOUNT_ID=…
```

```yaml
provider:
  type: cloudflare
  model: clef         # or clef-flash: faster, but less accurate
```

Clef reads only about the first 2,000 tokens of its input, so Aesthetics keeps each request under 8,000 characters and reports when it truncated an artifact. Prefer `clef` for CI: in the [evals](../../evals/README.md), `clef` judged as well as OpenAI, while `clef-flash` failed some good artifacts.

## Laya, self-hosted

Run the open-source model locally, with no API key:

```sh
pip install "laya[serve]"
laya-serve            # listens on http://localhost:8000
```

```yaml
provider:
  type: laya
  # base_url: http://gpu-box:8000/v1
```

If the server sets `LAYA_API_KEY`, export the same variable for Aesthetics.

## Any other endpoint

- **A System One-compatible API**, such as OpenRouter:

  ```yaml
  provider:
    type: systemone
    base_url: https://openrouter.ai/api/v1
    model: typesafe/jev-1.13
    api_key_env: OPENROUTER_API_KEY
  ```

- **An OpenAI-compatible chat model**, such as Ollama. It is asked to answer like a decision model:

  ```yaml
  provider:
    type: chat
    base_url: http://localhost:11434/v1
    model: llama3.3
  ```

- **Your own program**, which reads a System One request on stdin and writes the response on stdout:

  ```yaml
  provider:
    type: command
    command: [python3, tools/my-provider.py]
  ```

## Switch for one run

```sh
aesthetics check --provider typesafe
AESTHETICS_PROVIDER=laya aesthetics check
```

Flags override environment variables, which override the configuration file.

## Compare providers

To see how well a provider judges before you rely on it, run the evals with it:

```sh
go run ./evals -provider cloudflare -model clef-flash
```
