# spec_models_015 — Model providers, resilience, fallback and per-model settings

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/runtime/model_factory.go`, `anthropic.go`, `openai_images.go`, `images.go`, `fallback.go`, `httpclient.go`, `settings.go`, `usage.go`; `internal/breaker` |
| Tests | `internal/runtime/anthropic_test.go`, `fallback_test.go`, `httpclient_test.go`, `settings_test.go`, `pin_test.go`, `images_test.go`; `internal/breaker/breaker_test.go` |
| Depends on | [spec_config_002](spec_config_002.md) |
| Used by | [spec_engine_016](spec_engine_016.md) |

## 1. Purpose

Every model is an ADK `model.LLM`. A factory builds one from a model reference for one of four providers, wraps it with per-model settings, images and (optionally) a fallback chain, and gives every SDK a shared HTTP client with retry and stall limits.

## 2. Model references

- **MDL-01** A reference is `provider/model` or a bare `model`. `ParseModelRef(ref, default)` splits on the first `/` only when the prefix is a known provider (`gemini`, `anthropic`, `openai`, `ollama`, case-insensitive); otherwise the whole string is a model of the default provider. OpenRouter-style names therefore need the provider spelled out: `openai/anthropic/claude-sonnet-5`.
- **MDL-02** `NewModel(cfg, override)` uses `override` else `cfg.ModelName()`, builds the primary, and if `llm.fallback_models` is non-empty wraps primary + each buildable fallback in a fallback model. Fallbacks that fail to build are logged and skipped. `NewModelRef` builds exactly one model (no chain) — used by `doctor` and pins.

## 3. Providers

| Provider | Implementation | Credentials |
|---|---|---|
| `gemini` | ADK `gemini.NewModel` over `genai` | `api_key` (Gemini API backend when set); otherwise `project_id`/`location` (Vertex) |
| `anthropic` | Native adapter over `anthropic-sdk-go` Beta Messages API | `api_key`, else SDK resolution (`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ant auth login` profile, workload identity); optional `base_url` |
| `openai` | ADK OpenAI model (Responses API) wrapped for images and text tool calls | `[llm.openai]` key and base URL |
| `ollama` | Same as `openai` | Uses `[llm.openai]`; base URL defaults to `http://localhost:11434/v1` when unset or still the OpenAI default; key is always the placeholder `ollama` (never the OpenAI key) |

- **MDL-10** Empty provider: pick Gemini if a Gemini key exists, else Anthropic if its key exists (its configured model unless `default_model` is set), else OpenAI-compatible if key or base URL set, else local Ollama. Unknown provider: Gemini if a Gemini key exists, else error "unsupported or unconfigured LLM provider".

### 3.1 Anthropic adapter
- **MDL-20** Default model `claude-opus-5`; default `max_tokens` 16000 unless the request sets one.
- **MDL-21** The system instruction becomes one system text block with ephemeral **cache control** (prompt caching).
- **MDL-22** Function declarations become tools with JSON Schema input (raw JSON schema preferred; genai `Schema` converted: lower-case types, enums, formats, items, properties, required, nullable → `[type,"null"]`). An absent schema is an empty object.
- **MDL-23** Contents map to messages; consecutive same-role contents merge (so parallel tool results share one user message). The conversation must start with a user message. `FunctionCall` ⇄ `tool_use`; `FunctionResponse` → `tool_result` (JSON body; `is_error` when the response has a non-empty `error`). An image directly after a tool result is placed inside that tool result (for `view_image`); other images are image blocks; unsupported inline media become a text placeholder.
- **MDL-24** Thinking blocks round-trip unchanged: thinking text in `Thought` with its signature in `ThoughtSignature`; redacted thinking is stored with a `redacted:` prefix. Unsigned thoughts and user-side thoughts are dropped.
- **MDL-25** `temperature` is sent only to models that accept sampling (prefixes `claude-haiku-4-5`, `claude-sonnet-4-6`, `claude-opus-4-6`, `claude-sonnet-4-5`, `claude-opus-4-5`, `claude-opus-4-1`, `claude-sonnet-4-`, `claude-opus-4-0`, `claude-3`); current models reject it with 400.
- **MDL-26** Server-side refusal fallback on `claude-opus-5*` and `claude-fable-5*`: `fallbacks = "default"` routes by category (beta `server-side-fallback-2026-07-01`); a model ID pins one fallback (beta `2026-06-01`); `off` disables.
- **MDL-27** Streaming yields partial text deltas, then one aggregated final response (same shape as Gemini). Non-streaming yields one response.
- **MDL-28** Stop reasons: `max_tokens` → MaxTokens; `refusal` → Safety with `ErrorCode=refusal` and the note "The model declined to continue this request. (<category>)" appended as text; else Stop. An empty final response gets one empty text part. `ModelVersion` = the model that served.
- **MDL-29** Usage: prompt = input + cache reads + cache writes; cached = cache reads; cache writes reported via `CustomMetadata["cache_creation_input_tokens"]`.
- **MDL-30** Errors: 401/403 → "authentication failed … check llm.anthropic.api_key or ANTHROPIC_API_KEY"; 429 → "rate limited"; other status → "API error (N)".

