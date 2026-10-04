# Edge and HTTP Ingress Security

Tokensavy supports long-lived SSE and WebSocket requests. Protect the request
ingress without imposing a response `WriteTimeout`: a write deadline would
terminate healthy long generations and streams.

## Application defaults

- `server.max_header_bytes: 65536` limits HTTP/1 request headers to 64 KiB;
  Go maps it to the corresponding HTTP/2 header-list limit.
- `server.read_header_timeout: 10` bounds slow-header attacks. It does not
  limit request processing or response streaming.
- `server.max_request_body_size: 268435456` is the absolute 256 MiB safety net.
- `gateway.max_body_size: 268435456` remains available to multimodal, Gemini,
  image, video, and batch-image endpoints.
- `gateway.text_max_body_size: 33554432` limits the known pure-text
  `/embeddings` and `/alpha/search` endpoints to 32 MiB.
- H2C defaults to 50 concurrent streams per connection, a 2 MiB connection
  upload window, and a 512 KiB stream upload window.
- Invalid credential abuse is limited in process by trusted client IP (IPv6
  `/64`): 120 failures per 60 seconds followed by a 60-second block. This is a
  per-instance safety net; multi-instance enforcement still belongs at the
  load balancer, CDN, or WAF.

Do not add a single application-wide request semaphore: an SSE request may
legitimately occupy it for many minutes. Apply connection and unauthenticated
request controls at the edge; authenticated user/API-key concurrency remains
the application's responsibility.

## Trusted client IPs

Authentication, API-key IP restrictions, authentication rate limits, security
audit identity and session binding always use Gin's `server.trusted_proxies`
chain. Configure only the exact CIDR/IP addresses that connect directly to the
application. An absent or explicitly empty list trusts no forwarded addresses.
An edge proxy must replace client-supplied forwarding headers with its verified
client address. For a CDN, configure its trusted chain at the edge and restrict
direct origin access. Do not trust arbitrary internet peers.

The historical `security.trust_forwarded_ip_for_api_key_acl` name is retained
for configuration compatibility, but is disabled by default and now affects
only legacy request/usage metadata. Neither that switch nor
`security.forwarded_client_ip_headers` can override security decisions.
Existing `false` settings are never changed back to `true` by migration.
Custom metadata headers remain normalized, de-duplicated and limited to 16;
they can be changed through YAML, `SECURITY_FORWARDED_CLIENT_IP_HEADERS`, or
admin settings. Metadata from an untrusted sender is not an authorization fact.

Before upgrading a reverse-proxy deployment, set its actual trusted proxy
addresses. Sites that formerly relied on raw-header takeover may otherwise see
the socket peer address. Existing IP-bound sessions may require a new login
when the authoritative address changes.

Example for a proxy on the same host:

```yaml
server:
  trusted_proxies:
    - 127.0.0.1/32
    - ::1/128
```

## First-run setup authorization

The web setup wizard requires an operator token for installation and both
database/Redis connection tests. On first startup the server generates the token
and prints it to its local startup log; enter it in the wizard. Operators can
instead provide `SETUP_BOOTSTRAP_TOKEN` with at least 32 characters. The browser
keeps the token only in memory and sends it in `X-Setup-Token`, never in URLs.
Installed-system guards invalidate this capability after installation. The
read-only status endpoint remains public. `AUTO_SETUP` and CLI setup retain
their operator-controlled configuration flows. Protect remote setup with HTTPS
or an SSH tunnel, as it also transports database and administrator credentials.

## Generated-image downloads

URLs returned by image providers are fetched through a separate public-only
transport. Every redirect and DNS result is checked, and the connection uses
the exact approved address while preserving the original HTTP Host and TLS
server name. HTTP/HTTPS proxies must support CONNECT to that approved address,
including for plain HTTP image URLs; unsupported tunnels fail rather than
falling back to an unsafe destination or bypassing the proxy. Private configured
API endpoints and private S3 upload endpoints are unaffected. Only recognized
PNG/JPEG/WebP/GIF bytes are accepted for generated-image object storage.

## Nginx baseline

Define shared zones in the `http` block. Tune rates to measured legitimate
traffic; the values below are conservative starting points, not universal
capacity targets.

