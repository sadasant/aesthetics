# Providers reference

Aesthetics judges guidelines with *decision models*: models that answer typed questions with calibrated probabilities instead of generating text. Select one with `provider.type`, `--provider`, or `AESTHETICS_PROVIDER`.

| Type | Service | Endpoint | Credentials | Default model | Context per request | Questions per request |
| --- | --- | --- | --- | --- | --- | --- |
| `openai` | OpenAI Decisions API | `https://api.openai.com/v1/decisions` | `OPENAI_API_KEY` | `gpt-6-luna` | 400,000 chars | 16 |
| `typesafe` | TypeSafe Jev | `https://api.typesafe.ai/v1/systemone` | `TYPESAFE_API_KEY` | `jev-latest` | 90,000 chars | 16 |
| `cloudflare` | Cloudflare Workers AI: Clef, Clef-flash | `https://api.cloudflare.com/client/v4/accounts/{account}/ai/run/@cf/cloudflare/{model}` | `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` | `clef` | 8,000 chars | 16 |
| `laya` | Laya via `laya-serve`, self-hosted | `http://localhost:8000/v1/systemone` | `LAYA_API_KEY` if the server requires it | `laya` | 24,000 chars | 8 |
| `systemone` | Any System One-compatible API, such as OpenRouter | `{base_url}/systemone` | `api_key_env`, optional | required | 60,000 chars | 8 |
| `chat` | Any OpenAI-compatible chat completions API, asked to answer as a decision model | `{base_url}/chat/completions` | `OPENAI_API_KEY` or `api_key_env`, optional | required | 100,000 chars | 16 |
| `command` | Any program: System One request JSON on stdin, response JSON on stdout | `command` | the program's own | optional | 100,000 chars | 16 |

Every value can be overridden: see [Provider](configuration.md#provider). Endpoints and default models were checked against each provider's documentation in October 2026.

## Provider details

| Type | Details |
| --- | --- |
| `openai` | Yes/no questions are sent as predicates. `gpt-6-luna` was the only model offered when checked. |
| `typesafe` | Jev reads up to 32,000 tokens. Requests use the System One protocol, which Clef, Laya, and OpenRouter share. |
| `cloudflare` | Responses arrive in Cloudflare's `{"result": …}` envelope, which is unwrapped. Clef reads about the first 2,000 tokens of text. |
| `laya` | Open-source, with an 8,192-token context. Installed with `pip install "laya[serve]"` and started with `laya-serve`. |
| `chat` | Works with OpenAI-compatible servers such as Ollama, vLLM, and LM Studio. Probabilities are the model's self-reports. |
| `command` | The program runs directly, without a shell. |

## Behavior common to all providers

- Requests that hit rate limits, overloads, or transient server errors are tried up to four times with exponential backoff, honoring `Retry-After`.
- Credentials never appear in reports or `--record` files.
- A failed request makes the affected results indeterminate. Other artifacts are unaffected, and the run exits with 3 unless `fail_on_incomplete` is false.
- Judgment quality per provider is measured by the [evals](../../evals/README.md).