### 3.2 OpenAI-compatible wrapper
- **MDL-40** Never streams (text-encoded tool calls are only recognisable in complete responses).
- **MDL-41** Text tool calls: a text part that is (optionally code-fenced) JSON `{"name", "arguments"|"args"|"parameters"}` becomes a `FunctionCall` **only if the name is a tool offered in that request**, so quoted JSON can't trigger arbitrary tools. `file_path` is aliased to `path`.
- **MDL-42** Images: the ADK OpenAI model rejects image parts, so each image is replaced by a unique marker text (prefixed U+2063) and a request middleware swaps markers for `input_image` data URLs in the outgoing JSON. Markers travel in the request context; nothing is shared between requests.

### 3.3 Images in history
- **MDL-45** Every model is wrapped so stored-image references in a request are expanded to bytes just before sending; history and session files only ever hold references ([spec_images_011](spec_images_011.md)).

## 4. HTTP client, retries, stalls

- **MDL-50** All SDKs share an HTTP client: dial timeout 30 s, TLS handshake 15 s, response-header timeout = stall limit, and a body wrapper that cancels the request if no bytes arrive for the stall limit (each read resets the timer), returning `ErrStalled` "model API stopped responding: no data for <d>".
- **MDL-51** Stall limit = `llm.stall_timeout_seconds` (default 600 s; ≤0 → 10 min). It also bounds non-streamed generations, so it must exceed the longest one.
- **MDL-52** Retries = `llm.max_retries` (default 3; negative → 0) on rate limits, overload, 5xx and dropped connections. Gemini: `Attempts = retries+1`, initial delay 1 s, max 30 s. Anthropic and OpenAI: SDK backoff (0.5 s doubling to 8 s with jitter, honouring `Retry-After`).
- **MDL-53** A stream that fails after output has been shown is not retried.

## 5. Fallback chain

- **MDL-60** The chain reports the primary's name. It tries models in order, skipping those whose circuit breaker is open (threshold: 1 failure — SDK retries already ran). If every breaker is open, it tries them all rather than fail unasked. Breakers are consulted just before each attempt (a half-open trial is reserved).
- **MDL-61** It moves on **only** when a model fails before yielding anything. After output has been yielded, an error ends the call (a failure is recorded). Cancellation never moves on and records no verdict (`Abandon`). A consumer stopping early counts as success.
- **MDL-62** A response from a fallback is marked `CustomMetadata["blitz_fallback_from"] = <primary>` and `ModelVersion = <fallback>` if empty, so cost is priced by the model that answered.
- **MDL-63** If all fail: "every model failed: <joined errors>".
- **MDL-64** The engine notifies the user once when a fallback starts answering ("model.fallback") and once when the primary is back ("model.fallback_recovered"), not on every call.
- **MDL-65** Breaker behaviour (`internal/breaker`): after the threshold the breaker opens for 15 s, doubling up to 5 min; then one half-open trial call is allowed.

## 6. Per-model settings

- **MDL-70** `[model_settings."<model>"]` keys are normalised to the bare model name (provider prefix dropped); when both `gpt-5` and `openai/gpt-5` exist, the bare key wins deterministically.
- **MDL-71** Every built model (primary, each fallback, each pinned agent's model) is wrapped to apply **its own** settings on each call from a lookup carried in the run context, so changes apply from the next call, even in turns already running. The request is copied, never mutated (it is shared across the chain). (Decision: settings apply per built model, not per agent; applying them per agent would give fallbacks the primary's settings.)
- **MDL-72** Unsupported settings are dropped from the request (debug-logged) and reported by `/model_settings`: OpenAI/Ollama have no `seed`; Anthropic accepts `max_tokens`, and `temperature` only on sampling-capable models; `top_p` and `seed` are not sent to Anthropic. Gemini accepts all.
- **MDL-73** `reasoning_effort` (`minimal|low|medium|high|max`; `xhigh` is read as `max`) and `thinking_budget` (tokens, `0` = off) become genai `ThinkingConfig` (level and budget; `max` is `high` there). The OpenAI adapter maps them to `reasoning.effort` (a budget only chooses none or some). Gemini 2.5 takes no level; Gemini 3 rejects a level with a budget, so the level wins. Anthropic: the exact effort reaches the adapter in the request context and goes to `output_config.effort` (`minimal` → `low`) on models that take it (Opus 4.5, Sonnet/Opus 4.6+, and models without sampling parameters); a budget becomes `thinking` enabled (at least 1024, `max_tokens` raised above it, no `temperature`) or disabled at 0, on Claude 3.7 and later. Thinking blocks round-trip with their signatures (MDL-24).
- **MDL-74** The session effort (`/effort`, `--effort`, `SetSetting effort`) overrides every model's `reasoning_effort` for the requests only: `ModelSettings` and saved settings are unchanged. `auto` (or empty) clears it. It is not persisted.

## 7. Test model
- **MDL-80** `MockLLM` returns scripted contents in order (then "Done."), records requests, and can attach usage, `ServedBy` and metadata. It backs the placeholder model and tests.

## 8. Known gaps
- Reasoning settings per model (effort, thinking budgets) are not implemented.
