// @vitest-environment node
import { afterEach, describe, expect, it } from 'vitest'
import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const publicRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../../../../../public')
const fixtureKey = 'fixture-only-not-a-real-key'
const temporaryDirectories: string[] = []
afterEach(() => { temporaryDirectories.splice(0).forEach(path => rmSync(path, { recursive: true, force: true })) })

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), 'sub2api-install-test-'))
  temporaryDirectories.push(directory)
  const env: Record<string, string> = { PATH: process.env.PATH || '/usr/bin:/bin', CODEX_HOME: join(directory, 'codex'), CLAUDE_CONFIG_DIR: join(directory, 'claude'), SUB2API_BASE_URL: 'https://gateway.example.test', SUB2API_API_KEY: fixtureKey }
  return { directory, env }
}

function install(name: 'codex' | 'claude-code', env: Record<string, string>) {
  const result = spawnSync('/bin/bash', [join(publicRoot, 'install', name + '.sh')], { env, encoding: 'utf8', timeout: 10000 })
  expect(result.stdout + result.stderr).not.toContain(fixtureKey)
  if (result.error) throw result.error
  return result
}

describe('neutral online installers', () => {
  it('creates a valid Codex provider with the new ID and key variable', () => {
    const { env } = fixture()
    env.SUB2API_BASE_URL += '/v1/'
    env.MODEL_ID = 'fixture-model'
    expect(install('codex', env).status).toBe(0)
    const path = join(env.CODEX_HOME, 'config.toml')
    const config = JSON.parse(execFileSync('python3', ['-c', 'import json,sys,tomllib; print(json.dumps(tomllib.load(open(sys.argv[1], "rb"))))', path], { env, encoding: 'utf8' }))
    expect(config.model_provider).toBe('sub2api')
    expect(config.model).toBe('fixture-model')
    expect(config.model_providers.sub2api).toEqual({ name: 'sub2api', base_url: 'https://gateway.example.test/v1', env_key: 'SUB2API_API_KEY', wire_api: 'responses', requires_openai_auth: false, supports_websockets: false })
    expect(readFileSync(path, 'utf8')).not.toContain(fixtureKey)
    expect(statSync(path).mode & 0o777).toBe(0o600)
  })

  it('retains an existing provider and profiles byte-for-byte and refuses to overwrite a candidate', () => {
    const { env } = fixture()
    mkdirSync(env.CODEX_HOME)
    const path = join(env.CODEX_HOME, 'config.toml')
    const original = 'model_provider = "KDAN"\nprofile = "work"\n[model_providers.KDAN]\nenv_key = "KDAN_API_KEY"\n[profiles.work]\nmodel_provider = "custom"\n'
    writeFileSync(path, original)
    expect(install('codex', env).status).toBe(0)
    const candidate = join(env.CODEX_HOME, 'config.sub2api.toml')
    const proposed = readFileSync(candidate, 'utf8')
    expect(proposed).toContain('model_provider = "sub2api"')
    expect(readFileSync(path, 'utf8')).toBe(original)
    expect(install('codex', env).status).not.toBe(0)
    expect(readFileSync(candidate, 'utf8')).toBe(proposed)
    expect(readFileSync(path, 'utf8')).toBe(original)
  })

  it('does not follow an existing configuration symlink when writing', () => {
    const { directory, env } = fixture()
    mkdirSync(env.CODEX_HOME)
    const target = join(directory, 'managed.toml')
    writeFileSync(target, 'model_provider = "managed"\n')
    symlinkSync(target, join(env.CODEX_HOME, 'config.toml'))
    expect(install('codex', env).status).toBe(0)
    expect(readFileSync(target, 'utf8')).toBe('model_provider = "managed"\n')
    expect(existsSync(join(env.CODEX_HOME, 'config.sub2api.toml'))).toBe(true)
  })

  it('retains native Claude fields and creates private files without printing credentials', () => {
    const { env } = fixture()
    expect(install('claude-code', env).status).toBe(0)
    const path = join(env.CLAUDE_CONFIG_DIR, 'settings.json')
    expect(JSON.parse(readFileSync(path, 'utf8'))).toEqual({ env: { ANTHROPIC_BASE_URL: 'https://gateway.example.test', ANTHROPIC_AUTH_TOKEN: fixtureKey } })
    expect(statSync(path).mode & 0o777).toBe(0o600)
  })

  it('preserves existing Claude credentials and privacy settings and writes only a review candidate', () => {
    const { env } = fixture()
    mkdirSync(env.CLAUDE_CONFIG_DIR)
    const path = join(env.CLAUDE_CONFIG_DIR, 'settings.json')
    const original = JSON.stringify({ env: { ANTHROPIC_BASE_URL: 'https://existing.example.test', ANTHROPIC_AUTH_TOKEN: 'old-fixture-token' }, permissions: { deny: ['Read(.env)'] }, enableAllProjectMcpServers: false })
    writeFileSync(path, original)
    expect(install('claude-code', env).status).toBe(0)
    expect(readFileSync(path, 'utf8')).toBe(original)
    const candidate = join(env.CLAUDE_CONFIG_DIR, 'settings.sub2api.json')
    const proposed = readFileSync(candidate, 'utf8')
    expect(JSON.parse(proposed).env.ANTHROPIC_AUTH_TOKEN).toBe(fixtureKey)
    expect(install('claude-code', env).status).not.toBe(0)
    expect(readFileSync(candidate, 'utf8')).toBe(proposed)
    expect(readFileSync(path, 'utf8')).toBe(original)
  })

  it.each(['codex', 'claude-code'] as const)('%s requires the new variables without adopting a legacy key', name => {
    const { env } = fixture()
    delete env.SUB2API_API_KEY
    env.KDAN_API_KEY = fixtureKey
    expect(install(name, env).status).not.toBe(0)
    expect(existsSync(env.CODEX_HOME)).toBe(false)
    expect(existsSync(env.CLAUDE_CONFIG_DIR)).toBe(false)
  })
})

