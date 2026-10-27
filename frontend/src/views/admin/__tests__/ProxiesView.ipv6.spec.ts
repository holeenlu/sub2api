import { describe, expect, it } from 'vitest'
import { parseProxyUrl, parseProxyBatch } from '@/components/common/ProxyBatchInput.vue'

describe('proxy batch URL parsing (IPv6 support)', () => {
  it.each([
    ['socks5://[2001:db8::1]:1080', true],
    ['socks5h://[2001:db8::1]:1080', true],
    ['http://[::1]:8080', true],
    ['socks5://user:pass@[2001:db8::1]:1080', true],
    ['socks5://proxy.example.com:1080', true],
    ['http://192.168.1.1:8080', true],
    ['socks5://user:pass@proxy.example.com:1080', true],
    // bare IPv6 without brackets is ambiguous with host:port — rejected
    ['socks5://2001:db8::1:1080', false],
    // unsupported schemes / malformed ports stay invalid
    ['ftp://example.com:21', false],
    ['socks5://example.com:port', false]
  ])('%s => %s', (line, expected) => {
    expect(parseProxyUrl(line) !== null).toBe(expected)
  })

  it('extracts bare IPv6 hosts while retaining credentials verbatim', () => {
    expect(parseProxyUrl('socks5h:// user : p%40ss:word @[2001:db8::1]:1080')).toEqual({
      protocol: 'socks5h', host: '2001:db8::1', port: 1080,
      username: ' user ', password: ' p%40ss:word '
    })
  })
  it.each(['http://example.com:0', 'http://example.com:65536'])(
    'rejects out of range ports: %s', line => expect(parseProxyUrl(line)).toBeNull()
  )
  it('counts duplicates without conflating credential delimiters', () => {
    const result = parseProxyBatch([
      'http://user:pass@proxy.example:8080',
      'http://user:pass@proxy.example:8080',
      'socks5h://user:pass@[2001:db8::1]:1080',
      'ftp://proxy.example:21', ''
    ].join('\n'))
    expect(result).toMatchObject({ total: 4, valid: 2, duplicate: 1, invalid: 1 })
  })
})
