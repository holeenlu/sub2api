# Tokensavy online installers

Set `TOKENSAVY_API_KEY` before running an installer. Existing client files are backed up first.

```bash
export TOKENSAVY_API_KEY='your-key'
curl -fsSL https://tokensavy.ai/install/codex.sh | bash
curl -fsSL https://tokensavy.ai/install/claude-code.sh | bash
```

Review scripts before piping them into a shell. They never print the key or call a model API.
