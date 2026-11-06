---
title: "056 · Conversations across models"
weight: 56
---

*A conversation is Blitz's, not a provider's. It's kept in one neutral form, with what only one provider can read tagged with where it came from. Each model call adapts it to the model about to answer, from one record of what that model can do: what it reads and writes, how it streams, how it thinks, what it costs. Every call is recorded with its cost. So a conversation can change model, even provider, mid-way, and still say what it cost* (`spec_056_conversation_adapters`)

| | |
|---|---|
| Status | **Draft** (2026-11-05). The owner decided, on PR #92: a model switch belongs to the conversation, takes effect from its next turn, and drops the previous provider's reasoning; history stays in ADK's event log, tagged with where each part came from; the status bar offers a new chat before switching one that has history. Its other decisions are in §18. |
| Source | `pkg/engine/runtime` (`origin.go`: origins, tagging and adapting; adapters: `model_factory.go`, `anthropic.go`, `apiprovider.go`, `apiresponse.go`, `enterprisemodel.go`, `cloud.go`, `ollama.go`, `openai_images.go`; wrappers: `settings.go`, `signatures.go`, `fallback.go`, `outputcap.go`, `guard.go`, `images.go`, `media.go`, `geminifiles.go`; `usage.go`, `compact.go`, `contextparts.go`, `listmodels.go`, `httpclient.go`, `quota.go`, `speech.go`), `pkg/engine/session`, `pkg/engine/models.go`, `pkg/images`, `pkg/config/features.go`, `pkg/enterprise/models.go` |
| Tests | `pkg/engine/runtime/conformance_test.go` (CA-150: the switch grid, compaction on Claude, cost), `pkg/engine/runtime/origin_test.go`, `pkg/engine/runtime/wirefake` (the strict fakes, and `wirefake_test.go` proving them strict) |
| Depends on | [spec_015_models](spec_015_models.md) (models, fallbacks MDL-60–65), [spec_018_workspace](spec_018_workspace.md) (WS-40), [spec_011_images](spec_011_images.md) (attachments), [spec_017_sessions](spec_017_sessions.md), [spec_039_secrets](spec_039_secrets.md), [spec_053_enterprise](spec_053_enterprise.md) (the organization's models), [spec_055_urns](spec_055_urns.md) (artifacts), [spec_024_desktop](spec_024_desktop.md) (DSK-39) |

## 1. Purpose

History is kept as Gemini's messages (`genai.Content` in ADK's events). Each provider's own state is stored inside them as it came: Claude's signed thinking in `ThoughtSignature`, Gemini's thought signatures, and IDs in each provider's format. Every adapter reads every part, whoever wrote it. What a model can do is decided in a dozen places, by its name or its provider: what it reads (`media.go`), how it thinks (`anthropic.go`), which settings it takes (`settings.go`), whether it streams (`model_factory.go`, `apiprovider.go`). Cost is one running total per session.

So a conversation that changes model can send one provider another's state. It already does, through:
- `/model`;
- an agent's pin;
- a command's or a job's `model:`;
- `fallback_models`, which mixes providers in one turn. When the mix causes a refusal, the chain hides it by moving on to the next model (MDL-61a).

Found on 2026-11-05 by reading the code. None of the providers' answers has been seen live yet.

- **Protocol.** These break a call:
  - **Gemini to Claude.** Tool calls go out with an empty ID. ADK gives Gemini's calls `adk-…` IDs and blanks every `adk-` ID before each call (`RemoveClientFunctionCallID`).
  - **Claude to Gemini.** Claude's thinking goes to Gemini with Claude's signature on it. Gemini 3 wants its own signature on the current turn's function calls.
  - **To OpenAI.** Any `FileData` part is a hard error (`unsupported content part`). Claude's adapter drops it silently instead.
  - **Fallbacks:** each member of a chain was sent the primary's model name (ADK names the agent's model on the request, and the Gemini and Claude adapters prefer it): a fallback to Claude asked Anthropic for a Gemini model, and a fallback between Gemini models called the primary again.
  - **With no switch at all:**
    - a compaction summary is stored as the model's turn, and the Claude adapter refuses a history that opens with it;
    - masking a secret inside Claude's signed thinking changes what was signed.
- **Capabilities are decided by a model's name, or not at all:**
  - **Claude on Bedrock is misread.** Its IDs (`us.anthropic.claude-…`) don't start with `claude-`, so it is treated as a model without PDFs, thinking, effort or sampling.
  - **A Gemini model behind the gateway** is treated as OpenAI and gets no audio or video.
  - **Vision is assumed.** Every model is assumed to read images.
  - **The organization's catalog is ignored.** It gives each model's capabilities and maximum output; neither is used.
