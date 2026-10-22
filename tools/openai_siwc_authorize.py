#!/usr/bin/env python3
"""Capture one SIWC browser callback locally; never exchange or upload credentials."""
import getpass
import hmac
from http.server import BaseHTTPRequestHandler, HTTPServer
import re
import sys
import time
from urllib.parse import parse_qs, urlsplit
import webbrowser

AUTHORIZE_URL = "https://auth.openai.com/api/accounts/authorize"
CALLBACK_URL = "http://127.0.0.1:1455/auth/callback"
TIMEOUT_SECONDS = 30 * 60
MAX_URL_LENGTH = 32 * 1024


def single_query(query):
    try:
        values = parse_qs(query, keep_blank_values=True, strict_parsing=True)
    except ValueError:
        raise ValueError("Malformed URL parameters") from None
    if any(len(items) != 1 for items in values.values()):
        raise ValueError("Duplicate URL parameters are not accepted")
    return {key: items[0] for key, items in values.items()}


def validate_authorization_url(raw):
    if len(raw) > MAX_URL_LENGTH or any(ord(char) < 32 for char in raw):
        raise ValueError("Invalid authorization URL")
    url = urlsplit(raw)
    if (url.scheme != "https" or url.netloc != "auth.openai.com" or
            url.path != "/api/accounts/authorize" or url.fragment):
        raise ValueError("Only the official OpenAI SIWC authorization URL is accepted")
    query = single_query(url.query)
    expected = {
        "redirect_uri": CALLBACK_URL, "resource": "https://api.openai.com/v1",
        "response_type": "code", "code_challenge_method": "S256",
    }
    if any(query.get(key) != value for key, value in expected.items()):
        raise ValueError("Unexpected SIWC redirect, resource, or PKCE configuration")
    if (not query.get("client_id") or not query.get("state") or
            not query.get("nonce") or not query.get("ext_agent_host_id") or
            not re.fullmatch(r"[A-Za-z0-9_-]{43}", query.get("code_challenge", ""))):
        raise ValueError("Incomplete SIWC authorization parameters")
    if query["client_id"] == "dynamic_agent_client" and not query.get("agent_name_hint"):
        raise ValueError("Initial SIWC registration requires the application name")
    scopes = set(query.get("scope", "").split())
    if not {"openid", "profile", "email", "offline_access", "resource.invoke", "chatgpt.tokens.use.direct"} <= scopes:
        raise ValueError("Missing SIWC identity or ChatGPT plan usage scopes")
    return query


def validate_callback(target, host, pending):
    if len(target) > MAX_URL_LENGTH or any(ord(char) < 32 for char in target):
        raise ValueError("Invalid callback")
    url = urlsplit(target)
    if (host != "127.0.0.1:1455" or url.scheme or url.netloc or
            url.path != "/auth/callback" or url.fragment):
        raise ValueError("Unexpected callback destination")
    query = single_query(url.query)
    if not hmac.compare_digest(query.get("state", "").encode(), pending["state"].encode()):
        raise ValueError("Callback state does not match this attempt")
    if bool(query.get("code")) == bool(query.get("error")):
        raise ValueError("Callback must contain either a code or an OAuth error")
    if query.get("error"):
        return CALLBACK_URL + "?" + url.query
    client_id = query.get("client_id", "")
    if pending["client_id"] == "dynamic_agent_client":
        if not client_id or client_id == "dynamic_agent_client":
            raise ValueError("Initial callback is missing its issued client ID")
    elif client_id and client_id != pending["client_id"]:
        raise ValueError("Callback client ID differs from the selected registration")
    return CALLBACK_URL + "?" + url.query


class CallbackHandler(BaseHTTPRequestHandler):
    def setup(self):
        self.request.settimeout(5)
        super().setup()

    def log_message(self, _format, *_args):
        # Request targets contain a one-use authorization code and must not be logged.
        pass

    def do_GET(self):
        try:
            callback = validate_callback(self.path, self.headers.get("Host", ""), self.server.pending)
        except (ValueError, UnicodeError):
            self.respond(400, "Invalid callback. The current sign-in attempt is still waiting.")
            return
        if self.server.callback is not None:
            self.respond(409, "A callback was already captured.")
            return
        self.server.callback = callback
        self.respond(200, "Callback captured locally. Return to your terminal and paste the callback into the account settings. You can close this page.")

    def respond(self, status, message):
        body = message.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
        self.end_headers()
        self.wfile.write(body)


def capture_callback(auth_url):
    pending = validate_authorization_url(auth_url)
    # The constructor binds and starts listening before the browser can redirect.
    # If another application owns this port, fail; do not redirect to its listener.
    with HTTPServer(("127.0.0.1", 1455), CallbackHandler) as server:
        server.pending = pending
        server.callback = None
        server.timeout = 1
        if not webbrowser.open(auth_url, new=1, autoraise=True):
            print("The local listener is ready. Open the authorization link from account settings in this computer's browser.", file=sys.stderr)
        deadline = time.monotonic() + TIMEOUT_SECONDS
        while server.callback is None:
            if time.monotonic() >= deadline:
                raise TimeoutError("Sign-in timed out. Start a new authorization in account settings.")
            server.handle_request()
        return server.callback


def main():
    if len(sys.argv) != 1:
        print("Run without arguments. Paste the authorization URL into the hidden prompt, not the command line.", file=sys.stderr)
        return 2
    if not sys.stdin.isatty():
        print("Run in an interactive local terminal so the authorization URL is not echoed or piped into logs.", file=sys.stderr)
        return 2
    try:
        auth_url = getpass.getpass("Paste the full SIWC authorization URL (hidden): ").strip()
        callback = capture_callback(auth_url)
        print("Paste this one-use callback into account settings; do not share it:")
        print(callback)
        return 0
    except (ValueError, OSError, TimeoutError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    except (KeyboardInterrupt, EOFError):
        print("Sign-in cancelled locally.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
