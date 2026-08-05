export type PaginationItem = number | 'ellipsis'

export function paginationItems(currentPage: number, totalPages: number): PaginationItem[] {
  if (totalPages <= 0) return []
  if (totalPages <= 7) return Array.from({ length: totalPages }, (_, index) => index + 1)

  const pages = new Set([1, totalPages])
  for (let page = currentPage - 1; page <= currentPage + 1; page += 1) {
    if (page > 1 && page < totalPages) pages.add(page)
  }
  if (currentPage <= 4) {
    pages.add(2)
    pages.add(3)
    pages.add(4)
    pages.add(5)
  }
  if (currentPage >= totalPages - 3) {
    pages.add(totalPages - 1)
    pages.add(totalPages - 2)
    pages.add(totalPages - 3)
    pages.add(totalPages - 4)
  }

  const sorted = Array.from(pages).filter((page) => page > 0).sort((left, right) => left - right)
  const result: PaginationItem[] = []
  sorted.forEach((page, index) => {
    if (index > 0 && page - sorted[index - 1] > 1) result.push('ellipsis')
    result.push(page)
  })
  return result
}
