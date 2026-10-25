# Structured output compatibility

The adapter accepts Responses requests using text.format.type=json_object or
json_schema. BPS rejects native text/response_format request fields, so the
adapter sends the output requirements as developer instructions and validates
the final response locally. This does not provide upstream constrained decoding.

- Preserve the requested model and account. Model-access rejections remain
  upstream errors; structured output support does not grant model permissions.
- Return a successful final answer only when it is valid JSON and, for
  json_schema, satisfies the supplied schema. Do not retry, repair, strip fences,
  or replace invalid model output with fabricated data.
- Withhold structured message text until the terminal response is validated.
  Reconstruct message events from that validated response, rather than replaying
  unvalidated deltas. Ordinary text requests remain incremental.
- Preserve tool continuations and explicit refusals as protocol items, outside
  the final-answer JSON contract. Preserve upstream failure/incomplete status
  without exposing partial structured message text.
- Resolve schema references inside the submitted document only. Never fetch
  remote schemas or read local files. Limit schemas to 4 MiB and answer text to
  16 MiB, subject to the existing SSE event limit.

# Tool history and input boundaries

- Client function schemas and tool transport envelopes allow 4 MiB at their
  respective encoding stages. Catalogs allow 8 MiB, native replay items 16 MiB,
  and persistent entries 16.5 MiB including wrapper/signature overhead. Each
  process cache and the shared Redis cache have a 32 MiB total budget. Tool
  correction remains limited to two rounds. These are local resource guards,
  not verified provider maxima. The SSE event and translated read-pump limit is 32 MiB.

- OAuth models routed through BPS do not advertise native encrypted multi-agent
  capabilities. Apply this limit to generated, fetched and pinned group catalogs
  by the actual mapped route, and recalculate the client ETag. Plaintext client
  tools remain available; refresh the catalog and start a new conversation to
  leave an old client-side v2 override or encrypted history behind.
- Rebuild complete CUSTOM calls using their current catalog's exact
  `codex2api.custom/NAME` marker and raw input. Preserve cached native calls
  verbatim. Retain unavailable historical tools as recorded history; never
  infer a new tool target from executable text. The one bounded exception is
  `recoverTransportEnvelope`: a `name(...)` / `await name(...)` call whose
  callee exactly matches one catalog entry and whose single argument is one
  JSON literal is read as that tool's envelope. Anything less exact stays
  unverifiable.
- A result may precede its call in client history. The result restores the
  native call from the replay cache once; the later explicit call for the same
  call_id is not emitted a second time.
- Fields BPS cannot carry (`max_output_tokens`, `max_tool_calls`, `truncation`,
  `include`, `service_tier`, `top_logprobs`, `reasoning.summary`) are dropped
  and named in `Bridge.Warnings`; they are never silently applied.
- Require a matching complete call or scoped replay-cache entry for each tool
  result. Report the input path when neither is available. Do not invent calls
  or discard their results.
- Share the native Responses classification for named function_call_output
  inputs with no call_id: these are externally supplied context, not paired tool
  results. Lower them to user messages with explicit source attribution and the
  original text/image order, without inventing a call or claiming execution.
  Their inline images use the existing message-attachment upload path. Count
  them as a new input turn and retain them in the unconsumed compaction tail.
  Reject a current encrypted standalone assignment before omission or upload;
  only explicitly enabled historical omission may replace old ciphertext.
  Nonempty call_id outputs still require complete matching calls or scoped cache.
- Reject encrypted message parts with an `encrypted_content` diagnostic and
  input path. Do not reinterpret ciphertext as plaintext. Top-level encrypted
  reasoning items retain their existing handling.
- When the upstream rejects encrypted conversation state (HTTP 400 or an
  in-stream error that `isOpenAIEncryptedContextRejection` recognizes), the
  gateway does not retry. It never removes reasoning ciphertext, encrypted
  messages, tool results or compaction context from the prepared history, never
  switches accounts or protocols for that request, and does not change account
  health. Native, BPS, compact SSE and WS share the same classification: the
  request fails with `invalid_encrypted_content` and plaintext-history recovery
  guidance (resend the original plaintext history or start a new conversation).
  Ordinary 400s keep their own handling. The only lossy option is the explicit
  account switch `openai_excel_bps_ignore_encrypted_content`, which omits old
  encrypted message parts before the request is sent; it is not a retry and does
  not decrypt anything. This replaces the source project's one-shot
  reasoning-stripping retry, which was deliberately not adopted here.
- Preserve `detail: original` on HTTPS images and inline images rewritten by
  the relay. Let the upstream model validate its supported detail levels; do
  not silently downgrade the requested detail.
