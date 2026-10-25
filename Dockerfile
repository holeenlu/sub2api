# syntax=docker/dockerfile:1.7
# API-only build from the repository root; intentionally no embed build tag.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=
ARG COMMIT=unknown
ARG DATE=unknown
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}

RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -mod=readonly -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE} -X main.BuildType=source" \
    -o /out/sub2api ./cmd/server

# Keep the existing PostgreSQL backup/restore tools and runtime versions.
FROM postgres:18-alpine AS pg-client
FROM alpine:3.21 AS runtime
LABEL org.opencontainers.image.title="sub2api API" \
      org.opencontainers.image.source="https://github.com/holeenlu/sub2api"

RUN apk add --no-cache ca-certificates tzdata su-exec libpq zstd-libs \
    lz4-libs krb5-libs libldap libedit
COPY --from=pg-client /usr/local/bin/pg_dump /usr/local/bin/pg_dump
COPY --from=pg-client /usr/local/bin/psql /usr/local/bin/psql
COPY --from=pg-client /usr/local/lib/libpq.so.5* /usr/local/lib/

# This new API role uses its own neutral runtime identity and entrypoint.
RUN addgroup -g 1000 sub2api && adduser -u 1000 -G sub2api -s /bin/sh -D sub2api
WORKDIR /app
COPY --from=builder --chown=sub2api:sub2api --chmod=755 /out/sub2api /app/sub2api
COPY --from=builder --chown=sub2api:sub2api /src/backend/resources /app/resources
COPY LICENSE /app/LICENSE
RUN mkdir -p /app/data && chown sub2api:sub2api /app /app/data
COPY --chmod=755 deploy/api/docker-entrypoint.sh /app/docker-entrypoint.sh
ENV DATA_DIR=/app/data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=3 \
    CMD wget -q -T 5 -O /dev/null http://127.0.0.1:${SERVER_PORT:-8080}/health || exit 1
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/sub2api"]
