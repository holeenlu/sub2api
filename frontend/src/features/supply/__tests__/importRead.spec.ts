import { describe, expect, it } from 'vitest'
import { readImport } from '../helpers'
describe('supplier import file ownership', () => {
  it('rejects cancelled reads and retains the size boundary before parsing', async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(readImport(new File(['private import'], 'supplier.json'), 'size limit', controller.signal))
      .rejects.toMatchObject({ name: 'AbortError' })
    await expect(readImport(new File(['x'.repeat(5 * 1024 * 1024 + 1)], 'large.json'), 'size limit'))
      .rejects.toThrow('size limit')
  })
  it('aborts an in-flight FileReader without returning its private data', async () => {
    const controller = new AbortController()
    const pending = readImport(new File(['private import'], 'supplier.json'), 'size limit', controller.signal)
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await rejected
  })
})
