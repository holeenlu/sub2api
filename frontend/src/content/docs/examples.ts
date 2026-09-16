import type { DocsCodeExample } from './types'

function endpointFor(kind: NonNullable<import('./types').DocsNavItem['exampleKind']>): string {
  return {
    chat: '/v1/chat/completions', responses: '/v1/responses', messages: '/v1/messages',
    image: '/v1/images/generations', models: '/v1/models'
  }[kind]
}

export function buildDocsExamples(kind: NonNullable<import('./types').DocsNavItem['exampleKind']>, baseRoot: string, model: string): DocsCodeExample[] {
  const root = baseRoot.replace(/\/+$/, '').replace(/\/v1$/, '') || 'https://api.example.com'
  const endpoint = `${root}${endpointFor(kind)}`
  const isGet = kind === 'models'
  const anthropic = kind === 'messages'
  const body = kind === 'responses'
    ? { model, input: 'Explain why low latency matters in one sentence.' }
    : kind === 'chat'
      ? { model, messages: [{ role: 'user', content: 'Explain why low latency matters in one sentence.' }] }
      : kind === 'messages'
        ? { model, max_tokens: 256, messages: [{ role: 'user', content: 'Explain why low latency matters in one sentence.' }] }
        : kind === 'image'
          ? { model, prompt: 'A precise product illustration on a white background', size: '1024x1024' }
          : undefined
  const shellQuote = (value: string) => "'" + value.replace(/'/g, "'\"'\"'") + "'"
  const curlArgs = [`curl --fail-with-body --max-time 300 ${isGet ? '' : '-X POST '}${shellQuote(endpoint)}`,
    `  -H "${anthropic ? 'x-api-key: $API_KEY' : 'Authorization: Bearer $API_KEY'}"`]
  if (anthropic) curlArgs.push("  -H 'anthropic-version: 2023-06-01'")
  if (body) curlArgs.push("  -H 'Content-Type: application/json'", `  -d ${shellQuote(JSON.stringify(body))}`)
  const curlCode = curlArgs.join(' \\\n')
  const pythonHeaders = anthropic
    ? `{\n        "x-api-key": os.environ["API_KEY"],\n        "anthropic-version": "2023-06-01",\n    }`
    : `{"Authorization": f"Bearer {os.environ['API_KEY']}"}`
  return [
    { id: 'curl', label: 'cURL', language: 'bash', code: curlCode },
    { id: 'python', label: 'Python', language: 'python', code: `import os\nimport requests\n\nresponse = requests.${isGet ? 'get' : 'post'}(\n    ${JSON.stringify(endpoint)},\n    headers=${pythonHeaders},${body ? `\n    json=${JSON.stringify(body, null, 4).replace(/\n/g, '\n    ')},` : ''}\n    timeout=300,\n)\nresponse.raise_for_status()\nprint(response.json())` },
    { id: 'node', label: 'Node.js', language: 'javascript', code: `const response = await fetch(${JSON.stringify(endpoint)}, {\n  method: '${isGet ? 'GET' : 'POST'}',\n  headers: {\n    '${anthropic ? 'x-api-key' : 'Authorization'}': ${anthropic ? 'process.env.API_KEY' : '`Bearer ${process.env.API_KEY}`'},${anthropic ? "\n    'anthropic-version': '2023-06-01'," : ''}${body ? "\n    'Content-Type': 'application/json'," : ''}\n  },${body ? `\n  body: JSON.stringify(${JSON.stringify(body, null, 2).replace(/\n/g, '\n  ')})` : ''}\n})\nif (!response.ok) throw new Error(await response.text())\nconsole.log(await response.json())` }
  ]
}
