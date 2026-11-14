# Application state ownership

app.ts owns host UI state, notifications and read-only build information. The public-settings snapshot/request mechanism is shared/config/publicSettings.ts; app.ts composes it with the site API, injected configuration, branding callbacks and application disposal. adminSettings.ts owns the scoped administrative navigation/settings projection, cancels superseded reads and rejects stale identity results.

Business stores remain in their domains:

- Identity owns authentication, profile/security and administrator compliance.
- Site owns announcements, acknowledgement state and popup queues.
- Subscriptions owns active subscription state and polling.
- Supply owns supplier identity and business scope.
- Payments owns page/query command state; there is no global replacement payment store.

The root application binds scopes and lifecycle through domain public contracts. A cache or in-flight request must not survive a different identity or business scope. Technical UI feedback is provided once inside the application's effect scope. Application unmount disposes that scope and active private views.

Import host state from its concrete module and business state from its domain. Do not recreate a root store registry or a service locator. Cross-domain user directory, settings and resource workflows belong in app/compositions.

Detailed design and language standards remain authoritative in [FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md) and [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md). This document records ownership rather than acceptance: Docker and page-by-page validation are `not_run`; full frontend acceptance remains incomplete.
