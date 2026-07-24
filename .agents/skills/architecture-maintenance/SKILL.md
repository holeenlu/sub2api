---
name: architecture-maintenance
description: Maintain long-term software architecture health while planning, implementing, reviewing, or refactoring changes. Use for cross-module features, new workflows, database changes, authentication, permissions, billing, retries, concurrency, routing, deployment architecture, major refactors, or whenever a request may add state, configuration, APIs, pages, or parallel execution paths.
---

# Architecture Maintenance

Treat every requested feature as a hypothesis to validate, not an implementation command.

## Classify the change

- **S**: copy, styling, deterministic local fix. Implement directly with focused tests.
- **M**: cross-module feature, setting, API, or shared behavior. Inspect existing capability, state ownership, and duplicate paths first.
- **L**: workflow, database, retry, billing, authentication, permissions, concurrency, or routing. Produce an architecture decision before implementation; add an ADR when the decision has lasting consequences.

## Run the architecture gate

Before an M/L change:

1. State the user outcome and measurable success condition.
2. Trace the current end-to-end path: data, domain, service, API, UI, cache, failure, migration, and tests.
3. Identify the unique owner of every affected state.
4. Compare at least: no-build/configuration, extension of an existing flow, minimal addition, and refactor-first.
5. Check for a second state source, parallel configuration, duplicate workflow, or change amplification across independent locations.
6. Identify obsolete code that the change can remove.
7. Record migration, rollback, compatibility, and removal conditions.
8. Give one decision: implement directly, refactor first, merge into an existing feature, do not implement, or request a product decision.

Do not optimize for line count or abstraction alone. Prefer the smallest set of stable concepts and the fewest correct change locations.

## Work by touchpoint

Improve the path being changed to a healthy state. Do not launch unrelated repository-wide rewrites. When a third copy of the same business rule would be introduced, stop and establish a single owner first.

Allow deletion of superseded handlers, helpers, flags, compatibility branches, tests, and documentation in the same delivery. Never leave a temporary compatibility layer without an owner and removal trigger.

## Preserve critical invariants

Read the nearest project instructions and architecture documents before acting. For this repository, read:

- `AGENTS.md` for mandatory project rules and verification commands;
- `docs/architecture/overview.md` for module ownership;
- `docs/architecture/invariants.md` for non-negotiable contracts;
- the relevant file in `docs/adr/` for established decisions;
- `docs/architecture-health.md` for accepted debt and triggers.

If these sources disagree with code, treat code and tests as evidence of current behavior, surface the drift, and update the documentation as part of the change.

## Complete the change

An M/L change is complete only when:

- behavior is correct, including failure, cancellation, retry, and rollback;
- no duplicate state owner or parallel configuration remains;
- module responsibilities remain explicit;
- migrations and compatibility have removal or compensation paths;
- tests protect business contracts rather than implementation details;
- superseded code and stale documentation are removed;
- architecture documents, ADRs, and health notes are updated when affected;
- the relevant project verification commands have run.

## Report in a fixed format

For M/L work, report:

1. **Product decision** — implement, adjust, merge, reject, or request a decision.
2. **Architecture impact** — ownership, boundaries, and data-flow changes.
3. **Code change** — what was added, reused, changed, and deleted.
4. **Health change** — concepts added or removed, compatibility debt, and change amplification.
5. **Verification** — commands run, results, and anything not verified.

Escalate before a broad rewrite, destructive migration, public API break, or material authentication/billing redesign. Include evidence, alternatives, migration, rollback, and risk.
