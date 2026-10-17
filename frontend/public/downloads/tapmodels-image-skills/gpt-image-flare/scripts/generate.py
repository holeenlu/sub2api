#!/usr/bin/env python3
"""Generate or edit one image with the fixed Flare model."""
from __future__ import annotations
import argparse, base64, ipaddress, json, os, re, secrets, ssl, sys, tempfile, time, tomllib
from contextlib import contextmanager
from io import BytesIO
from pathlib import Path
from typing import Iterator, Mapping
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit, urlunsplit
from urllib.request import HTTPHandler, HTTPRedirectHandler, HTTPSHandler, Request, build_opener
try:
    from PIL import Image, UnidentifiedImageError
except ImportError:
    Image = None
    UnidentifiedImageError = OSError

MODEL = "gpt-image-2.5-flare"
MAX_INPUT_BYTES = 20 << 20
MAX_RESPONSE_BYTES = 128 << 20

class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: ANN001
        return None

def _loopback(host: str) -> bool:
    if host == "localhost" or host.endswith(".localhost"):
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False

def images_endpoint(base_url: str, operation: str) -> str:
    if operation not in {"generations", "edits"}:
        raise RuntimeError(f"Unsupported image operation: {operation}")
    try:
        p = urlsplit(base_url)
        host = p.hostname or ""
        _ = p.port
    except ValueError as exc:
        raise RuntimeError("The configured provider base_url is invalid") from exc
    if p.scheme not in {"http", "https"} or not p.netloc or not host:
        raise RuntimeError("The configured provider base_url must be an HTTP(S) URL")
    if p.username or p.password or p.query or p.fragment:
        raise RuntimeError("The configured provider base_url must not contain credentials, query, or fragment")
    if p.scheme != "https" and not _loopback(host):
        raise RuntimeError("The configured provider base_url must use HTTPS except for loopback testing")
    path = p.path.rstrip("/")
    if path.endswith("/responses"):
        path = path[:-10]
    if not path.endswith("/v1"):
        path += "/v1"
    return urlunsplit((p.scheme, p.netloc, path + f"/images/{operation}", "", ""))

def _strmap(value: object, label: str) -> dict[str, str]:
    if value is None:
        return {}
    if not isinstance(value, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in value.items()):
        raise RuntimeError(f"{label} must contain only string keys and values")
    return dict(value)

def _env_name(value: str) -> bool:
    return re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", value) is not None

def load_provider(require_token: bool = True) -> tuple[str, str, dict[str, str], str | None]:
    env_base, env_key = os.environ.get("TAPMODELS_BASE_URL"), os.environ.get("TAPMODELS_API_KEY")
    # A key-only environment is the normal setup when Codex provides the URL
    # and env_key name.  Only an explicit base URL opts into full override.
    if env_base is not None:
        base = (env_base or "").strip()
        token = (env_key or "").strip()
        if not base:
            raise RuntimeError("TAPMODELS_BASE_URL is required when using the TapModels environment override")
        if require_token and not token:
            raise RuntimeError("TAPMODELS_API_KEY is required when using the TapModels environment override")
        return "TapModels environment", base, {}, token or None
    home = Path(os.environ.get("CODEX_HOME", Path.home() / ".codex")).expanduser()
    path = home / "config.toml"
    try:
        config = tomllib.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise RuntimeError(f"Codex config not found: {path}") from exc
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise RuntimeError(f"Codex config is invalid: {path}") from exc
    if "profile" in config or "profiles" in config:
        raise RuntimeError("Codex profile-based provider selection is not supported; use top-level model_provider/model_providers or TAPMODELS_BASE_URL")
    name, providers = config.get("model_provider", "openai"), config.get("model_providers", {})
    provider = providers.get(name) if isinstance(name, str) and isinstance(providers, dict) else None
    if not isinstance(provider, dict):
        raise RuntimeError(f"Model provider {name!r} is not configured at the top level")
    base = provider.get("base_url")
    if not isinstance(base, str) or not base.strip():
        raise RuntimeError(f"Model provider {name!r} has no base_url")
    headers = _strmap(provider.get("http_headers"), "http_headers")
    env_headers = _strmap(provider.get("env_http_headers"), "env_http_headers")
    if require_token:
        for header, variable in env_headers.items():
            if not _env_name(variable):
                raise RuntimeError("env_http_headers contains an invalid environment variable name")
            value = os.environ.get(variable, "").strip()
            if not value:
                raise RuntimeError(f"Configured header environment variable {variable!r} is missing")
            headers[header] = value
    configured_env_key = provider.get("env_key")
    if configured_env_key is not None and (not isinstance(configured_env_key, str) or not _env_name(configured_env_key)):
        raise RuntimeError(f"Model provider {name!r} has an invalid env_key")
    token: str | None = None
    if require_token:
        if configured_env_key:
            token = os.environ.get(configured_env_key, "").strip()
            if not token:
                raise RuntimeError(f"Configured provider environment variable {configured_env_key!r} is missing")
        else:
            token = str(provider.get("experimental_bearer_token", "")).strip()
            if not token:
                raise RuntimeError(f"No API key is configured for model provider {name!r}")
    return str(name), base.strip(), headers, token

