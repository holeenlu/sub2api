# SIWC adaptation — 2026-10-21

Shared KDAN/TapModels/Tokensavy capability. Source: ranxi2001/sub2api production 472e0db09201ad3f950a512fe99812bdd3686449, PR485 e48c0a5b0 and PR491 be417ffa3. Initial base c5d1b1a64; integration base refreshed to main 186a7a2e6 after the concurrent scheduling delivery. User authorized implementation, repair, HTTP/SSE review and client WebSocket integration; this turn does not separately request publication or deployment.

## Execution
1. Add explicit SIWC registration and signed identity, durable independent grant and safe refresh/revocation.
2. Reuse Responses HTTP/SSE, conversion and accounting; fetch models with the same SIWC credential.
3. Reuse the client WS session owner and bridge parser; permit persistent upstream SIWC WS under the existing WS settings, retaining complete-history HTTP mode and no-replay rules.
4. Verify combined behavior, existing gateway regressions, frontend and migrations; retain evidence and exact limitations.

## Source/local decisions
| Behavior | Source | Local adoption |
| --- | --- | --- |
| Dynamic registration | Retains issued client only after successful exchange | Persist pending registration before exchange; retry with issued client; verify ID-token issuer/sub/audience/nonce; opaque access tokens |
| Credential storage | Reauth Worker storage and CAS | Existing OpenAIOAuthService/private encrypted account storage, shared authz-bound OAuth sessions; no Worker dependency |
| Identity | Requires Codex chatgpt_account_id in access token | Independent signed SIWC issuer/sub/issued client; no inferred workspace from email or opaque metadata |
| Public Responses | HTTP/SSE, fixed official endpoint | Reuse native budgets/parser/metering; preserve real HTTP status and SIWC semantic error codes, no silent billing/protocol fallback |
| Models | Codex manifest using primary token | Official /v1/models with SIWC bearer; registration-sensitive cache, authoritative empty list and group projection |
| Client WS | Disabled | Existing per-connection owner and stream parser; persistent public SIWC WS when enabled, with proven same-connection incremental frames; configured HTTP mode expands owned history and rejects unknown previous IDs |
| Lost HTTP context | Deletes previous_response_id | Reject unresolved references before sending; WS bridge must expand its owned history first |
| Automatic login | Password/TOTP Worker | Excluded; manual browser authorization with optional local loopback helper only |
| Supplier authorization | No equivalent local scope | Preserve current explicit native/bps supplier policies; SIWC initially administrator-owned ordinary OAuth parents only |
| Protocol selection | Independent boolean | Explicit opt-in extra.openai_siwc, no automatic selection after binding; Codex/BPS selections/grants retained |

No retired tickets, Mihomo, Prism, automatic quality operations or credential workers are restored. No existing applied migration is changed. Historical usage remains immutable; new SIWC sends need their own measured authorization label.

## Validation
- Full backend unit suite: go test -tags=unit -p=4 ./... passed after integration with main 186a7a2e6. Existing service package passed in 352.372 seconds. Subsequent capability, credential, scheduler and quota isolation fixes passed the final targeted race suite (service 9.874 seconds), go vet and server build.
- SIWC HTTP/SSE, client WS, authentication, models, account tests and middleware race suites passed. Compatibility coverage includes Chat Completions and Anthropic Messages, both streaming and buffered, success and failure; provider status/code/param/request ID/Retry-After are preserved through the adapter.
- PostgreSQL 18.1 and Redis 8.4 integration: full migration harness passed; 16 concurrent CAS writes admit one winner even after native token rotation, stale refresh/callback cannot undo revoke, parent deletion cascades, historical usage remains immutable, and native/bps/siwc modes validate. Six stable parent identity changes reject stale writes; native access token, refresh token and expiry rotation do not discard an independently rotated SIWC grant or overwrite native credentials.
- Frontend: 136 focused cases, vue-tsc, affected ESLint, generated zh-TW, normal and supplier builds passed; local loopback helper: 5 Python tests passed.
- Wire regenerated using cached tooling; backend server build and go vet passed. Depguard: 0 issues. Full golangci-lint could not typecheck the Go 1.27.2 export format (version 5; installed analyser supports 4); this is a tooling limitation, not reported as a lint pass.
- No real OpenAI account authorization, entitlement, live inference or browser end-to-end test was performed. No production migration, release or deployment was executed. Implementation and validation were completed in an isolated worktree; subsequent authorized topic folding and publication are recorded in the delivery report.

