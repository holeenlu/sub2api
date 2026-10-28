# syntax=docker/dockerfile:1.7
# Both independent UI roles use this source build. The root Dockerfile is API-only.
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
COPY backend/internal/pkg/claude/prompts/catalog.json /app/backend/internal/pkg/claude/prompts/catalog.json
COPY backend/internal/service/scheduling_quota_defaults.json /app/backend/internal/service/scheduling_quota_defaults.json
# Run the existing production checks once, then generate both role artifacts.
# Explicit outDir keeps UI output independent of the API source tree.
RUN pnpm run check:i18n && pnpm exec vue-tsc -b \
    && VITE_API_BASE_URL=/api/v1 pnpm exec vite build --outDir dist \
    && VITE_API_BASE_URL=/api/v1 VITE_VENDOR_PORTAL=true pnpm exec vite build --outDir dist-vendor

FROM ${NGINX_IMAGE} AS runtime
# Mutable provenance belongs after dependency/build layers, never before install.
ARG VERSION=dev
ARG COMMIT=unknown
ARG SOURCE_SHA256=unknown
RUN printf '%s' "$VERSION" | grep -Eq '^[0-9A-Za-z.-]+$' \
    && printf '%s' "$COMMIT" | grep -Eq '^[0-9A-Za-z.-]+$' \
    && printf '%s' "$SOURCE_SHA256" | grep -Eq '^[0-9A-Za-z.-]+$' \
    && printf '{"version":"%s","commit":"%s","source_sha256":"%s"}\n' "$VERSION" "$COMMIT" "$SOURCE_SHA256" > /etc/nginx/ui-build.json \
    && rm -rf /usr/share/nginx/html/*
LABEL org.opencontainers.image.title="sub2api UI" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      io.sub2api.source.sha256="$SOURCE_SHA256"
COPY LICENSE /usr/share/licenses/sub2api/LICENSE
USER 101:101
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health

FROM runtime AS main
COPY deploy/web/nginx.conf.template /etc/nginx/web.conf.template
COPY --chmod=755 deploy/web/entrypoint.sh /usr/local/bin/web-entrypoint
COPY --from=frontend-builder /app/frontend/dist/ /usr/share/nginx/html/
ENTRYPOINT ["/usr/local/bin/web-entrypoint"]
CMD ["nginx", "-c", "/tmp/web/nginx.conf", "-g", "daemon off;"]

FROM runtime AS vendor
COPY deploy/vendor-portal/nginx.conf.template /etc/nginx/vendor-portal.conf.template
COPY --chmod=755 deploy/vendor-portal/entrypoint.sh /usr/local/bin/vendor-portal-entrypoint
COPY --from=frontend-builder /app/frontend/dist-vendor/ /usr/share/nginx/html/
ENTRYPOINT ["/usr/local/bin/vendor-portal-entrypoint"]
CMD ["nginx", "-c", "/tmp/vendor-portal/nginx.conf", "-g", "daemon off;"]