def read_prompt(args: argparse.Namespace) -> str:
    try:
        value = Path(args.prompt_file).expanduser().read_text(encoding="utf-8") if args.prompt_file else args.prompt
    except OSError as exc:
        raise RuntimeError("Unable to read --prompt-file") from exc
    if not (value or "").strip():
        raise RuntimeError("Provide a non-empty --prompt or --prompt-file")
    return value.strip()

def default_output(fmt: str) -> Path:
    home = Path(os.environ.get("CODEX_HOME", Path.home() / ".codex")).expanduser()
    return home / "generated-images" / f"{MODEL}-{time.strftime('%Y%m%d-%H%M%S')}.{ 'jpg' if fmt == 'jpeg' else fmt }"

def decode_image(data: bytes, label: str) -> tuple[str, int, int, str]:
    if Image is None:
        raise RuntimeError("Pillow is required; install dependencies from requirements.txt")
    try:
        with Image.open(BytesIO(data)) as im:
            im.verify()
        with Image.open(BytesIO(data)) as im:
            im.load(); fmt, size = (im.format or "").upper(), im.size
            mime = Image.MIME.get(fmt, "")
    except (OSError, UnidentifiedImageError, ValueError) as exc:
        raise RuntimeError(f"{label} is not a decodable image") from exc
    if size[0] <= 0 or size[1] <= 0 or not mime.startswith("image/"):
        raise RuntimeError(f"{label} is not a decodable image")
    return {"PNG": "png", "JPEG": "jpeg", "WEBP": "webp"}.get(fmt, fmt.lower()), size[0], size[1], mime

def input_file(path: Path, label: str) -> tuple[Path, bytes, str]:
    resolved = path.expanduser().resolve()
    if not resolved.is_file():
        raise RuntimeError(f"{label} file not found: {resolved}")
    try: data = resolved.read_bytes()
    except OSError as exc: raise RuntimeError(f"Unable to read {label.lower()} file") from exc
    if not data: raise RuntimeError(f"{label} file is empty: {resolved}")
    if len(data) > MAX_INPUT_BYTES: raise RuntimeError(f"{label} exceeds this tool's 20 MiB input limit: {resolved}")
    _, _, _, mime = decode_image(data, label)
    return resolved, data, mime

def multipart_payload(fields, files):
    boundary = f"----codex-image-{secrets.token_hex(16)}"; body = []
    for name, value in fields:
        body += [f"--{boundary}\r\n".encode(), f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode(), value.encode(), b"\r\n"]
    for name, path, data, mime in files:
        filename = re.sub(r'["\r\n]', "_", path.name)
        body += [f"--{boundary}\r\n".encode(), f'Content-Disposition: form-data; name="{name}"; filename="{filename}"\r\n'.encode(), f"Content-Type: {mime}\r\n\r\n".encode(), data, b"\r\n"]
    body.append(f"--{boundary}--\r\n".encode()); return b"".join(body), f"multipart/form-data; boundary={boundary}"

