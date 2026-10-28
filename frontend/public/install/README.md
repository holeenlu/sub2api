# sub2api online installers

Requires Python 3. Set both `SUB2API_API_KEY` and `SUB2API_BASE_URL` before running an installer. The base URL is the gateway root shown by your deployment console.

```bash
export SUB2API_BASE_URL='https://your-domain.example'
export SUB2API_API_KEY='your-key'
curl -fsSL https://your-domain.example/install/codex.sh | bash
curl -fsSL https://your-domain.example/install/claude-code.sh | bash
```

Review scripts before piping them into a shell. They never print the key, pass it as a process argument, or call a model API. Configuration files are created with mode `600`.

A new Codex installation uses `model_provider = "sub2api"` and `env_key = "SUB2API_API_KEY"`. Claude Code keeps its native `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` fields. `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, and Codex's optional `MODEL_ID` remain supported.

Existing client configurations are never overwritten or migrated. If `config.toml` or `settings.json` exists, the installer writes `config.sub2api.toml` or `settings.sub2api.json` beside it for manual review. Back up the existing file before merging only the fields you choose; preserve your current provider, profiles, login and privacy settings. An existing candidate is also retained and causes the installer to stop without overwriting it.

The image skills and model-catalog updater still honor the active provider's configured `env_key`, including an existing provider with a historical name. Only an explicit `SUB2API_BASE_URL` opts the image skills into an environment override, which then requires `SUB2API_API_KEY`; no automatic legacy-variable fallback is added.
