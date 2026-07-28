# ADR 0002: Caddy Edge State Belongs to the Active Deployment

Status: Accepted
Date: 2026-07-28

## Context

The active Sub2API Caddy container on the production host inherited volumes and a network from a retired gateway deployment. Its Compose file therefore could not be independently recreated or removed. It also retained a public listener that proxied to a removed backend.

## Decision

The active `sub2api-deploy` Compose project owns Caddy's `/data` and `/config` volumes and connects Caddy only to `sub2api-deploy_sub2api-network`. Existing Caddy state is copied once into the new owned volumes before cutover so ACME accounts and certificates remain intact. The retired listener and network are removed from Caddy's configuration.

The edge proxy preserves the current HTTP-to-HTTPS redirect and HTTPS upstream contract, while adding bounded header/idle timeouts, upstream health checks, conservative compression that excludes SSE, explicit forwarded-header provenance, Caddy health checks, and a file-descriptor limit appropriate for long-lived streaming connections.

## Alternatives considered

### Retain the inherited volumes and network

Rejected. They make the active deployment depend on the retired deployment's resource names and prevent independent lifecycle management.

### Start Caddy with blank storage

Rejected. It risks unnecessary certificate issuance and loses ACME state. The migration copies the existing state instead.

### Continue exposing the old management listener

Rejected. Its backend no longer exists and it expands public attack surface without providing service.

## Consequences

The Caddy service has a brief controlled restart during cutover. Rollback is performed by restoring the previous Compose file and Caddyfile; the retired volumes and network are retained until HTTPS and streaming verification succeeds.

## Migration and rollback

Before cutover, back up the active Caddyfile, Compose file, and storage volumes. Validate the replacement Caddyfile in the pinned image. Copy storage into the new project-owned volumes, recreate only the Caddy container, and verify HTTP redirect, HTTPS, HTTP/3, application health, and streaming behavior. Do not delete retired resources until the new service is healthy.

## Revisit triggers

- Caddy is moved behind a CDN or load balancer, requiring an explicit trusted-proxy policy.
- More than one Caddy replica is introduced.
- Certificate storage is moved from Docker volumes to a managed shared store.
