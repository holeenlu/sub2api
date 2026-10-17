import fs from 'node:fs/promises'
import path from 'node:path'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'

// Build-time only. Pinned official artifacts, with integrity checked before
// extraction. Never fetch or execute a remotely selected version at runtime.
const manifest = JSON.parse(await fs.readFile(new URL('./runtime.json', import.meta.url), 'utf8'))
const [arch, destination] = process.argv.slice(2)
const artifact = manifest[arch]
if (!artifact || !destination || !path.isAbsolute(destination)) throw new Error('Expected amd64/arm64 and an absolute destination')
const response = await fetch(artifact.url, { signal: AbortSignal.timeout(180000) })
if (!response.ok) throw new Error('Official CLI artifact download failed: ' + response.status)
const body = Buffer.from(await response.arrayBuffer())
const actual = 'sha512-' + createHash('sha512').update(body).digest('base64')
if (actual !== artifact.integrity) throw new Error('Official CLI integrity mismatch')
await fs.mkdir(destination, { recursive: true })
const archive = path.join(destination, 'official-cli.tgz')
await fs.writeFile(archive, body)
const entries = execFileSync('tar', ['-tzf', archive], { encoding: 'utf8' }).trim().split('\n')
const binary = entries.find(entry => /^package\/(?:bin\/)?claude$/.test(entry))
if (!binary) throw new Error('Official CLI package has no executable')
execFileSync('tar', ['-xzf', archive, '-C', destination, binary])
await fs.rename(path.join(destination, binary), path.join(destination, 'claude'))
await fs.chmod(path.join(destination, 'claude'), 0o755)
for (const license of entries.filter(entry => /^package\/(?:LICENSE|LICENSE\.md|LICENSE\.txt)$/.test(entry))) {
  execFileSync('tar', ['-xzf', archive, '-C', destination, license])
  await fs.copyFile(path.join(destination, license), path.join(destination, 'LICENSE'))
}
await fs.rm(archive)
await fs.rm(path.join(destination, 'package'), { recursive: true, force: true })
console.log('Verified official Claude Code ' + manifest.version + ' for ' + arch)
