#!/usr/bin/env bash
set -euo pipefail
: "${RELEASE_VERSION:?}" "${RELEASE_SHA:?}" "${GITHUB_REPOSITORY:?}" "${RUNNER_TEMP:?}"
if [[ -n ${RELEASE_PORTAL_IMAGE:-} ]]; then
  test -s frontend/dist-vendor/index.html || { echo 'Missing matching supplier frontend artifact' >&2; exit 1; }
fi
owner=${GITHUB_REPOSITORY%%/*}
owner_lower=$(printf '%s' "$owner" | tr '[:upper:]' '[:lower:]')
registries=("${RELEASE_IMAGE:-ghcr.io/$owner_lower/sub2api}")
if [[ ${SIMPLE_RELEASE:-false} != true && ${DOCKERHUB_USERNAME:-skip} != skip ]]; then
  registries+=("${DOCKERHUB_USERNAME}/sub2api")
fi
arches=(amd64 arm64)
if [[ ${SIMPLE_RELEASE:-false} == true ]]; then arches=(amd64); fi
for arch in "${arches[@]}"; do
  args=(--platform "linux/$arch" --file ".release-context/$arch/Dockerfile"
    --label "org.opencontainers.image.version=$RELEASE_VERSION"
    --label "org.opencontainers.image.revision=$RELEASE_SHA"
    --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY")
  if [[ -n ${RELEASE_CHANNEL:-} ]]; then
    # Brand Dockerfiles retain their installed name and compatibility symlink,
    # while automatic-release archives always supply the sub2api executable.
    args+=(--build-arg BINARY_NAME=sub2api)
  fi
  for registry in "${registries[@]}"; do
    args+=(--tag "$registry:$RELEASE_VERSION-$arch")
    if [[ ${SIMPLE_RELEASE:-false} == true ]]; then
      args+=(--tag "$registry:$RELEASE_VERSION")
      if [[ ${DEFER_PROMOTION:-false} != true ]]; then args+=(--tag "$registry:latest"); fi
    fi
  done
  if [[ ${DRY_RUN:-false} == true ]]; then
    args+=(--output "type=oci,dest=$RUNNER_TEMP/sub2api-$arch.oci.tar")
  else
    args+=(--push)
  fi
  docker buildx build "${args[@]}" ".release-context/$arch"
  if [[ -n ${RELEASE_PORTAL_IMAGE:-} ]]; then
    portal_args=(--platform "linux/$arch" --file deploy/vendor-portal/Dockerfile --target release
      --label "org.opencontainers.image.version=$RELEASE_VERSION"
      --label "org.opencontainers.image.revision=$RELEASE_SHA"
      --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY"
      --tag "$RELEASE_PORTAL_IMAGE:$RELEASE_VERSION-$arch")
    if [[ ${SIMPLE_RELEASE:-false} == true ]]; then
      portal_args+=(--tag "$RELEASE_PORTAL_IMAGE:$RELEASE_VERSION")
      if [[ ${DEFER_PROMOTION:-false} != true ]]; then portal_args+=(--tag "$RELEASE_PORTAL_IMAGE:latest"); fi
    fi
    if [[ ${DRY_RUN:-false} == true ]]; then
      portal_args+=(--output "type=oci,dest=$RUNNER_TEMP/vendor-portal-$arch.oci.tar")
    else
      portal_args+=(--push)
    fi
    docker buildx build "${portal_args[@]}" .
  fi
done
if [[ ${DRY_RUN:-false} != true && ${SIMPLE_RELEASE:-false} != true ]]; then
  major=${RELEASE_VERSION%%.*}
  minor=${RELEASE_VERSION#*.}; minor=${minor%%.*}
  for registry in "${registries[@]}"; do
    tags=(--tag "$registry:$RELEASE_VERSION")
    if [[ ${DEFER_PROMOTION:-false} != true ]]; then
      tags+=(--tag "$registry:latest" --tag "$registry:$major.$minor" --tag "$registry:$major")
    fi
    docker buildx imagetools create "${tags[@]}" \
      "$registry:$RELEASE_VERSION-amd64" "$registry:$RELEASE_VERSION-arm64"
  done
  if [[ -n ${RELEASE_PORTAL_IMAGE:-} ]]; then
    tags=(--tag "$RELEASE_PORTAL_IMAGE:$RELEASE_VERSION")
    if [[ ${DEFER_PROMOTION:-false} != true ]]; then tags+=(--tag "$RELEASE_PORTAL_IMAGE:latest"); fi
    docker buildx imagetools create "${tags[@]}" \
      "$RELEASE_PORTAL_IMAGE:$RELEASE_VERSION-amd64" "$RELEASE_PORTAL_IMAGE:$RELEASE_VERSION-arm64"
  fi
fi
