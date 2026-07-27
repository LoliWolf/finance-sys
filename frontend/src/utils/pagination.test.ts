import { describe, expect, it } from 'vitest'
import { paginationItems } from './pagination'

describe('paginationItems', () => {
  it('shows every page for a short result set', () => {
    expect(paginationItems(2, 4)).toEqual([1, 2, 3, 4])
  })

  it('keeps the current page and both boundaries for a long result set', () => {
    expect(paginationItems(87, 174)).toEqual([1, 'ellipsis', 86, 87, 88, 'ellipsis', 174])
  })

  it('expands pages near the beginning and end', () => {
    expect(paginationItems(1, 10)).toEqual([1, 2, 3, 4, 5, 'ellipsis', 10])
    expect(paginationItems(10, 10)).toEqual([1, 'ellipsis', 6, 7, 8, 9, 10])
  })
})