## Capability boundaries

| Capability | Implemented behavior |
| --- | --- |
| Responses HTTP stream | Official public POST /v1/responses with SIWC bearer, store=false, stream=true; only response.completed succeeds |
| Responses HTTP buffered | Same upstream SSE, converted after terminal completion; partial usage survives failures |
| Client WebSocket | Existing WS owner/bridge parser and shared connection-pool implementation; private SIWC lease and same-connection incremental prefix proof, or configured full-context HTTP bridge; current lane/event attribution and cancellation |
| Web search | web_search inside Responses, subject to upstream model/account policy |
| Vision/file inputs | Preserve model-supported Responses input_image/input_file; HTTP and client WS tests retain image content alongside web_search; no Files upload API added |
| Standalone endpoints | Existing capability filters exclude SIWC from alpha/search, images/generations, images/edits, embeddings, Live and compact; mixed pools can select a compatible ordinary account. Direct search/image/embedding sends also reject before credential acquisition |
| Tools | Function/custom via additional_tools, existing namespaces preserved; unsupported image_generation, file_search, Code Interpreter, hosted MCP and native computer-use tools rejected before sending |
| Remote history | Reject unresolved previous_response_id, conversation and prompt references; never silently discard referenced context |
| Model catalog | Same SIWC grant, official models array, visibility=list and server order; bounded cache, synchronous stale revalidation, local revoke/identity checks |

## Authorization and operations

