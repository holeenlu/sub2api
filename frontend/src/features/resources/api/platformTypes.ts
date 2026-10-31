export type KnownAccountPlatform = 'anthropic' | 'openai' | 'gemini' | 'antigravity' | 'grok' | 'kimi' | 'zhipu' | 'deepseek' | 'minimax' | 'opencode_go' | 'typesafe' | 'command_code' | 'cline'

export type AccountPlatform = KnownAccountPlatform | (string & {})

export type AccountType = 'oauth' | 'setup-token' | 'apikey' | 'upstream' | 'bedrock' | 'service_account'
