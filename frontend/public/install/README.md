# KDAN online installers

Set `KDAN_API_KEY` before running an installer. Existing client files are backed up first.

```bash
export KDAN_API_KEY='your-key'
curl -fsSL https://your-domain.example/install/codex.sh | bash
curl -fsSL https://your-domain.example/install/claude-code.sh | bash
```

Review scripts before piping them into a shell. They never print the key or call a model API.
