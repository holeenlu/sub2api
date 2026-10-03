#!/usr/bin/env python3
"""Update only the local Codex catalog using the configured gateway API key.

Python 3.11+. Run: python3 update-codex-models.py
Use --check to fetch and validate without replacing the file.
No generation requests, automatic retries, configuration edits or login changes.
"""
import argparse
import json
import os
from pathlib import Path
import tempfile
import tomllib
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("Catalog endpoint redirected; verify the configured gateway URL")


def load_configuration(path: Path):
    with path.open("rb") as handle:
        config = tomllib.load(handle)
    provider_id = config.get("model_provider", "openai")
    provider = config.get("model_providers", {}).get(provider_id, {})
    base = str(provider.get("base_url", "")).rstrip("/")
    url = urllib.parse.urlsplit(base)
    if url.username or url.password or url.query or url.fragment:
        raise ValueError("Use a gateway base URL without credentials, query or fragment")
    if not url.hostname or (url.scheme != "https" and not (url.scheme == "http" and url.hostname in {"localhost", "127.0.0.1", "::1"})):
        raise ValueError("A HTTPS gateway base URL is required (HTTP is allowed for localhost)")
    token = provider.get("experimental_bearer_token") or os.environ.get(provider.get("env_key", ""), "")
    if not token and provider.get("requires_openai_auth"):
        auth_path = path.parent / "auth.json"
        if auth_path.exists():
            with auth_path.open() as handle:
                token = json.load(handle).get("OPENAI_API_KEY", "")
    if not isinstance(token, str) or not token.strip():
        raise ValueError("No gateway API key found in the active provider, its environment variable or auth.json")
    # OAuth access/refresh tokens are intentionally not repurposed for a gateway.
    root = base[:-3] if base.endswith("/v1") else base
    endpoint = root + "/backend-api/codex/models?catalog_view=client"
    configured_path = config.get("model_catalog_json", "~/.codex/codex-models.json")
    output = Path(os.path.expanduser(str(configured_path)))
    if not output.is_absolute():
        output = path.parent / output
    return config, endpoint, token.strip(), output


def validate_manifest(body: bytes, config: dict) -> dict:
    value = json.loads(body)
    if not isinstance(value, dict) or not isinstance(value.get("models"), list) or not value["models"]:
        raise ValueError("The gateway did not return a nonempty Codex models manifest")
    seen = set()
    visible = []
    for model in value["models"]:
        if not isinstance(model, dict):
            raise ValueError("Invalid model entry")
        slug = model.get("slug")
        if not isinstance(slug, str) or not slug.strip() or slug in seen or "*" in slug:
            raise ValueError("Missing, duplicate or wildcard model ID")
        name = slug.strip().rsplit('/', 1)[-1].lower()
        if (str(model.get('visibility', '')).strip().lower() == 'hide'
                or str(model.get('model_purpose', '')).strip().lower() == 'background'
                or name.startswith('codex-auto-') or name == 'gpt-reserve'):
            continue
        seen.add(slug)
        visible.append(model)
        if not isinstance(model.get("input_modalities"), list) or not model["input_modalities"]:
            raise ValueError("A model is missing input modalities")
        levels = model.get("supported_reasoning_levels")
        if not isinstance(levels, list) or any(not isinstance(level, dict) or not isinstance(level.get("effort"), str) for level in levels):
            raise ValueError("Invalid reasoning levels")
        default = model.get("default_reasoning_level")
        if default and default not in {level["effort"] for level in levels}:
            raise ValueError("Default reasoning level is not supported")
        context = model.get("context_window")
        if isinstance(context, bool) or not isinstance(context, int) or context <= 0:
            raise ValueError("Invalid context window")
        messages = model.get("model_messages")
        if not isinstance(messages, dict) or not isinstance(messages.get("instructions_template"), str):
            raise ValueError("Missing Codex model instructions")
    for key in ("model", "review_model"):
        selected = config.get(key)
        if selected and selected not in seen:
            raise ValueError("The configured main or review model is absent; select an authorized model before updating")
    if not visible:
        raise ValueError("The gateway returned no client-selectable models")
    return {**value, 'models': visible}


def atomic_catalog_write(path: Path, value: dict):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, prefix=".codex-models-", delete=False) as handle:
            temporary = Path(handle.name)
            json.dump(value, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        temporary = None
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))) / "config.toml")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    try:
        config, endpoint, token, output = load_configuration(args.config.expanduser().resolve())
        request = urllib.request.Request(endpoint, headers={"Authorization": "Bearer " + token, "Accept": "application/json", "Accept-Encoding": "identity"})
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=30) as response:
            body = response.read(16 * 1024 * 1024 + 1)
        if len(body) > 16 * 1024 * 1024:
            raise ValueError("Model catalog exceeds the 16 MiB size limit")
        value = validate_manifest(body, config)
        if args.check:
            print("Catalog validated; no files were changed.")
        else:
            atomic_catalog_write(output, value)
            print(f"Updated {output} with {len(value['models'])} models. Restart Codex to load the catalog.")
    except urllib.error.HTTPError as error:
        parser.exit(1, f"Catalog request failed with HTTP {error.code}; existing files were retained.\n")
    except (urllib.error.URLError, OSError, ValueError, tomllib.TOMLDecodeError):
        # Do not echo URLs, credentials or untrusted upstream error bodies.
        parser.exit(1, "Catalog update failed validation, authentication, TLS or I/O checks; existing files were retained. Check your provider URL, key, selected models and file path.\n")


if __name__ == "__main__":
    main()
