import { describe, expect, it } from 'vitest'
import { readExcelBPSModels } from '../excelBPSModels'
describe('account BPS explicit selections', () => {
  it.each([undefined, null, 'model', {}, [], [42, false, null, '', '  ']])('never infers a selection from %j', value => {
    expect(readExcelBPSModels(value)).toEqual([])
  })
  it('retains only explicit strings and returns an independent selection', () => {
    const input = Object.freeze([' a ', 42, 'a', 'b', false])
    const models = readExcelBPSModels(input)
    expect(models).toEqual(['a', 'b'])
    models.push('c')
    expect(input).toEqual([' a ', 42, 'a', 'b', false])
  })
})
