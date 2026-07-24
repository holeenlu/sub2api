# Sub2API Engineering Contract

This repository follows an architecture-maintenance workflow. The objective is not only to deliver requested behavior, but to keep the system coherent, simple, testable, and able to absorb upstream changes.

## Before changing code

Classify the change:

- **S**: copy, styling, or deterministic local fix. Implement directly with focused verification.
- **M**: cross-module feature, setting, API, or shared behavior. Inspect existing capability, state ownership, and duplicate paths first.
- **L**: workflow, database, retry, billing, authentication, permissions, concurrency, or routing. Complete an architecture gate before implementation and add/update an ADR when the decision is durable.

For M/L work, answer:

1. What user outcome and success condition are required?
2. Why can the existing flow not carry it as-is?
3. Which module uniquely owns each affected state?
4. Does the proposal add a second state source, configuration system, or workflow?
5. What existing code can be reused, unified, or deleted?
6. How do failure, cancellation, retry, rollback, migration, and compatibility work?
7. How many independent locations must change for one business rule?

Give one product decision before implementation: implement directly, refactor first, merge into an existing feature, do not implement, or request a product decision.

## Required architecture sources

Read the relevant documents before M/L work:

- `docs/architecture/overview.md`
- `docs/architecture/invariants.md`
- `docs/architecture/request-lifecycle.md`
- relevant files in `docs/adr/`
- `docs/architecture-health.md`

For API Key work, also read `docs/architecture/api-key-principal.md`.

If documents disagree with code and tests, surface the drift and update the documents in the same change.

## Non-negotiable rules

- New state must have one authoritative owner.
- Do not create a new page, API, configuration path, or workflow until the existing path has been evaluated.
- API Key, User, Group, Channel, and Account are separate identities; do not collapse their responsibilities.
- Billing, refund, retry, and quota changes require contract tests and mutation-style proof for critical branches.
- Do not copy authentication, quota, concurrency, or usage attribution logic into each protocol handler. Use shared seams.
- A compatibility layer must have an owner, removal trigger, and test.
- Remove superseded helpers, branches, tests, and documentation when replacement is complete.
- Avoid broad formatting or unrelated renames in customization commits.
- Generated files are changed only through their generator and only when the core model genuinely requires it.
- Prefer concept integrity over avoiding textual merge conflicts. A `custom_*` sidecar is not automatically safer if it creates a second source of truth.

## Upstream and local customization

- `upstream` is `Wei-Shaw/sub2api` and is fetch-only.
- `origin` is `holeenlu/sub2api` and receives local customization.
- Keep local changes in small, atomic commits and record unavoidable official touchpoints.
- Use temporary `sync/upstream-YYYYMMDD` branches to merge upstream into `origin/main`.
- Do not rewrite published customization history during routine upstream sync.

See `docs/architecture/upstream-customization-boundary.md`.

## Verification

For documentation/governance-only changes:

```bash
./scripts/verify-architecture.sh --quick
```

For backend/frontend behavior changes:

```bash
./scripts/verify-architecture.sh --full
```

Run focused tests during iteration. Before delivery, report exact commands run and any skipped checks with reasons.

## Delivery format for M/L changes

Report:

1. **Product decision**
2. **Architecture impact**
3. **Code change** — added, reused, changed, and deleted
4. **Health change** — concepts added/removed, compatibility debt, change amplification
5. **Verification**

Escalate before a broad rewrite, destructive migration, public API break, or material authentication/billing redesign.
