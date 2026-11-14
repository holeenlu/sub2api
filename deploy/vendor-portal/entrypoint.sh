#!/bin/sh
set -euf
fail() { printf 'vendor-portal: %s\n' "$1" >&2; exit 1; }

# Reject paths, credentials and NGINX syntax. Deployment values stay outside JS.
validate_origin() {
    name=$1
    value=$2
    [ "$(printf '%s' "$value" | tr -d '\r\n')" = "$value" ] || fail "$name must be a single-line origin"
    case "$value" in http://*|https://*) ;; *) fail "$name must be an HTTP(S) origin" ;; esac
    authority=${value#*://}
    printf '%s\n' "$authority" | grep -Eq '^([A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?)(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$' \
        || fail "$name must contain only a hostname/IP and optional port"
    case "$authority" in
        \[*\]) port= ;;
        *:*) port=${authority##*:} ;;
        *) port= ;;
    esac
    if [ -n "$port" ]; then
        [ "$port" -ge 1 ] && [ "$port" -le 65535 ] || fail "$name has an invalid port"
    fi
}

: "${PORTAL_PUBLIC_ORIGIN:?Set the public supplier site origin}"
: "${BACKEND_ORIGIN:?Set the existing operations backend origin}"
PORTAL_PUBLIC_ORIGIN=${PORTAL_PUBLIC_ORIGIN%/}
BACKEND_ORIGIN=${BACKEND_ORIGIN%/}
validate_origin PORTAL_PUBLIC_ORIGIN "$PORTAL_PUBLIC_ORIGIN"
validate_origin BACKEND_ORIGIN "$BACKEND_ORIGIN"
PORTAL_SCHEME=${PORTAL_PUBLIC_ORIGIN%%://*}
PORTAL_AUTHORITY=${PORTAL_PUBLIC_ORIGIN#*://}
case "$PORTAL_AUTHORITY" in
    \[*) PORTAL_HOST="${PORTAL_AUTHORITY%%]*}]" ;;
    *) PORTAL_HOST=${PORTAL_AUTHORITY%%:*} ;;
esac
backend_authority=${BACKEND_ORIGIN#*://}
case "$backend_authority" in
    \[*) BACKEND_TLS_NAME=${backend_authority#\[}; BACKEND_TLS_NAME=${BACKEND_TLS_NAME%%]*} ;;
    *) BACKEND_TLS_NAME=${backend_authority%%:*} ;;
esac

mkdir -p /tmp/vendor-portal/client /tmp/vendor-portal/proxy
: > /tmp/vendor-portal/trusted-proxies.conf
for cidr in ${PORTAL_TRUSTED_PROXY_CIDRS:-}; do
    printf '%s\n' "$cidr" | grep -Eq '^[0-9A-Fa-f:.]+(/[0-9]{1,3})?$' \
        || fail 'PORTAL_TRUSTED_PROXY_CIDRS must be space-separated IPs/CIDRs'
    printf 'set_real_ip_from %s;\n' "$cidr" >> /tmp/vendor-portal/trusted-proxies.conf
done

# Runtime DNS permits backend restarts without rebuilding/restarting the portal.
PORTAL_RESOLVERS=$(awk '/^nameserver[ \t]+/ { if ($2 ~ /^[0-9A-Fa-f:.]+$/) { if ($2 ~ /:/) printf "[%s] ", $2; else printf "%s ", $2 } }' /etc/resolv.conf)
[ -n "$PORTAL_RESOLVERS" ] || fail 'No DNS resolver found in /etc/resolv.conf'
export BACKEND_ORIGIN PORTAL_HOST PORTAL_AUTHORITY PORTAL_SCHEME BACKEND_TLS_NAME PORTAL_RESOLVERS
envsubst '${BACKEND_ORIGIN} ${PORTAL_HOST} ${PORTAL_AUTHORITY} ${PORTAL_SCHEME} ${BACKEND_TLS_NAME} ${PORTAL_RESOLVERS}' \
    < /etc/nginx/vendor-portal.conf.template > /tmp/vendor-portal/nginx.conf
nginx -t -c /tmp/vendor-portal/nginx.conf
exec "$@"
