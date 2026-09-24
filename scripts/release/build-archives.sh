#!/usr/bin/env bash
set -euo pipefail
: "${VERSION:?}" "${COMMIT:?}" "${DATE:?}" "${CHANNEL:?}" "${UPSTREAM_VERSION:?}"
root=$(pwd)
output="$root/dist/release"
mkdir -p "$output"
if [[ "${ZH_TW:-false}" == true ]]; then
  npm ci --prefix tools/zh-tw --omit=dev
  node tools/zh-tw/convert-go.mjs backend
fi
# Linux packages are used by systemd installations; Compose uses OCI images.
for arch in amd64 arm64; do
  stage=$(mktemp -d)
  trap 'rm -rf "$stage"' EXIT
  (cd backend && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags embed -trimpath \
    -ldflags="-s -w -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.Date=$DATE -X main.BuildType=release -X main.UpstreamVersion=$UPSTREAM_VERSION -X github.com/Wei-Shaw/sub2api/internal/service.ReleaseChannel=$CHANNEL" \
    -o "$stage/$CHANNEL" ./cmd/server)
  if [[ "$CHANNEL" != sub2api ]]; then cp "$stage/$CHANNEL" "$stage/sub2api"; fi
  tar -czf "$output/${CHANNEL}_${VERSION}_linux_${arch}.tar.gz" -C "$stage" .
  rm -rf "$stage"
  trap - EXIT
done
(cd "$output" && sha256sum ./*.tar.gz | sed 's|  ./|  |' > checksums.txt)