- **Streaming.** It's one switch for the whole engine:
  - OpenAI and Ollama called directly never stream: their tool calls are parsed from whole replies, even for models that make real function calls.
  - A non-streamed call has to finish inside the stall timeout (600 s).
- **Output:**
  - **Some output is refused.** The OpenAI converter errors on any output item other than text, function calls and reasoning: generated images, web search calls, code interpreter calls.
  - **Some output is dropped.** Claude's converter drops server tool blocks.
  - **Some output isn't handled.** Nothing deals with an image a model returns. Nothing acts on Gemini's `SAFETY`, `RECITATION` or `MALFORMED_FUNCTION_CALL`. Nothing recognises "context too long".
- **Cost:**
  - **Double counting.** OpenAI's reasoning tokens are counted twice: they're inside `output_tokens`, and they're also added from `ThoughtsTokenCount`.
  - **Wrong price.** A fallback with no price is charged at the primary's.
  - **One unpriced call** leaves the whole session unpriced.
  - **The organization's prices** are shown but never used.
  - **No per-model costs.** A session keeps one total, with no record of which model cost what.

## 2. Terms

- **Wire API**: the protocol a request speaks. `gemini` (the Gemini API and Vertex AI), `anthropic-messages` (Anthropic, Vertex, Bedrock, Azure Foundry, the gateway's Messages format), `openai-responses` (OpenAI, Azure OpenAI, Ollama's OpenAI endpoint, the gateway's Responses format).
- **Provider**: where the request goes, as configured (`gemini`, `vertex`, `anthropic`, `vertex-anthropic`, `bedrock`, `azure-foundry`, `openai`, `azure-openai`, `ollama`, `api`, `enterprise`).
- **Origin**: a wire API, a provider and the model that answered.
- **Neutral part**: content any model may read, once it's adapted.
- **Provider state**: content only its origin may read back.

## 3. The conversation

- **CA-01** **The event log stays.** History stays in ADK's event log (`<id>.events.jsonl`). Its content is neutral parts and provider state:
  - **Neutral parts:**
    - text;
    - a tool call (name, arguments, Blitz's ID) and its result;
    - an attachment by reference (`blitz-image:<sha>`, CA-60);
    - a file a model made, by reference (CA-70);
    - a provider tool's call and result, in their neutral form (CA-82);
    - a citation (CA-84);
    - a compaction summary.
  - **Provider state:** thinking and its signature, redacted thinking, Gemini's thought signatures, response and item IDs, encrypted reasoning, and a provider's file IDs.
- **CA-02** **Every model event records its origin** in `CustomMetadata["blitz_origin"]` (`{api, provider, model}`). A fallback's answer records the fallback's origin, not the primary's. The provider state itself carries its origin too, so it can be told apart wherever the conversation goes (a request holds contents, not events): each reasoning part's and signature's `ThoughtSignature` is stored as `blitz-state:<api>|<provider>|<model>`, a NUL, then the provider's own bytes (empty for unsigned reasoning). Each model's answers are stamped as they arrive, in the per-provider wrapper (`settingsModel`, `stampResponse`).
- **CA-03** **Tool calls carry Blitz's own IDs.** A call the provider gave no ID gets `bz-<uuid>` when it's stored. ADK's blanking of `adk-` IDs never touches these IDs. A provider's own IDs (`toolu_…`, `call_…`) are kept as they came. Each adapter maps IDs to its API's format and back (CA-23).
- **CA-04** **A compaction summary is neutral context**, never the model's own turn. Adapters that need a conversation to open with the user get it as a user turn (CA-21). ADK's summarizer flattens the history to text (`proseOnly`), so the summary carries no provider state.
- **CA-05** **The model's text is stored as it came.** The UI's masked copy is separate. Secrets are masked in what's shown and logged, never inside provider state (CA-29).
- **CA-06** **Events written before this spec** have no origin, and their provider state has no tag: it's read as foreign to every model and dropped (CA-20). A conversation from before goes on without its old reasoning, which no API needs from an earlier turn. Its tool calls that ADK left without an ID are paired again for the APIs that need IDs (`bz-legacy-<n>`, a call and its result matched by name in order, CA-23).
- **CA-07** **A conversation's model is kept with it** (CA-100), in the session's metadata. Resume, forks and snapshots carry it.

## 4. The model record

- **CA-10** **One record per model.** Every model has one `ModelInfo`, and every capability decision reads it. No adapter or wrapper decides by a model's name or provider. It holds:
  - **Identity:** wire API, provider, canonical name, and the aliases and versioned IDs that map to it (Bedrock's `us.anthropic.claude-…`, Vertex's `@` versions, a gateway's aliases).
  - **Size:** context window, maximum output, and the default `max_tokens` Blitz asks for.
  - **Input** (CA-60): each kind it reads (images, PDFs, text files, audio, video), with MIME types, inline size and page limits, and whether it takes uploads. A kind it can't read is sent as text (a PDF's) or as a note.
  - **Output:**
    - text;
    - images, audio and files it can return (CA-70);
    - structured output (a JSON schema, CA-27).
  - **Tools:**
    - whether it calls tools at all: many local models don't (CA-103);
    - natively, or as text Blitz parses;
    - its schema dialect (CA-25);
    - its limits on names and on tool count;
    - parallel calls;
    - `tool_choice`;
    - the provider tools it offers (CA-80).
  - **Reasoning:**
    - its thinking mode: none, budget, adaptive, or always on;
    - its effort levels and how Blitz's map to them (minimal … max);
    - whether reasoning can be sent back, and must be on a tool loop's last turn;
    - whether its tool calls need a signature.
  - **Settings it takes:** temperature, top_p, top_k, seed, stop sequences, presence and frequency penalties.
  - **Streaming** (CA-50):
    - whether it streams;
    - whether it streams with tools;
    - whether a long non-streamed call is safe.
  - **Caching** (CA-28):
    - automatic or explicit;
    - breakpoints;
    - minimum length;
    - write and read prices.
  - **Price** (CA-120): input, cached input, cache writes (per lifetime), output, reasoning when billed apart, long-context tiers, and per-request or per-query charges for provider tools.
  - **What's been learned about it** (CA-13), and where each fact came from (CA-11), so `/model` can say which facts are known and which are guesses.
- **CA-11** **Where a record comes from**, the later winning:
  - Blitz's built-in table (`pkg/config`, dated as `PriceChanges` is);
  - what the provider says about the model, where it says it:
    - its model list (`listmodels.go`);
    - Ollama's `/api/show`: its capabilities (`tools`, `vision`, `thinking`, `completion`) and its context length;
    - a gateway's or an OpenAI-compatible server's `/models`, where it gives more than names;
  - what Blitz has learned from the model's own answers (CA-13);
  - the organization's catalog, for its models (spec_053: its capabilities, `max_output_tokens` and price);
  - the settings' `[pricing]` and `[model_settings]`, which the person sets and which always win.
  
  **We can't know every model's limits** (the owner, 2026-11-05). A model none of these describes gets its wire API's usual record: tools, streaming, images where the wire API takes them, no reasoning, a 32,000-token context window, no price. Each of those facts is marked as a guess, and CA-13 corrects it from what the model does. Blitz says once, in `blitz doctor` and in the model list, which of a model's facts are guesses.
- **CA-12** **Records are read at each call, not copied when the engine is built.** A price or capability change applies without a restart.
- **CA-13** **A model's limits are learned from its refusals.** When a call is refused because of something the record let through, the adapter recognises the refusal (each wire API's errors, and the words its servers use: "does not support tools", "image input is not supported", an unknown parameter, `stream` refused, a context length or output limit stated in the error), and then:
  1. turns that thing off in the model's record, or sets the limit the error states;
  2. sends the same call again without it, once;
  3. tells the person once per model and fact: "<model> doesn't take images: they're described to it instead".
  
  What can be learned this way:
  - tools: off, or a smaller limit on the tool count;
  - each input kind, and its size limits;
  - each setting;
  - streaming, and streaming with tools;
  - thinking and effort;
  - system instructions (folded into the first user turn);
  - structured output;
  - each provider tool;
  - the context window and the maximum output.
- **CA-14** **Learning is kept and can be undone.**
  - **Where:** what's learned is saved per provider and model in `~/.blitz/models.learned.toml`, each fact with when and why. It survives restarts and is shared by every workspace.
  - **Undoing:** `blitz models forget <ref>` (and **Forget what Blitz learned** on the model in Settings) clears it, and `[model_settings]` overrides it.
  - **Rechecking:** a fact learned from a refusal is tried again after 30 days, or when the provider's model list shows the model changed, since servers and models are updated.
  - **The organization's models:** a learned fact never changes what its catalog says. It's reported to the organization's service with the model's name (spec_053), so its admins can correct the catalog.
- **CA-15** **A refusal that isn't recognised** is an error as today (CA-32), with the provider's words. It's never retried blindly. The conformance suite (CA-150) has a case for each refusal CA-13 recognises, in each wire API's words.

## 5. Requests

Each wire API has one adapter (`Adapter.Request`). It runs at every call, inside each fallback member's wrapper, so each member of a chain gets its own. It turns the conversation into what that API accepts. Compaction, review, judges, the aside (`/btw`) and background tasks call models through the same adapters.

- **CA-20** **Provider state goes back only to its origin.** It's re-sent only to the model that wrote it: the same wire API, provider and model. Every other model gets the conversation, its tool calls and their results, but not the previous model's reasoning. Dropped state stays in the log, so switching back sends it again. Reasoning from a model of the same family is dropped too, unless that provider documents that it carries across.
- **CA-21** **Roles.** Each adapter puts turns in the order its API wants:
  - **Anthropic:** starts with the user, alternates, and each tool result follows its call. A conversation that would open with the model (a summary, CA-04) opens with a user turn holding it.
  - **Gemini:** user and model turns, with function responses in the user turn.
  - **OpenAI:** items in order, each `function_call_output` after its `function_call`.
- **CA-22** **System instructions** go where the API takes them:
  - Anthropic: `system`, text only;
  - Gemini: `systemInstruction`;
  - OpenAI: `instructions`.
  
  Anything in them that isn't text is refused when the agent is loaded, not at the call.
- **CA-23** **IDs.**
  - **Mapping.** Blitz's tool call IDs (CA-03) are mapped to each API's rules: Anthropic's pattern and length, OpenAI's `call_id`, Gemini's optional `id`. They're mapped back when the reply comes.
  - **Pairing.** A call with no result (a turn stopped mid-tool) gets a result saying it didn't run, so no API sees an unpaired call.
  - **Calls from text.** Tool calls parsed from text (CA-24) get Blitz's IDs.
- **CA-24** **Tools are sent natively.** They're parsed from text only for a model whose record says it can't call tools natively. Only those models need their whole reply before tool calls can be read (CA-50).
- **CA-25** **Tool schemas** are sent in each API's dialect:
  - **Gemini:** JSON Schema in `parametersJsonSchema`, which Gemini has taken since November 2025 (it's exclusive with the older OpenAPI-subset `parameters`). Which keywords it refuses (`$ref` is undocumented) is checked in the paid run (CA-151); a keyword it refuses is resolved before sending (`$ref` inlined).
  - **Anthropic:** JSON Schema with `"type"` lower case and `nullable` as `[t, "null"]`.
  - **OpenAI:** JSON Schema, `strict` off.
  
  A schema a dialect can't express is simplified, with what was dropped noted in the tool's description. If it can't be simplified, the tool isn't offered to that model, and `/tools` says why.
- **CA-26** **Names and other tool limits.**
  - A tool or MCP tool name another API can't take (its characters, its length) is mapped for that API and back.
  - More tools than a model takes are cut to its limit, Blitz's own tools first, and the turn says which were left out.
  - `parallel_tool_calls` and `tool_choice` are sent only to models that take them.
- **CA-27** **Settings and structured output.**
  - **Settings:** temperature, top_p, top_k, seed, stop sequences, penalties and `max_tokens` are sent to a model only as its record allows. A setting it doesn't take is dropped, and Studio and `/model` show it as not applying to that model (not only a debug log).
  - **Reasoning:** Blitz's effort (minimal … max) and thinking budget map to the model's own:
    - Gemini's level or budget, never both;
    - Claude's budget or adaptive effort, with `max_tokens` raised to fit a budget;
    - OpenAI's reasoning effort.
    
    A level the model doesn't have takes the nearest one, and the turn's usage says which was used.
  - **Structured output:** a request for it (a JSON schema, used by review, judges and workflow steps that ask for it) uses the model's own structured output where it has one. Otherwise it uses an instruction and validation, retried once.
- **CA-28** **Caching** is the adapter's:
  - **Anthropic:** breakpoints on the system prompt and tools, and on the conversation's last stable turn. Its record says whether 5-minute or 1-hour writes are used.
  - **Gemini and OpenAI:** their automatic caching, with a stable prefix kept stable (the system prompt and tools, then history, never reordered).
  
  Cache reads and writes are recorded with each call (CA-120).
- **CA-29** **Secrets** in the conversation are expanded for the call as today (`expandContents`), in neutral text only, never inside provider state. A secret a model writes is masked in what's shown, logged and kept in the transcript (spec_039_secrets). Signed reasoning isn't edited: reasoning with a secret in it is hidden from view and kept unchanged for the model that wrote it.

## 6. Responses

Each adapter turns a reply back into the conversation (`Adapter.Response`).

- **CA-30** **Every part of a reply is kept or turned into a neutral part.** Nothing is an error and nothing is silently dropped:
  - text;
  - reasoning, as provider state;
  - tool calls;
  - provider tool calls and results (CA-80);
  - citations (CA-84);
  - images, audio and files (CA-70).
  
  A part Blitz doesn't know is kept as provider state, and the turn notes "<model> returned <type>, which Blitz doesn't show yet".
- **CA-31** **Why a reply ended** is normalised: `stop`, `tool_calls`, `max_tokens`, `refusal`, `safety`, `recitation`, `malformed_tool_call`, `context_length`, `other`.
  - `max_tokens` keeps today's handling: a cut-off tool call is noted and not run, and the turn ends with `OUTPUT_LIMIT`.
  - `refusal` and `safety` end the turn with the provider's words and `REFUSED` or `BLOCKED`.
  - `recitation` ends it with `RECITATION`.
  - `malformed_tool_call` is retried once with a note to the model, then ends with `MALFORMED_TOOL_CALL`.
  - `context_length` is CA-111.
- **CA-32** **Errors** are normalised per wire API to one set:
  - `auth`;
  - `quota` (the provider's, or the organization's, `ErrOrgLimit`);
  - `rate_limit` (429, with its Retry-After);
  - `overloaded` (503, 529);
  - `context_length`;
  - `bad_request` (with the provider's message);
  - `unavailable`;
  - `canceled`.
  
  Retries keep today's settings (`llm.max_retries`, backoff from 1 s to 30 s). Only `rate_limit`, `overloaded` and `unavailable` are retried.

## 7. Streaming

- **CA-50** **Each call streams when the model can.** Whether it streams is decided per call, from the model's record:
  - **Streaming at all** depends on whether the client wants it (the service does; a one-shot CLI call with `--output json` doesn't need to) and whether the model streams.
  - **With tools offered,** it also depends on whether the model streams with tools.
  
  A model that parses tool calls from text (CA-24) streams its text to the person and parses the whole reply at the end. OpenAI and Ollama models with native tool calls stream.
- **CA-51** **One stream shape.** A streamed reply is passed on as partial text and partial reasoning (shown as "thinking", never as answer). Tool calls are passed on only once complete. A non-streamed reply is passed on whole, as one final response, the same shape. The desktop and the CLI show "writing…" while a non-streamed call runs.
- **CA-52** **A long non-streamed call** isn't cut by the stall timeout:
  - a model whose record says a long call is safe gets a timeout sized to its maximum output;
  - one that isn't safe is streamed internally and passed on whole (today's Anthropic behaviour, made general).
  
  The stall timeout applies to the gaps between chunks of a stream, not to a whole call.
- **CA-53** **A model that refuses to stream** (a 400 on `stream`) is called unstreamed from then on. This is learned per model (CA-13), not per wrapper. The record says so, and `/model` shows it.
- **CA-54** **What reaches the screen keeps today's rules** (MDL-61):
  - a fallback moves on only before anything has been passed on;
  - masking holds back a possible secret's tail (`guardrails.Hold`) whether the reply streamed or not;
  - cancellation stops the stream and keeps what came;
  - a steer waits for the next tool result.

## 8. Multimodal input

- **CA-60** **An attachment is kept once, as it came,** under its hash (`~/.blitz/images`), and referenced from the conversation (`blitz-image:<sha>`). That covers a picture, a PDF or other document, a text file, audio and video.
  - Whatever a model can't take is made from the original at each call: scaling and re-encoding to its limits (CA-61).
  - Today every picture is scaled once to Claude's limits. From now on each model gets its own size from its record.
- **CA-61** **At each call, each attachment takes one route by the model's record:**
  - **inline,** within its limits;
  - **uploaded:** Gemini's Files API, where the record says uploads are taken. The upload's ID is kept beside the file, never in the conversation, and reused while valid. Every model of a fallback chain must take uploads for it to be used.
  - **as text:** a PDF's text, cut at 300,000 characters;
  - **as a note:** "[clip.mp4: this model can't take video]".
  
  A fallback chain gets only what every member takes (today's rule).
- **CA-62** **Attachments inside tool results** (a screenshot, `view_image`, `view_document`, `view_media`) go where each API takes them:
  - Gemini: beside the function response, in the same turn;
  - Anthropic: inside `tool_result.content`;
  - OpenAI: `function_call_output`, then the image as `input_image` in a user item.
  
  A kind the API can't put there is CA-61's text or note.
- **CA-63** **Attachments are checked against the model that will read them at its next call**, not only the active agent's model when they're added.
  - **On a switch,** the switch says what the new model won't see: "3 videos and 1 audio file will be described, not shown, to <model>".
  - **On adding a file,** one no model of the conversation can take is still refused (IMG-63).
- **CA-64** **Vision is a capability, not an assumption.** A model whose record doesn't read images gets each image as a note naming it. A local model's vision is read from Ollama where it says so, and taken from `[model_settings]` otherwise.

## 9. Model output and artifacts

- **CA-70** **What a model makes is kept like an attachment:** an image (a Gemini or OpenAI image model's, an image-generation provider tool's), audio, or a file a provider tool wrote (code execution's output, a chart).
  - It's stored under its hash in the same store, and the conversation references it (`blitz-image:<sha>`, with its MIME type and `made_by` origin).
  - It's never kept as base64 in the event log.
  - A provider's file ID (Anthropic's or OpenAI's Files API) is downloaded when the reply comes and kept as provider state, so the file stays when the provider's copy expires.
- **CA-71** **Showing it.** What a model makes is shown in the conversation (an image, a player, a file chip with **Save**), and the CLI prints its path. In a job run (spec_053 EN-40), it's written to the run's `out/` folder too, so it becomes one of the run's artifacts (`urn:blitz:artifacts:…`, spec_055).
- **CA-72** **Another model reads it back as an attachment** of the user's (CA-61). Most APIs don't take images in the model's own turns, so the adapter moves it to the next user turn, after the model's text, with a note saying which model made it.
- **CA-73** **Spoken output.** `generate_audio` (the `[audio]` model) returns its file's reference in its result as `view_media` does, so the next model can hear it where its record allows.

## 10. Provider tools

- **CA-80** **Provider tools are on wherever they can be** (the owner's decision 2026-11-05).
  - **Which:** a provider's built-in tools: Google Search grounding, URL context and code execution; Anthropic's web search, web fetch and code execution; OpenAI's web search, file search and code interpreter. Each model's record lists the ones it offers.
  - **When:** each is sent with every call to a model that calls tools (CA-10) and offers it, in place of Blitz's client tool of the same purpose (`web_search`, `web_fetch`, `run_code`). The two are never offered together, so the model isn't asked to choose between them.
  - **Otherwise:** a model without the provider's tool gets Blitz's client tool as today.
- **CA-81** **Provider tools follow Blitz's rules, as the client tool they replace would.** A provider tool isn't sent when its client equivalent wouldn't be allowed:
  - by the permission mode (plan and read-only modes keep web search and drop code execution);
  - by an agent's `tools`, which can name one off (`-provider:code_execution`) or all (`-provider:*`);
  - by `[tools] provider = false` in the settings;
  - by a guardrail;
  - by the organization's policy (spec_053), which can turn any of them off.
  
  Each provider tool call is written to the audit log as the client tool's would be. Its cost counts toward the turn's limits (CA-83, CA-124).
- **CA-82** **Stored as Blitz's own tool use.** A provider tool's call and result are stored as a call to Blitz's client tool of the same purpose (`web_search`, `web_fetch`, `run_code`), with Blitz's ID (CA-03) and its result as text, with any files (CA-70) and citations (CA-84). The provider's raw blocks are kept as provider state beside them.
  - **What any model reads:** an ordinary tool call and its paired result, of a tool it knows. It doesn't read prose about another model's work, and it can't be asked to pair a result it never requested.
  - **For the model that made them:** they're sent back in the provider's own form (CA-20).
  - **Declaring the tool:** if the history holds calls to a client tool that isn't being offered (its provider tool is on instead), the adapter still declares it, for APIs that check a history's tool names against what's declared. Its description says the provider's tool is the one to use now.
  - **A provider tool with no client equivalent:** its call and result are stored as a call to `provider_tool` (name, input, result), declared the same way.
- **CA-83** **Provider tools are priced** as each provider charges: per query or per container hour, priced from its record (CA-120), recorded with the call that used them, and counted toward cost limits.
- **CA-84** **Citations** (Gemini's grounding, Anthropic's and OpenAI's citations) are kept neutrally (source title, URL, the text they support) and shown under the answer. Another model gets them as part of the text.
- **CA-83** **Provider tools are priced** as each provider charges: per query or per container hour, priced from its record (CA-120). They're recorded with the call that used them.

## 11. Fallbacks

- **CA-90** **Each member of a fallback chain gets its own adapter** (CA-20–29), so a member never receives another's provider state.
- **CA-91** **A fallback mid-turn** sends the partial turn (the tool calls and results so far) through its own adapter (`adaptRequest`). When its API requires state only the first model has, the adapter meets the requirement:
  - **Gemini**, which requires its signature on the first call of each step in the turn under way: the placeholder it documents (`skip_thought_signature_validator`).
  - **Claude**, which with thinking on requires its own thinking at the head of the assistant message a tool loop goes on from (and Opus 5.5 and Fable always think): the other model's calls and their results in the turn under way go as text ("[Called read_file with {…}]", "[read_file returned {…}]"). Claude's own calls, with their thinking, stay calls.
  - **OpenAI** needs nothing: it reads any paired calls.
  
  Where nothing can meet it, the chain moves on as for any error before output (MDL-61a), and the notice says why.
- **CA-91a** *2026-11-05 (found by the conformance suite).* **Each member of a chain asks for its own model.** ADK names the agent's model on every request (`LLMRequest.Model`), which for a chain is its primary, and the Gemini and Claude adapters prefer that name to their own: a Claude fallback asked Anthropic for `gemini-3-pro`, and a Gemini Flash fallback asked for Pro again. Each member's request now names the member (`adaptRequest`).
- **CA-92** **A fallback's answer** records its own origin (CA-02) and is priced at its own model's price. It's unpriced when its model has no price, never charged at the primary's.

## 12. A conversation's model

- **CA-100** **A conversation has its own model.** `/model <ref>`, the desktop's status bar (DSK-39) and `SessionService.SetModel` set it for this conversation, from its next turn.
  - Other conversations and jobs in the workspace keep theirs.
  - A turn that's running finishes on the model it started with.
  - The workspace's default (`blitz.default_model`, WS-40) is what a new conversation starts with.
  - An agent's pin still decides what that agent runs on, and `/model` says so.
  - A command's or a job's `model:` runs that run on its model, through the adapters, and doesn't change the conversation's.
- **CA-101** **Before the first turn on a new model, Blitz prepares the switch:**
  1. **The context:** if the conversation is past the new model's compaction point (CA-110), it's compacted first.
  2. **The attachments** the new model won't see are named (CA-63).
  3. **The tools** it won't be offered are named (CA-25, CA-26).
  4. **The cost:** "From the next turn, <model> reads this conversation again: about N tokens, $X." There's no amount when the model has no price.
  
  `SessionService.PreviewModel` returns all four without switching.
- **CA-102** **A new chat.** `/model <ref> --new` and the status bar's **New chat with <model>** start a new conversation on that model, in the same workspace, with nothing carried over.
- **CA-103** **A model that can't call tools.** A model whose record says it doesn't call tools (many local Ollama models) runs the agent without tools.
  - **On a switch:** the preview says so: "<model> can't use tools: <agent> will only answer, without reading or changing files".
  - **On choosing one:** `/model` and the status bar mark these models.
  - **In a fallback chain:** a call that offers tools skips such a member, and the notice says why.

## 13. Context and tokens

- **CA-110** **The compaction point is the model's:**
  - `token_threshold` when set (today's setting);
  - otherwise the model's context window, less its maximum output, less 10%.
  
  The meter (DSK-39) is measured against it.
- **CA-111** **A `context_length` error** is summarised away automatically: the conversation is compacted, with nothing asked (the owner's decision 2026-11-05), the turn notes "The conversation was summarised to fit <model>", and the same model is tried once more. If it still fails, the error goes to the fallback chain (MDL-61a). Without compaction, it ends the turn with `CONTEXT_LENGTH`.
- **CA-112** **Counting before a call:**
  - the provider's count-tokens API, where the record has one, for a switch's preview (CA-101) and before a call that would be close to the limit;
  - an estimate otherwise (today's: characters/4, 1500 per image and per PDF page, audio and video by length), scaled to the last count the model reported.
  
  The meter says which it shows.

## 14. Cost

- **CA-120** **Each model call adds a record to the conversation's ledger:**
  - when it happened;
  - its origin (CA-02);
  - what it was for (the turn, compaction, review, a judge, a background task, a subagent);
  - its usage, each kind once: input, cached input, cache writes (by lifetime), output, reasoning, provider tool requests;
  - the price it was charged at, and its cost; or that it had no price.
  
  The ledger is kept with the session (`<id>.usage.jsonl`), copied by forks and snapshots, and cut back with a rewind.
- **CA-121** **Totals are sums of the ledger**, by conversation, model, provider and purpose.
  - **A call with no price** makes only that call's cost unknown. The total says "$X, plus N calls with no price".
  - **Speech and search** are recorded the same way, with their own origin.
- **CA-122** **Usage is normalised once per wire API:**
  - OpenAI's reasoning tokens are inside its output tokens and aren't added again;
  - Gemini's thoughts are added to its candidates;
  - Claude's thinking is inside its output;
  - cache writes are counted from Anthropic's `cache_creation` (per lifetime).
- **CA-123** **Prices** come from the model's record (CA-11), including the organization's prices for its models. Long-context tiers and per-request charges apply when the record has them.
- **CA-124** **Cost limits read the ledger:** `Turn.max_cost_usd`, the limits guardrail (spec_041 GT-32), a job's and a background task's. A limit can't be kept with calls that have no price, and the turn says so when it starts, as `--max-cost-usd` does today.
- **CA-125** **Showing it.**
  - `SessionService.GetUsage` returns the ledger's totals by model and purpose, and the calls when asked.
  - The desktop's cost (the status bar's hover, DSK-39) and the CLI's `/usage` show the breakdown.
  - `blitz sessions show <id> --usage` lists the calls.
- **CA-126** **Sessions from before this spec** keep their total as one record ("before 0.9.x, by model unknown"). Calls from then on are recorded by model.

## 15. The service, the CLI and the desktop

- **CA-130** **The service.** *Built 2026-11-05.* `SessionService` gains:
  - `SetSessionModel(workspace, session_id, model)`: moves the conversation, compacting it first when it wouldn't fit (`SESSION_NOT_FOUND`, `BAD_MODEL_REF`);
  - `PreviewSessionModel(workspace, session_id, model)` (CA-101);
  - `SessionInfo.model`: the conversation's own model, "" for the workspace's;
  - `GetUsage` by model (CA-125).
  
  `WorkspaceService.SetModel` keeps setting the workspace's default for new conversations (WS-40). The fields are added, never renumbered (`buf breaking`). The conversation's model is kept in the session's metadata (`SessionRecord.Model`), copied by forks and snapshots, and runs its turns (`runtime.WithConversationModel`; a command's or job's own model, `WithModel`, wins over it).
- **CA-131** **The CLI.** *Built 2026-11-05.*
  - `/model <ref>` switches the open conversation with CA-101's notice ("From the next turn, <model> reads this conversation again: about N tokens, $X."); with no conversation open, it sets the workspace's model, which the next one starts on. `/model <ref> --new` sets the workspace's model and starts a new conversation on it.
  - `/model` alone shows the conversation's model and the workspace's default (and, once CA-10 is in, the record's main facts: context, input kinds, tools, reasoning, streaming, price).
  - The notices are translated (the owner's decision 2026-11-05: CLI output is translated).
- **CA-132** **The desktop's status bar** (DSK-39) lists models under **Model**: models with settings of their own first, then the provider's chat models from their records (never embedding, image-only or speech models).
  - In a conversation with history, choosing one offers **New chat with <model>** first and **Switch this conversation** second, with CA-101's preview.
  - With no history, it switches at once.
  - What's shown as the model is the conversation's.
- **CA-133** **Docs.** The site's models guide explains switching (what carries over, what doesn't, what it costs), and the release notes say what changed.

## 16. Compatibility

- **CA-140** **Old sessions.** Sessions from before this spec load and continue: they have no origins (CA-06), no ledger (CA-126) and no model of their own, so they take the workspace's default.
- **CA-141** **Old clients.** An older client talking to a newer service still works: `WorkspaceService.SetModel` still switches what new conversations start with, and `GetUsage`'s old totals stay filled.
- **CA-142** **A downgrade.** A session written by a newer Blitz still loads in an older one: `blitz_origin` is metadata it ignores, and `bz-` IDs are IDs.

## 17. Proof

- **CA-150** **A conformance suite** (`pkg/engine/runtime`) runs a scripted conversation across every pair of wire APIs, in both directions, at a turn's end and mid-turn (fallbacks):
  - **Content:** tool calls, thinking, an attachment of each kind, a tool-result screenshot, a model-made image, a provider tool's result with citations, and a compaction.
  - **Streaming:** each switch is run streamed and not, and with text-parsed tool calls.
  - **The fakes** are as strict as each API. They refuse:
    - empty, unpaired or badly formed IDs;
    - roles out of order;
    - a missing or foreign thinking block;
    - a foreign or missing signature;
    - a part in a place the API doesn't take it;
    - a schema outside its dialect;
    - a setting it doesn't take.
- **CA-151** **Each fake's rules come from the provider's documentation** and are confirmed once against the real APIs before the suite counts as proof. That's a short paid run of the switch grid that the owner approves, with its cost quoted first. The run's date, models and cost are recorded here.
- **CA-152** **Cost tests:**
  - a mixed conversation priced by hand is compared with the ledger, including unpriced calls, fallback calls, cache writes, reasoning, provider tools, speech and search;
  - each wire API's usage normalisation (CA-122) has a case of its own.
- **CA-153** **Journey tests** (the desktop's scripted window stream and the CLI's REPL) cover:
  - a switch with history (the menu, the preview, the next turn on the new model, the cost by model);
  - a new chat from the menu;
  - a switch that compacts first.
- **CA-154** **The soak.** The Linux soak runs a long conversation that switches model every 50 turns across the fakes. It must stay flat (memory, file descriptors, threads), and its ledger must add up to its totals.

## 18. Decisions

All three settled by the owner on 2026-11-05:

- **CA-160** **Provider tools are on wherever the model supports tools** (CA-80–83), within Blitz's rules (CA-81).
- **CA-161** **Images:** Blitz handles images a model returns (CA-70) now. An image-generation tool comes later (`.agents/TODO.md`).
- **CA-162** **`context_length`:** summarised automatically, without asking (CA-111).

## 19. Not in this spec

- Embedding models (spec_035 search) and speech models (`[audio]`) beyond CA-73: they don't hold conversations.
- Changing what an agent's pin means (spec_014).
- Batch APIs.