```nginx
limit_conn_zone $binary_remote_addr zone=tokensavy_conn:20m;
limit_req_zone  $binary_remote_addr zone=tokensavy_auth:20m rate=5r/s;
limit_req_zone  $binary_remote_addr zone=tokensavy_api:40m rate=30r/s;
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl http2;
    server_name api.example.com;

    client_header_timeout 10s;
    client_max_body_size 256m;
    large_client_header_buffers 4 16k;
    limit_conn tokensavy_conn 40;

    location ~ ^/(auth|api/auth)/ {
        limit_req zone=tokensavy_auth burst=10 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location ~ ^/(v1/)?(embeddings|alpha/search)$ {
        client_max_body_size 32m;
        limit_req zone=tokensavy_api burst=60 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location / {
        limit_req zone=tokensavy_api burst=60 nodelay;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 1800s;
        proxy_send_timeout 1800s;
        proxy_pass http://127.0.0.1:8080;
    }
}
```

If Nginx gzip is enabled in the `http` block, keep `text/event-stream` out of
`gzip_types` and do not use `gzip_types *` for Tokensavy. The
`proxy_buffering off` setting above prevents proxy buffering, but it does not
disable the gzip response filter. Use an explicit list for ordinary responses:

```nginx
gzip on;
gzip_types text/plain text/css application/json application/javascript application/xml image/svg+xml;
```

If a shared global configuration cannot exclude SSE by content type, set
`gzip off;` in the locations serving streaming API routes. This leaves gzip
available for the web UI and static assets.

Do not use an incoming `$http_x_forwarded_for` value unless Nginx real-IP
processing is restricted to explicit trusted proxy CIDRs.

## Caddy and CDN

The bundled `deploy/Caddyfile` sets a 64 KiB header limit, a 10-second header
timeout, a 256 MiB absolute body limit, and overwrites forwarded addresses from
the TCP peer. It is therefore a direct-to-Caddy baseline. Do not use its
`{remote_host}` forwarding lines unchanged behind a CDN: all clients would be
attributed to a CDN egress address, collapsing rejection aggregation and the
invalid-auth limiter onto unrelated users.

The bundled Caddy configuration leaves `flush_interval` unset so Caddy can
automatically flush `text/event-stream` responses while still propagating
client cancellation upstream. Do not set it globally: positive values can add
streaming latency, while Caddy 2.6.2's special `-1` mode also causes
reverse-proxied requests to continue after clients disconnect. The
configuration uses an explicit response content-type list for compression. Do
not replace that list with `text/*` or the shorthand `encode gzip zstd`: both
match `text/event-stream` and can buffer SSE until the response ends. Keep
streaming responses uncompressed while retaining compression for the web UI,
JSON, and static assets.

For a CDN deployment, first firewall the origin so only current CDN egress
CIDRs can connect. Then configure those exact ranges as Caddy trusted proxies
and derive upstream headers from Caddy's parsed `{client_ip}`. For example:

```caddyfile
{
	servers {
		trusted_proxies static 192.0.2.0/24 2001:db8:1234::/48
		trusted_proxies_strict
		client_ip_headers CF-Connecting-IP X-Forwarded-For
	}
}

api.example.com {
	reverse_proxy 127.0.0.1:8080 {
		header_up X-Real-IP {client_ip}
		header_up X-Forwarded-For {client_ip}
	}
}
```

Replace the documentation ranges with the CDN's published, automatically
maintained egress ranges. `CF-Connecting-IP` is safe here only because direct
origin access is blocked and Caddy trusts only those TCP peers. Configure
Tokensavy `server.trusted_proxies` with the Caddy address/private subnet so the
application accepts only Caddy's rewritten headers.

Caddy core does not provide a general request-rate limiter; use a trusted
CDN/WAF, a supported rate-limit module, or host firewall controls.

At a CDN/WAF, configure connection limits, header/body limits, bot challenges,
and per-IP/ASN rates before traffic reaches the origin. Allow origin ingress
only from CDN egress CIDRs or a private load balancer. Keep the application port
off the public Internet.

## DDoS boundary

Application checks reduce amplification after a connection reaches Go. They
cannot absorb volumetric attacks, TLS floods, bandwidth saturation, or a large
distributed source set. Those require upstream network capacity, CDN/WAF
filtering, provider firewall rules, and origin isolation. Avoid high-cardinality
metrics or per-request database security logs during rejection storms.