def _safe(value: object) -> str | None:
    text = str(value or "").strip(); return text if re.fullmatch(r"[A-Za-z0-9._:/-]{1,128}", text) else None

def api_failure(status, kind, headers: Mapping[str, str] | object = {}) -> RuntimeError:
    parts = [f"status={status}", f"type={_safe(kind) or 'unknown'}"]
    getter = getattr(headers, "get", None)
    if getter:
        for key in ("x-request-id", "request-id", "cf-ray"):
            rid = _safe(getter(key))
            if rid: parts.append(f"request_id={rid}"); break
    return RuntimeError("Image API request failed (" + ", ".join(parts) + ")")

def response_type(raw: bytes) -> str:
    try:
        value = json.loads(raw); error = value.get("error", value) if isinstance(value, dict) else {}
        candidate = _safe(error.get("type")) if isinstance(error, dict) else None
        return candidate or "http_error"
    except (ValueError, UnicodeDecodeError): return "http_error"

def open_request(request: Request, timeout: int):
    return build_opener(HTTPHandler(), HTTPSHandler(context=ssl.create_default_context()), NoRedirect()).open(request, timeout=timeout)

@contextmanager
def reserve_output(output: Path, force: bool) -> Iterator[tuple[int, int] | None]:
    output.parent.mkdir(parents=True, exist_ok=True); lock = output.parent / f".{output.name}.lock"; reservation = None
    try: lock_fd = os.open(lock, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as exc: raise RuntimeError(f"Output is already being written: {output}") from exc
    os.close(lock_fd)
    try:
      if not force:
        try: fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError as exc: raise RuntimeError(f"Output already exists: {output}; use --force to replace it") from exc
        s = os.fstat(fd); reservation = (s.st_dev, s.st_ino); os.close(fd)
      try:
          yield reservation
      except BaseException:
          if reservation:
              try:
                  s = output.stat()
                  if (s.st_dev, s.st_ino) == reservation: output.unlink()
              except FileNotFoundError: pass
          raise
    finally:
        try: lock.unlink()
        except FileNotFoundError: pass

def write_output(output: Path, image: bytes, reservation) -> None:
    temp = ""
    try:
        with tempfile.NamedTemporaryFile(dir=output.parent, prefix=f".{output.name}.", delete=False) as f:
            temp = f.name; f.write(image); f.flush(); os.fsync(f.fileno())
        os.chmod(temp, 0o600)
        if reservation:
            try: s = output.stat()
            except FileNotFoundError as exc: raise RuntimeError("Output reservation was removed during the request") from exc
            if (s.st_dev, s.st_ino) != reservation: raise RuntimeError("Output changed during the request; refusing to overwrite it")
        os.replace(temp, output); temp = ""
    finally:
        if temp:
            try: os.unlink(temp)
            except FileNotFoundError: pass

def parser():
    p = argparse.ArgumentParser(description=f"Generate or edit one image with {MODEL}"); source = p.add_mutually_exclusive_group(); source.add_argument("--prompt"); source.add_argument("--prompt-file")
    p.add_argument("--image", action="append", type=Path); p.add_argument("--mask", type=Path); p.add_argument("--size"); p.add_argument("--quality", default="high"); p.add_argument("--output-format", choices=("png", "jpeg", "webp"), default="png"); p.add_argument("--background", choices=("auto", "opaque", "transparent")); p.add_argument("--output", type=Path); p.add_argument("--timeout", type=int, default=300); p.add_argument("--force", action="store_true"); p.add_argument("--check-config", action="store_true"); p.add_argument("--dry-run", action="store_true"); return p

def main() -> int:
    args = parser().parse_args()
    if args.timeout <= 0: raise RuntimeError("--timeout must be greater than zero")
    if args.check_config and args.dry_run: raise RuntimeError("Use only one of --check-config and --dry-run")
    if args.mask and not args.image: raise RuntimeError("--mask requires at least one --image")
    provider, base, extra, token = load_provider(not (args.check_config or args.dry_run)); operation = "edits" if args.image else "generations"; endpoint = images_endpoint(base, operation)
    if args.check_config:
        print(json.dumps({"ok": True, "provider": provider, "generation_endpoint": images_endpoint(base, "generations"), "edit_endpoint": images_endpoint(base, "edits"), "model": MODEL, "credentials_checked": False})); return 0
    payload = {"model": MODEL, "prompt": read_prompt(args), "n": 1, "quality": args.quality, "output_format": args.output_format, "response_format": "b64_json"}
    if args.size:
        if not re.fullmatch(r"(?:auto|[1-9]\d{1,4}x[1-9]\d{1,4})", args.size): raise RuntimeError("--size must be 'auto' or WIDTHxHEIGHT with positive dimensions")
        payload["size"] = args.size
    elif operation == "generations": payload["size"] = "1024x1024"
    if not args.quality.strip(): raise RuntimeError("--quality must not be empty")
    if args.background: payload["background"] = args.background
    files = []
    if operation == "edits":
        files = [("image[]", *input_file(path, "Input image")) for path in args.image]
        if args.mask: files.append(("mask", *input_file(args.mask, "Mask")))
    if args.dry_run:
        print(json.dumps({"ok": True, "dry_run": True, "endpoint": endpoint, "operation": operation, "payload": payload, "input_files": len(args.image or []), "has_mask": bool(args.mask)}, ensure_ascii=False, indent=2)); return 0
    output = (args.output or default_output(args.output_format)).expanduser().resolve()
    with reserve_output(output, args.force) as reservation:
        if operation == "edits": body, content_type = multipart_payload([(k, str(v)) for k, v in payload.items()], files)
        else: body, content_type = json.dumps(payload, ensure_ascii=False).encode(), "application/json"
        headers = {"Content-Type": content_type, "Accept": "application/json", **extra, "Authorization": f"Bearer {token}"}; response_headers = {}
        try:
            with open_request(Request(endpoint, body, headers, method="POST"), args.timeout) as response:
                response_headers = response.headers; raw = response.read(MAX_RESPONSE_BYTES + 1)
        except HTTPError as exc:
            try:
                if 300 <= exc.code < 400:
                    kind = "redirect_blocked"
                else:
                    try: raw_error = exc.read(1_000_000)
                    except OSError: raw_error = b""
                    kind = response_type(raw_error)
            finally: exc.close()
            raise api_failure(exc.code, kind, exc.headers) from exc
        except (URLError, TimeoutError, OSError) as exc: raise api_failure("unavailable", "network_error") from exc
        if len(raw) > MAX_RESPONSE_BYTES: raise api_failure(200, "response_too_large", response_headers)
        try:
            data = json.loads(raw); item = data["data"][0]; image = base64.b64decode(item["b64_json"], validate=True)
        except (KeyError, IndexError, TypeError, ValueError, json.JSONDecodeError) as exc: raise api_failure(200, "invalid_response", response_headers) from exc
        fmt, width, height, _ = decode_image(image, "API response")
        if fmt != args.output_format: raise api_failure(200, "unexpected_image_format", response_headers)
        write_output(output, image, reservation)
    print(json.dumps({"ok": True, "path": str(output), "operation": operation, "model": item.get("model") or data.get("model") or MODEL, "generation_id": item.get("generation_id") or data.get("generation_id"), "format": fmt, "width": width, "height": height, "bytes": len(image)}, ensure_ascii=False)); return 0

if __name__ == "__main__":
    try: raise SystemExit(main())
    except RuntimeError as exc: print(json.dumps({"ok": False, "error": str(exc)}, ensure_ascii=False), file=sys.stderr); raise SystemExit(1)
