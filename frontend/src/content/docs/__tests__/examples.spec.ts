// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { buildDocsExamples } from '../examples'

describe('copyable API examples', () => {
  for (const kind of ['chat', 'responses', 'messages', 'image', 'models'] as const) {
    it(`${kind} expands the key and sends one shell command`, () => {
      const code = buildDocsExamples(kind, 'https://example.com/v1/', 'test-model')[0].code
      // Replace curl with a shell function; no network or real key is involved.
      const args = execFileSync('/bin/bash', ['-c', `curl() { printf '%s\\0' "$@"; }\n${code}`], {
        env: { KDAN_API_KEY: 'test-value' }
      }).toString().split('\0').filter(Boolean)
      expect(args).toContain(kind === 'messages' ? 'x-api-key: test-value' : 'Authorization: Bearer test-value')
      expect(args.filter(arg => arg.startsWith('https://example.com/'))).toHaveLength(1)
      expect(args.join(' ')).not.toContain('/v1/v1')
      if (kind !== 'models') expect(JSON.parse(args[args.indexOf('-d') + 1]).model).toBe('test-model')
    })
  }
})