describe('published download links', () => {
  it.each(['gpt-image-flare', 'gpt-image-sunburst'])('%s retains a historical provider and its explicit credential variable', name => {
    const { env } = fixture()
    delete env.SUB2API_BASE_URL
    env.KDAN_API_KEY = fixtureKey
    env.SUB2API_API_KEY = 'different-fixture-key'
    env.PYTHONDONTWRITEBYTECODE = '1'
    mkdirSync(env.CODEX_HOME)
    const path = join(env.CODEX_HOME, 'config.toml')
    const original = 'model_provider = "KDAN"\n[model_providers.KDAN]\nbase_url = "https://historical-provider.example.test/v1"\nenv_key = "KDAN_API_KEY"\n'
    writeFileSync(path, original)
    const script = join(publicRoot, 'downloads/sub2api-image-skills', name, 'scripts/generate.py')
    const check = 'import json,os,runpy,sys; m=runpy.run_path(sys.argv[1]); p=m["load_provider"](); assert p[3] == os.environ["KDAN_API_KEY"]; print(json.dumps(p[:3]))'
    const output = execFileSync('python3', ['-c', check, script], { env, encoding: 'utf8' })
    expect(JSON.parse(output)).toEqual(['KDAN', 'https://historical-provider.example.test/v1', {}])
    expect(output).not.toContain(fixtureKey)
    delete env.KDAN_API_KEY
    const missing = spawnSync('python3', ['-c', check, script], { env, encoding: 'utf8' })
    expect(missing.status).not.toBe(0)
    expect(missing.stderr).toContain('KDAN_API_KEY')
    expect(missing.stderr).not.toContain('different-fixture-key')
    expect(readFileSync(path, 'utf8')).toBe(original)
  })

  it('resolves every four-language download link to a current archive or checksum file', () => {
    const articles = import.meta.glob('../**/*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
    const links = new Set<string>()
    for (const [file, article] of Object.entries(articles)) {
      for (const match of article.matchAll(/\]\((\/downloads\/[^)]+)\)/g)) {
        expect(existsSync(join(publicRoot, match[1])), file + ': ' + match[1]).toBe(true)
        links.add(match[1])
      }
    }
    expect(links).toEqual(new Set(['/downloads/gpt-image-flare.zip', '/downloads/gpt-image-sunburst.zip', '/downloads/sub2api-codex-session-repair.zip', '/downloads/sub2api-claude-session-recovery.zip', '/downloads/SHA256SUMS.txt']))
  })
})
