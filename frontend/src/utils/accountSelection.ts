interface AccountIDRow {
  id: number
}

interface AccountListPage {
  items: AccountIDRow[]
  total: number
  pages?: number
}

type AccountPageFetcher = (
  page: number,
  pageSize: number,
  filters: Record<string, unknown>
) => Promise<AccountListPage>

const SELECT_ALL_PAGE_SIZE = 1000

export async function fetchAllAccountIds(
  fetchPage: AccountPageFetcher,
  filters: Record<string, unknown>
): Promise<number[]> {
  const requestFilters = {
    ...filters,
    lite: '1',
    include_scheduler_score: '0'
  }
  const firstPage = await fetchPage(1, SELECT_ALL_PAGE_SIZE, requestFilters)
  const pageCount = Math.max(
    firstPage.pages ?? 0,
    Math.ceil(firstPage.total / SELECT_ALL_PAGE_SIZE)
  )
  const ids = firstPage.items.map(account => account.id)

  for (let page = 2; page <= pageCount; page++) {
    const result = await fetchPage(page, SELECT_ALL_PAGE_SIZE, requestFilters)
    ids.push(...result.items.map(account => account.id))
  }

  const uniqueIDs = Array.from(new Set(ids))
  if (uniqueIDs.length !== firstPage.total) {
    // 这个 Error 只进 console：唯一的调用方（AccountsView）catch 之后走
    // t('admin.accounts.bulkActions.selectAllFailed') 提示用户。message 保持
    // 非本地化的技术描述，避免简体文案在 zh-TW / en 环境下泄漏到界面。
    throw new Error(
      `account list result is incomplete: expected ${firstPage.total} ids, got ${uniqueIDs.length}`
    )
  }
  return uniqueIDs
}