- Send only type and file_id for validated message attachment references,
  including uploaded images, replayed IDs and images moved from tool results.
  Inline tool screenshots retain their existing detail handling.
- Enforce the saved inline-image count and byte limits; defaults remain 20
  inline images and 64 MiB per request for the relay.
  A relay capacity error and an upstream overload are separate from a tool
  protocol error; HTTP 200 alone does not establish a successful SSE terminal.

# Tool transport corrections

- Validate a complete native tool batch before committing replay entries or
  emitting any client tool events. Never evaluate transport code in the gateway.
- When a completed batch consists entirely of identifiable run_officejs calls
  with valid outer arguments, return local validation errors as native tool
  results with `executed: false`. Continue on the same BPS account, model, effort,
  proxy and scoped conversation, with at most two corrective model requests.
- Preserve intended operations and call order. Require the corrected batch to
  have the same number of calls and pass the current catalog's transport checks.
  Restore already valid operations when their tool target is unchanged. For an
  invalid raw call, bind the original bytes whether the correction uses an explicit
  CUSTOM/FUNCTION_CODE/FUNCTION_CMD marker or an ordinary envelope for a declared
  custom tool or eligible code/cmd function. Do not guess raw fields on unrelated
  functions. Check the restored operation and size limits before dispatch. Never dispatch
  source text rewritten by the correction model, including whitespace changes.
  Store the bound native call in replay history so later turns see the exact
  operation dispatched to the client. Do not reinterpret unmarked named JSON
  envelopes as raw source; conflicting targets and broken nested wrappers remain
  unverifiable even when a correction copies their JSON into CUSTOM input. An
  explicit raw marker takes precedence over business fields named name/arguments
  inside literal JSON source. An unmarked, unnamed JSON object whose keys are
  all declared properties of some function tool in the current catalog is a
  bare argument object: a correction may only re-frame it as a function
  envelope with identical values; redirecting it into CUSTOM input or a raw
  code/cmd field is `original_payload_unverifiable`. JSON that no declared
  function could own stays literal and may be bound as CUSTOM input. Retain
  target, metadata and whole-batch checks.
- A wrong FUNCTION_CMD marker for an exact known function with explicit cmd:string
  can use the existing formatting corrections to produce an ordinary FUNCTION
  envelope only when the full original arguments pass its schema. Bind the exact
  original command bytes and require unchanged target and metadata. Unknown targets,
  wrong kinds, CODE precedence and unverifiable arguments remain rejected without
  selecting a replacement tool. Contract diagnostics contain only bounded target,
  reason, transport, catalog fingerprint and attempt/visibility flags.
- FUNCTION_CMD metadata is deliberately not validated against the declared
  parameter schema: the raw command is the operation and the metadata JSON is
  passed through as declared by the model (`function_cmd_transport.go`,
  `TestFunctionCmdRealParametersPassThrough`). Only cmd conflicts are checked.
  FUNCTION and FUNCTION_CODE arguments keep full schema validation.
  Keep ordinary text incremental and retain its original response identity and
  output indexes. Hide intermediate correction text and native tool events.
- A missing catalog target is a formatting error, not eligibility for unknown-tool
  regeneration. Bare JSON argument objects can gain a named envelope only if
  every argument value is preserved, including large integers; object key order
  and whitespace are immaterial. Use the existing bounded transport decoder for
  complete JSON fences, prose labels and encoded string layers before comparing
  argument objects. Recognize these objects before raw-payload binding, whether
  the correction uses an ordinary envelope or a raw code/cmd marker: never put
  the entire JSON argument wrapper into an executable field. Explicitly marked
  raw source and CUSTOM input remain literal bytes, not argument objects.
  Preserve explicit raw-transport metadata too. An invalid named envelope cannot
  authorize arbitrary replacement arguments. Rejections retain bounded structural
  diagnostics (reason, tool index, attempt, executed=false, original code shape
  and corrected tool kind), without code, argument values, identifiers or catalog contents.
- Release the preceding response body before a correction request so accounts
  with concurrency one do not deadlock. Cancel corrections on client disconnect.
  Include all returned terminal usage in the final success or failure response.
- This formatting path does not retry transport I/O failures, upstream rejections, incomplete streams, undeclared tool targets, unsupported native tools, missing call identities, function argument schema failures or structured answer failures. Preserve any explicit declared target on correction.
  Do not infer a tool target from arbitrary raw code. Stop on a changed batch or
  exhausted correction limit without dispatching any client tools.
- Treat this as bounded model correction, not constrained upstream decoding or
  a guarantee that arbitrary malformed model output will always recover.

# First-turn unknown-target correction

