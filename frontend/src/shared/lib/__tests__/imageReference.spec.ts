import { describe, expect, it } from 'vitest'
import { sanitizeImageReference } from '../imageReference.mjs'
import { sanitizeUrl } from '../url'
import cases from './fixtures/imageReferences.json'

describe('image reference contract', () => {
  it.each(cases)('$name', ({ input, expected }) => {
    expect(sanitizeImageReference(input)).toBe(expected)
    expect(sanitizeUrl(input, { allowRelative: true, allowDataUrl: true })).toBe(expected)
  })

  it('keeps general navigation URL normalization separate from image references', () => {
    expect(sanitizeUrl('https://example.invalid')).toBe('https://example.invalid/')
    expect(sanitizeUrl('/relative')).toBe('')
    expect(sanitizeUrl('data:image/png;base64,cG5n')).toBe('')
  })
})
