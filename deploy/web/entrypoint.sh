#!/bin/sh
set -euf
fail() { printf 'web: %s\n' "$1" >&2; exit 1; }
validate_origin() {
    name=$1
    value=$2
    [ "$(printf '%s' "$value" | tr -d '\r\n')" = "$value" ] || fail "$name must be a single-line origin"
    case "$value" in http://*|https://*) ;; *) fail "$name must be an HTTP(S) origin" ;; esac
    authority=${value#*://}
    printf '%s\n' "$authority" | grep -Eq '^([A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?)(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$' \
        || fail "$name must contain only a hostname/IP and optional port"
    case "$authority" in \[*\]) port= ;; *:*) port=${authority##*:} ;; *) port= ;; esac
    if [ -n "$port" ]; then
        [ "$port" -ge 1 ] && [ "$port" -le 65535 ] || fail "$name has an invalid port"
    fi
}
: "${WEB_PUBLIC_ORIGIN:?Set the public main site origin}"
: "${BACKEND_ORIGIN:?Set the API origin}"
WEB_PUBLIC_ORIGIN=${WEB_PUBLIC_ORIGIN%/}
BACKEND_ORIGIN=${BACKEND_ORIGIN%/}
validate_origin WEB_PUBLIC_ORIGIN "$WEB_PUBLIC_ORIGIN"
validate_origin BACKEND_ORIGIN "$BACKEND_ORIGIN"
WEB_SCHEME=${WEB_PUBLIC_ORIGIN%%://*}
WEB_AUTHORITY=${WEB_PUBLIC_ORIGIN#*://}
case "$WEB_AUTHORITY" in \[*) WEB_HOST="${WEB_AUTHORITY%%]*}]" ;; *) WEB_HOST=${WEB_AUTHORITY%%:*} ;; esac
backend_authority=${BACKEND_ORIGIN#*://}
case "$backend_authority" in
    \[*) BACKEND_TLS_NAME=${backend_authority#\[}; BACKEND_TLS_NAME=${BACKEND_TLS_NAME%%]*} ;;
    *) BACKEND_TLS_NAME=${backend_authority%%:*} ;;
esac
mkdir -p /tmp/web/client /tmp/web/proxy
: > /tmp/web/trusted-proxies.conf
for cidr in ${WEB_TRUSTED_PROXY_CIDRS:-}; do
    printf '%s\n' "$cidr" | grep -Eq '^[0-9A-Fa-f:.]+(/[0-9]{1,3})?$' \
        || fail 'WEB_TRUSTED_PROXY_CIDRS must be space-separated IPs/CIDRs'
    printf 'set_real_ip_from %s;\n' "$cidr" >> /tmp/web/trusted-proxies.conf
done
WEB_RESOLVERS=$(awk '/^nameserver[ \t]+/ { if ($2 ~ /^[0-9A-Fa-f:.]+$/) { if ($2 ~ /:/) printf "[%s] ", $2; else printf "%s ", $2 } }' /etc/resolv.conf)
[ -n "$WEB_RESOLVERS" ] || fail 'No DNS resolver found in /etc/resolv.conf'
export BACKEND_ORIGIN WEB_HOST WEB_AUTHORITY WEB_SCHEME BACKEND_TLS_NAME WEB_RESOLVERS
envsubst '${BACKEND_ORIGIN} ${WEB_HOST} ${WEB_AUTHORITY} ${WEB_SCHEME} ${BACKEND_TLS_NAME} ${WEB_RESOLVERS}' \
    < /etc/nginx/web.conf.template > /tmp/web/nginx.conf
nginx -t -c /tmp/web/nginx.conf
exec "$@"