The gateway separately permits one regeneration when the first tool interaction ends in exactly one undeclared run_officejs target at the end of a completed response. It must have no prior tool calls/results and no dispatched client tool. This path reuses the prepared request, current catalog, account, model, proxy and attachment IDs; it does not append a fabricated executed tool result. Its corrected response must pass the current catalog, argument schema, identity and parallel-call checks. A second unknown target, invalid arguments or incomplete response fails without dispatching tools. Both attempts' reported usage is retained, including progressive usage if the correction disconnects before its terminal event.

This path and the existing known-target formatting path are selected independently. A function argument schema error alone does not trigger either path.

For streaming clients, first-turn regeneration stops once nonempty text, reasoning
summary or refusal content has been forwarded. An unknown target then produces
`response.failed` with the original response identity and usage, without tool
dispatch or another generation. Empty lifecycle events and opaque reasoning alone
do not block regeneration; non-streaming requests remain buffered and retain their
one correction attempt. Known-target formatting corrections still preserve the
original text and replace only withheld tool slots.

# Optional inline image limit policies

Administrator settings under Facilities → Feature switches → Excel / BPS image
support select automatic compaction (default for new or unset settings), off,
or warning interception. Explicitly saved off/warn choices are preserved.
Existing persisted settings need no migration. Clients omitting the new fields
preserve their current values. Native uploads and HTTPS relay use the same
inline image counting rules, including tool outputs and agent messages; repeated
image occurrences each count. Existing size and resource limits still apply.

In native mode, off still validates the decoded 64 MiB limit and returns
basispoints_image_auto_compact_disabled with the setting needed to enable
recovery. Repeating the same oversized request cannot fix a disabled policy.
Preflight diagnostics contain policy/count/byte totals and whether compaction
has started, never image data, messages or credentials. Local preflight failures
have no upstream status. Code publication does not change a running site's settings.

Automatic compaction only runs when history plus new images exceeds the configured
limit and each partition fits independently. It accepts Codex client identities
and a stable session scoped by account, API key, thread and model. A digest of the
last successful input identifies the unconsumed tail; bootstrap accepts only a
trailing user batch or a complete terminal tool call/result batch. Uncertain
boundaries fail explicitly. Old history is compacted at most once with client
tools disabled, and the actual encrypted compaction window is used for the
continuation. New inputs stay intact. The compacted window is emitted before
continuation items with adjusted output indexes, and both phases' reported usage
is counted even when generation fails. This consumes additional model tokens.
The client must retain and echo the compaction output items on later requests;
the gateway reconciles verified history checkpoints as described below. Mock
protocol tests do not replace acceptance testing in the actual Codex client;
unsupported clients should use manual compact.

Warning mode requires 1 <= reserve < warning remainder < maximum images. With
20/8/3, 0–11 pass, 12–17 warn once per conversation cycle then pass on retry, and
18–20 block ordinary requests. At 12 images the user sees 5 available slots.
Only administrators see the reserve setting. Explicit compact endpoints and native
compaction triggers bypass these warning thresholds, but not the total limit.
Successful compact or a return below the warning threshold starts a new cycle.
Redis stores only progress position/digest and the atomic warning marker, expiring
after two idle hours. A missing stable session identity rejects requests requiring a policy action.
Redis session-state errors fail closed for ordinary requests; manual compact
remains available when Redis policy state is unavailable.

## Gateway checkpoint reconciliation

Ordinary Codex compaction output does not itself replace local client history.
After automatic compaction, the gateway commits an authenticated checkpoint
before emitting the window. Redis stores only canonical SHA256 digests, input
positions and split positions; it never stores the compacted window or full
messages. The client must echo the exact emitted window immediately after the
request input. On subsequent requests the gateway verifies the prior input and
window, moves the preserved new-input tail after that window, and drops only
the verified compacted prefix from the BPS request. It applies at most 16 such
checkpoints in order. Altered/missing windows fail explicitly where the prefix
matches; unrecognized histories retain normal limits. Concurrent checkpoint
writes use compare-and-swap and abort on conflict before generation.

The client's raw upload can still include old images. Raw request-size protection
remains in force; this is gateway reconciliation, not client memory cleanup.
A manual compact resets the checkpoint chain. State expiry or a different account,
model, key or thread can require manual compact again. Checkpoints remain useful
if generation fails after emitting the compaction window; all usage is still billed.

# Configurable image capacity

Administrators can raise image limits for larger servers without changing the
conservative defaults or existing saved settings. The settings UI, admin API,
per-request settings reader and admission middleware use these ceilings:

