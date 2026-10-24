---
name: gpt-image-sunburst
description: Generate or edit images with gpt-image-2.5-sunburst through the API provider and API key already configured in the Codex client. Use for Sunburst image generation, image editing, reference-image composition, or explicit $gpt-image-sunburst requests; do not use another image model.
---

# GPT Image 2.5 Sunburst

Use Python 3.11+. Create `.venv` in this skill directory and install `requirements.txt`; Pillow is required for real image decoding. Run `scripts/generate.py` with that environment's Python from this skill directory; do not assume a fixed installation path. (`--check-config` can run without Pillow, but generation and editing cannot.) The skill can be installed under the default `~/.agents/skills` or the compatible `~/.codex/skills` directory. It reads the top-level Codex `model_provider`/`model_providers` configuration. Setting only `TOKENSAVY_API_KEY` leaves the provider's `base_url` and `env_key` in effect; an explicit `TOKENSAVY_BASE_URL` enables a complete environment override and requires `TOKENSAVY_API_KEY`. Codex `profile`/`profiles` provider selection is rejected explicitly. An `env_key` configured in Codex is authoritative: a missing variable fails and never falls back to `OPENAI_API_KEY`. Never print, copy, or place the credential in a command argument or generated file.

The provider URL must use HTTPS; plain HTTP is accepted only for loopback test servers. Redirects are rejected before credentials could be sent to another host. Input images and masks must decode successfully and each is limited by this tool to 20 MiB; that is a tool limit, not a claim about provider limits. Output PNG, JPEG, and WebP are decoded and verified with Pillow, and the reported dimensions are the actual decoded dimensions.

Use an absolute output path. If the user does not choose one, allow the script to save under `$CODEX_HOME/generated-images`.

```bash
cd /path/to/gpt-image-sunburst
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
.venv/bin/python scripts/generate.py \
  --prompt "A precise description of the requested image" \
  --size 1024x1024 \
  --quality high
```

For editing, pass the source image with `--image`. Repeat `--image` for additional reference images. Add `--mask` only when the user supplies a mask for the first image.

```bash
cd /path/to/gpt-image-sunburst
.venv/bin/python scripts/generate.py \
  --image "/absolute/path/source.png" \
  --prompt "Add a red beret while preserving the subject, background, and composition" \
  --quality high
```

Use `--prompt-file PATH` instead of `--prompt` for long or multiline prompts. Supported controls are `--image`, `--mask`, `--size`, `--quality`, `--output-format`, `--background`, and `--output`. The script selects `/v1/images/edits` when `--image` is present and `/v1/images/generations` otherwise. Pass only controls the user requested; the displayed dimensions must come from the decoded file because the service may return a different size.

The script makes one paid API request only for an explicit generation or edit request and never retries automatically. A retry after an error requires a new user request or clear authorization. Output creation uses atomic temporary files and an exclusive reservation/lock; existing files require `--force`, and concurrent writers are rejected. On success, show the returned absolute path and render the local image in the response. Report the actual model, format, and dimensions emitted by the script.

Use `--check-config` for a credential-free readiness check. It validates provider and endpoint metadata without requiring or displaying a key. Use `--dry-run` to validate the payload and input files without any network request. Errors expose only safe status/type/request-id fields; raw provider JSON, base64 data, and credentials are never echoed.
