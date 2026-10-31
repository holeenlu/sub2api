# sub2api

An AI API gateway undergoing an independent architectural rebuild. The technical project name is sub2api. Website branding uses runtime settings; image, container and deployment identity belong to deployment configuration.

Maintenance branch: `NewAPI`. The rebuilt project is maintained only on this branch. `main`, `TapModels` and `tokensavy` remain historical/reference branches and are not synchronization, commit, push or release targets. Upstream changes are reviewed and selectively integrated into `NewAPI`; the old brand release matrix, self-update installer and parallel deployment selectors are not part of this project.

## Current scope

The first implementation wave introduces shared execution accounting, an integrated pure SSE package, centralized project identity and a standalone PostgreSQL/Redis-backed API deployment. The complete execution engine and remaining domains are still being migrated.

Frontend architecture and implementation belong to a separate session. The default image contains the API without an embedded frontend. A complete website deployment requires the frontend handoff; existing frontend files are not evidence that this integration is complete.

## Build

~~~sh
# Build from this branch without Node or another worktree.
docker build -t sub2api:local .

# Alternatively, use a Go SDK matching backend/go.mod.
make build-backend
~~~

Follow [API deployment](deploy/api/README.md) for required secrets, isolated PostgreSQL/Redis storage, naming and shutdown. Upgrade by building and deploying your own artifact. Do not use an existing production database or volume as a rebuild trial.

Website name, logo and public API links use the existing site_name, site_logo and api_base_url settings. Display links do not control the authenticated admin API, provider destinations or update trust. APP_IMAGE, COMPOSE_PROJECT_NAME and optional APP_CONTAINER_NAME control deployment identity.

## Engineering

- [Architecture, contracts and work packages](docs/architecture-rebuild/README.md)
- [Coding standards](docs/architecture-rebuild/CODING_STANDARDS.md)
- [Implementation and verification](docs/architecture-rebuild/IMPLEMENTATION.md)
- [Retirement of replaced implementations](docs/architecture-rebuild/RETIREMENT.md)
- [Frontend handoff](docs/architecture-rebuild/FRONTEND_HANDOFF.md)
- [Delivery and resource cleanup](docs/CHANGE_DELIVERY.md)
- [Protocol and product documentation](docs/README.md)

Replaced implementations are deleted with their integration slice. Modules that still serve active requests remain until their replacement takes over. Git preserves the source history; this project does not carry an extra legacy source tree.

## License and provenance

See [LICENSE](LICENSE) and [source adaptations](docs/SOURCE_ADAPTATIONS.md). Required copyright and license notices are retained. Unrelated advertising and sponsor promotion are excluded from the product; existing payment, subscription and supplier capabilities are reviewed by their actual purpose.

[中文](README_CN.md) · [日本語](README_JA.md)