| Setting | Default | Maximum |
| --- | ---: | ---: |
| Request body | 128 MiB | 1024 MiB |
| Shared admission budget | 1024 MiB | 65536 MiB |
| In-flight requests | 128 | 4096 |
| Relay image size | 32 MiB | 512 MiB |
| Relay image bytes per request | 64 MiB | 512 MiB |
| Images per request | 20 | 65536 |
| Relay disk storage | 1024 MiB | 262144 MiB |
| Relay stored images | 512 | 1048576 |
| Relay link lifetime | 30 minutes | 10080 minutes |

The budget must cover eight times the configured body limit, and its effective
value remains at least eight MiB per in-flight slot. It is accounting capacity,
not preallocated RAM. Storage must cover one request's image bytes and count;
request image bytes must cover one image. Base64 expands encoded request bodies.
The server/global body limit, gateway body limit and reverse proxy can impose
smaller limits; to admit bodies above their defaults, adjust them explicitly.
Higher settings do not bypass those limits or provision memory or disk.

Native attachments use local 32 MiB per-image and 64 MiB per-window size
limits. Local reliability adaptation (2026-10-14): upload original message
images afresh for each explicit client request, deduplicating only inside that
request. Superseded 2026-10-24: AttachmentCache reuses uploaded IDs across
requests of one trusted conversation scope (512 entries, 60 minutes). An upload
response still has no ownership-expiry proof, so callers invalidate a request's
IDs when it fails file ownership validation and may upload the originals again. Keep the global 32-upload bound; repeated images
therefore add upload traffic and latency. Distinct images of one request upload
with at most four in flight (2026-10-24); the first failure cancels the rest
and is returned, while IDs already uploaded stay reusable within the request. The per-request image count is
configurable in both modes. Both modes retain the 64-megapixel safeguard.
Relay download concurrency and automatic compaction checkpoint limits are
unchanged. Large capacity settings require appropriate resource provisioning;
these ceilings are not throughput or memory-use guarantees.

For account-bound context, a rejected native attachment upload (HTTP 429 only)
may be retried once on the same account, credentials and proxy. The gateway
retains successful parts and shares this single retry across all images in the
request. Honor Retry-After or the existing 429 fallback, with a minimum one-second
wait and a five-second maximum; longer cooldowns return through the existing 429
path. Cancellation, the original first-output deadline and the shared turn
attempt budget still apply, reserving a generation attempt when that budget is
enabled. A concurrent cooldown extension cancels the retry. Portable requests
retain normal failover; opaque context never becomes portable, and neither
inference nor uncertain transport failures are replayed by this upload recovery.

# In-band upstream failures

- Recognize error, response.failed and response.cancelled as failed terminals.
  Explicit error status takes precedence over known error codes and types;
  unknown identifiers use a safe 502 fallback. Never forward upstream free text.
- Buffered client responses return the classified HTTP status. Started streams
  retain their HTTP status and carry the classification in the error event.
  Ops records keep the actual upstream HTTP status separate from the semantic
  failure status; existing compact keepalives still finish with an SSE failure.
- A rate-limit terminal cools only the BPS route for subsequent requests, except
  observation-only quality probes. It never retries the accepted generation,
  disables an account on an in-band 403, or changes shared Codex account health.
- Failure/cancellation retains usage and withholds unvalidated tools. Readers
  stop at cancellation without waiting for EOF, including correction and image
  compaction readers. This does not add a BPS WebSocket transport.

Current resource recommendations and cross-protocol limits: [gateway matrix](../../../../docs/GATEWAY_PROTOCOL_LIMITS.md). Upload deadlines are 120 seconds per image and 180 seconds per phase. Concurrency, image count and pixel guards remain unchanged.

# Complete historical operations and current tool catalogs

A complete original client call followed by its recorded result may prove a past
operation even when that tool has left the current catalog or its schema changed.
Keep native identity, pairing and duplicate checks. Output-first, output-only,
conflicting or incomplete histories supply no new operation proof. This does not
authorize historical tools for a new call: corrected output still passes today's
catalog and schema, cannot reuse a past identity or operation, and remains within
the existing single withheld-call correction and visibility limits. Fixed blocker
diagnostics reuse the same eligibility decision; no history values are recorded.

# Client tool choice and same-account native routing

The BPS wire still accepts only auto/none. The gateway can use its existing
pre-send native capability path for required, named function/custom choices and
allowed_tools(auto/required) only with complete explicit client-only declarations
and unchanged native normalization. This does not emulate forcing inside BPS.
Mixed hosted tools, dynamic/inherited-only declarations, invalid selections and
native-incompatible namespace choices remain rejected. The native grant, owner,
probe and cancellation checks still apply; errors identify tool_choice.
