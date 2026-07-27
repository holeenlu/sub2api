#!/usr/bin/env bash
# Local Docker deployment helper for a checked-out Sub2API repository.
#
# Usage:
#   ./deploy/local-deploy.sh init       # create .env and persistent directories
#   ./deploy/local-deploy.sh build      # build the current source tree
#   ./deploy/local-deploy.sh up         # build (if needed) and start the stack
#   ./deploy/local-deploy.sh status
#   ./deploy/local-deploy.sh logs
#   ./deploy/local-deploy.sh check
#   ./deploy/local-deploy.sh down

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.local.yml"
ENV_FILE="${SCRIPT_DIR}/.env"
IMAGE_NAME="sub2api:latest"
DOCKER_BIN=""

cd "${REPO_ROOT}"

die() {
    printf 'ERROR: %s\n' "$1" >&2
    exit 1
}

require_command() {
    if [[ "$1" == "docker" ]]; then
        if command -v docker >/dev/null 2>&1; then
            DOCKER_BIN="$(command -v docker)"
        elif [[ -x "/Applications/Docker.app/Contents/Resources/bin/docker" ]]; then
            DOCKER_BIN="/Applications/Docker.app/Contents/Resources/bin/docker"
        else
            die "未找到 Docker CLI。请启动 Docker Desktop，或将其 CLI 加入 PATH"
        fi
        # Docker Desktop stores its credential helper beside the CLI. This is
        # needed when the CLI is discovered by absolute path rather than PATH.
        local docker_bin_dir
        docker_bin_dir="$(dirname "${DOCKER_BIN}")"
        case ":${PATH}:" in
            *":${docker_bin_dir}:"*) ;;
            *) export PATH="${docker_bin_dir}:${PATH}" ;;
        esac
        return
    fi
    command -v "$1" >/dev/null 2>&1 || die "未找到命令: $1"
}

compose() {
    "${DOCKER_BIN}" compose --env-file "${ENV_FILE}" -f "${COMPOSE_FILE}" "$@"
}

configured_image() {
    if [[ -f "${ENV_FILE}" ]]; then
        awk -F= '$1 == "SUB2API_IMAGE" { print $2; exit }' "${ENV_FILE}"
    fi
}

init() {
    if [[ ! -f "${ENV_FILE}" ]]; then
        cp "${SCRIPT_DIR}/.env.example" "${ENV_FILE}"
        if command -v openssl >/dev/null 2>&1; then
            sed -i.bak \
                -e "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(openssl rand -hex 32)/" \
                -e "s/^JWT_SECRET=.*/JWT_SECRET=$(openssl rand -hex 32)/" \
                -e "s/^TOTP_ENCRYPTION_KEY=.*/TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32)/" \
                "${ENV_FILE}"
            rm -f "${ENV_FILE}.bak"
        else
            die "初始化需要 openssl 生成安全密钥"
        fi
        chmod 600 "${ENV_FILE}"
        printf '已创建 %s\n' "${ENV_FILE}"
    else
        printf '保留已有 %s\n' "${ENV_FILE}"
    fi
    mkdir -p "${SCRIPT_DIR}/data" "${SCRIPT_DIR}/postgres_data" "${SCRIPT_DIR}/redis_data"
    if [[ -n "${DOCKER_BIN}" ]] || { command -v docker >/dev/null 2>&1; }; then
        require_command docker
        compose config >/dev/null
    else
        printf '提示：当前未安装 Docker，已完成配置初始化；执行 up/build 前请先安装 Docker。\n'
    fi
    printf '本地部署目录已准备完成。\n'
}

build() {
    require_command docker
    [[ -f "${ENV_FILE}" ]] || init
    "${DOCKER_BIN}" build -t "${IMAGE_NAME}" \
        --build-arg GOPROXY=https://goproxy.cn,direct \
        --build-arg GOSUMDB=sum.golang.google.cn \
        -f "${REPO_ROOT}/Dockerfile" "${REPO_ROOT}"
}

up() {
    require_command docker
    [[ -f "${ENV_FILE}" ]] || init
    [[ -d "${SCRIPT_DIR}/postgres_data" ]] || init
    local image
    image="$(configured_image)"
    image="${image:-${IMAGE_NAME}}"
    if [[ "${image}" == "${IMAGE_NAME}" ]] && ! "${DOCKER_BIN}" image inspect "${IMAGE_NAME}" >/dev/null 2>&1; then
        build
    fi
    compose up -d "$@"
    check
}

deploy() {
    require_command docker
    init
    local image
    image="$(configured_image)"
    image="${image:-${IMAGE_NAME}}"
    if [[ "${image}" == "${IMAGE_NAME}" ]]; then
        printf '正在构建当前源码镜像: %s\n' "${IMAGE_NAME}"
        build
    else
        printf '正在更新远程镜像: %s\n' "${image}"
        compose pull sub2api
    fi
    compose up -d --remove-orphans
    check
}

check() {
    local port
    port="${SERVER_PORT:-$(awk -F= '$1 == "SERVER_PORT" { print $2; exit }' "${ENV_FILE}" 2>/dev/null)}"
    port="${port:-8080}"
    local url="http://127.0.0.1:${port}/health"
    if command -v curl >/dev/null 2>&1; then
        local attempt
        for attempt in {1..30}; do
            if curl --fail --silent --show-error --max-time 10 "${url}" >/dev/null 2>&1; then
                printf '健康检查通过: %s\n' "${url}"
                return
            fi
            sleep 2
        done
        require_command docker
        compose ps -a
        die "健康检查失败，请查看: $0 logs"
    else
        printf '未找到 curl，跳过 HTTP 健康检查。\n'
    fi
}

usage() {
    printf '%s\n' \
        '用法: ./deploy/local-deploy.sh <命令>' \
        '不带命令时执行完整部署：初始化、更新/构建、启动、健康检查' \
        '' \
        '命令:' \
        '  deploy     完整部署（默认命令）' \
        '  init       创建 .env、生成密钥并创建持久化目录' \
        '  build      用当前源码构建 sub2api:latest' \
        '  up         启动服务（可附加 compose 参数）' \
        '  down       停止服务（保留数据）' \
        '  restart    重启服务' \
        '  status     查看服务状态' \
        '  logs       查看应用日志（可附加 compose 参数）' \
        '  check      执行 HTTP 健康检查'
}

command="${1:-}"
shift || true

case "${command}" in
    deploy) deploy "$@" ;;
    init) init "$@" ;;
    build) build "$@" ;;
    up) up "$@" ;;
    down) require_command docker; compose down "$@" ;;
    restart) require_command docker; compose restart "$@" ;;
    status) require_command docker; compose ps "$@" ;;
    logs) require_command docker; compose logs -f sub2api "$@" ;;
    check) check "$@" ;;
    -h|--help|help) usage ;;
    '') deploy "$@" ;;
    *) usage; die "未知命令: ${command}" ;;
esac
