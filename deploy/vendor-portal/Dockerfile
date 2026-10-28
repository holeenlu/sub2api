# syntax=docker/dockerfile:1.7
# Independent supplier UI; build from the repository root. No Go server or DB.
ARG NODE_IMAGE=node:24-alpine
ARG NGINX_IMAGE=nginx:1.30.6-alpine
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS frontend-builder
WORKDIR /app/frontend
ENV NODE_OPTIONS=--max-old-space-size=4096
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN --mount=type=cache,id=vendor-portal-pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --prefer-offline
COPY frontend/ ./
COPY docs/legal/ /app/docs/legal/
RUN pnpm run build:vendor

FROM ${NGINX_IMAGE} AS runtime
ARG RELEASE_VERSION=dev
ARG RELEASE_SHA=unknown
RUN printf '%s' "$RELEASE_VERSION" | grep -Eq '^[0-9A-Za-z.-]+$' \
    && printf '%s' "$RELEASE_SHA" | grep -Eq '^[0-9A-Za-z.-]+$' \
    && printf '{"version":"%s","commit":"%s"}\n' "$RELEASE_VERSION" "$RELEASE_SHA" > /etc/nginx/portal-build.json
COPY deploy/vendor-portal/nginx.conf.template /etc/nginx/vendor-portal.conf.template
COPY --chmod=755 deploy/vendor-portal/entrypoint.sh /usr/local/bin/vendor-portal-entrypoint
USER 101:101
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health
ENTRYPOINT ["/usr/local/bin/vendor-portal-entrypoint"]
CMD ["nginx", "-c", "/tmp/vendor-portal/nginx.conf", "-g", "daemon off;"]

# CI supplies the matching version's already-built vendor artifact here.
FROM runtime AS release
COPY frontend/dist-vendor/ /usr/share/nginx/html/

FROM runtime AS standalone
COPY --from=frontend-builder /app/frontend/dist-vendor/ /usr/share/nginx/html/
