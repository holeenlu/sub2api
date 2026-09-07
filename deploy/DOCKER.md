# TapModels Docker Image

TapModels lets developers reach multiple AI models through one unified API.

## Quick Start

```bash
docker run -d \
  --name tapmodels \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/tapmodels" \
  -e REDIS_URL="redis://host:6379" \
  ghcr.io/erwinlin/tapmodels:latest
```

## Docker Compose

```yaml
version: '3.8'

services:
  tapmodels:
    image: ghcr.io/erwinlin/tapmodels:latest
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/tapmodels?sslmode=disable
      - REDIS_URL=redis://redis:6379
    depends_on:
      - db
      - redis

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=postgres
      - POSTGRES_DB=tapmodels
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## Startup and Database Recovery

TapModels runs database migrations while starting. PostgreSQL may still be
recovering briefly after a host or Docker daemon restart. The application
retries transient PostgreSQL startup and connection errors with bounded
exponential backoff, then continues startup when the database is ready.
Permanent errors such as invalid credentials, migration checksum mismatches,
SQL errors, and incompatible data fail immediately.

The Compose deployment also checks PostgreSQL readiness with both `pg_isready`
and a simple SQL query. `depends_on: condition: service_healthy` helps order a
fresh Compose start, but application-level retries are still required when
Docker restores existing containers after a host restart.

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `PORT` | Server port | No | `8080` |
| `GIN_MODE` | Gin framework mode (`debug`/`release`) | No | `release` |

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `x.y.z` - Specific version
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

The Compose files under `deploy/` read the image from `TAPMODELS_IMAGE` (default `ghcr.io/erwinlin/tapmodels:latest`), so pin a tag or digest there instead of editing the Compose file.

## Links

- [Website](https://tapmodels.ai)
- [Documentation](https://docs.tapmodels.ai)