- Requires the existing persistent encryption key. SIWC registration and renewable tokens remain separate encrypted records. Explicit super-admin account backup version 2 exports them in `siwc_authorization`, separately from native credentials, and re-encrypts them with the destination key on import. The primary-identity fingerprint excludes the local database account ID and rotating tokens; provider registration and independent issuer/subject checks remain. No legacy ID-bound reader is retained because SIWC authorization has not yet been put into use. See [backup and migration](../SIWC_UI.md#账号备份与迁移).
- Four management method/path entries under /api/v1/admin/accounts/:id/siwc; all require accounts.authorize, exclude supplier scope and retain authority leases through callback.
- One new account setting, extra.openai_siwc, defaults off. Binding does not select the protocol; selecting SIWC does not delete native/BPS settings. Invalid or missing SIWC authorization fails closed.
- One new inference protocol branch uses existing HTTP/SSE machinery; client WS reuses the existing coordinator/parser and can use a private SIWC WebSocket lease. One new model-catalog source. No new background worker, network listener in the server, automatic login, external service, package dependency or deployment configuration.
- Migration 284 adds two SIWC tables and expands the existing authorization_mode constraint. It does not rewrite migration 279, old usage rows or native/Excel credentials. Production migration is separate from local validation.
- Disconnect invalidates pending callbacks even if no grant has yet been stored, and keeps issued client/host registration. Remote revoke confirmation is distinct from local disconnection.
- HTTP and semantic stream errors are distinguished; SIWC plan/auth failures do not invalidate Codex/BPS grants or silently change billing paths. Accepted generation is not automatically replayed.
- Credential preparation keeps SIWC failures outside generic account failover. Scheduler recovery exclusions and health samples use an independent SIWC mode; native/BPS mode hints cannot reopen an exhausted SIWC target. Client WS keeps actual HTTP or WebSocket handshake status separate from semantic stream status, with request ID and retry metadata in existing diagnostics.
- Scheduler metadata retains the explicit SIWC selection without exposing its credentials. SIWC request admission ignores native Codex quota observations, automatic quota pauses, quota-driven concurrency reduction and native prefix-cache evidence; explicit account state, concurrency, RPM and local user/API Key controls remain applicable.
- Callback request bodies are omitted from audit storage. The optional local loopback helper accepts its authorization URL through hidden input and never exchanges/stores tokens.
- earliest_refresh_at is retained verbatim. The currently published reference does not define its encoding or scheduling algorithm; implementation follows documented expiry/serialized refresh instead of guessing units.

## Source and verification references

- ranxi2001/sub2api PR485 e48c0a5b02d26820fcbc6c9f5ead74dd4cad4934 and PR491 be417ffa3, audited through 472e0db09201ad3f950a512fe99812bdd3686449.
- https://developers.openai.com/siwc/token-sharing-open-source/sign-in
- https://developers.openai.com/siwc/token-sharing-open-source/token-reference
- https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions
- https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference
- https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery
- https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations

Official technical references were checked on 2026-10-21. Real-account entitlement and live inference remain unverified. Validation evidence is retained in .release/siwc-*.log in the implementation worktree.

## Final change accounting

Compared with integration base 186a7a2e6, including all newly added files:

| Category | Files | Added lines | Removed lines | Net lines |
| --- | ---: | ---: | ---: | ---: |
| Business code, UI, translations and local helper | 66 | 3174 | 133 | 3041 |
| Backend/frontend/helper tests | 33 | 3139 | 17 | 3122 |

SQL migration adds 18 lines separately; regenerated dependency injection changes one line in and one out. Documentation is excluded from the business/test counts. Four management method/path endpoints, one opt-in account setting, one Responses inference branch and one model-catalog source are added. Client WS and compatibility protocols reuse existing execution paths. No new background worker or dependency is added.


## Upstream incremental continuation follow-up — 2026-10-21

The original HTTP bridge accepted downstream WebSocket deltas but expanded them into full upstream requests. That was conversation compatibility, not upstream incremental continuation. The current follow-up removes this completion gap for SIWC client WebSocket sessions; it does not claim that direct SIWC HTTP requests can use persistent previous-response storage.

- Existing global WS/OAuth/v2 and account mode settings select the transport. Explicit off, HTTP bridge or forced HTTP settings retain their behavior. No new API, administrator flag, database migration or background worker is added.
- The shared WS pool supplies one private lease per ingress. The target is fixed to the public Responses WebSocket endpoint, with only the SIWC bearer and application User-Agent. SIWC sockets are never reused by another ingress or speculatively prewarmed.
- The bridge still owns tenant/key/account/grant checks, admission, per-turn limits, cancellation, output and accounting. An internal event reader presents WS JSON events to that existing parser; it does not introduce another metering or retry engine.
- Only a fully accepted response.completed with complete output establishes an anchor. Constant-size digests of actual normalized settings and ordered input/output items prove that the next complete history has the expected prefix. A matching request sends previous_response_id and only the suffix. Tools, image inputs, reasoning and numeric precision retain the normalizer's existing contract.
- Token rotation, connection age/closure, settings changes or prefix mismatch remove the incremental shortcut before this turn is written. Rebinding/revocation/identity change stops use of the grant. Already-written generations are not replayed over a new socket or HTTP.
- Success diagnostics retain the actual 101 handshake and websocket protocol. The ingress_ws_siwc_write log reports full_payload_bytes and wire_payload_bytes for uncompressed JSON, incremental and reused. These are not compressed network bytes, cached-token billing or end-user latency measurements.

Validation uses local public-endpoint redirection in tests only; production code has no alternative SIWC endpoint. The socket test captures one connection across three generations, a second-turn function result without resending the first prompt/reasoning, and a full request after instructions change. A matched HTTP fallback runs the same second turn for payload-size comparison. Additional coverage includes forced-HTTP settings, revoke/rebind during dial, token rotation, partial reads, cancellation, stale close callbacks and bare-error usage draining. No real SIWC authorization, paid upstream inference or production deployment is performed by this follow-up.
