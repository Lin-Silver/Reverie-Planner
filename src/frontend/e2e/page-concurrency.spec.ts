import { test, expect } from '@playwright/test'

const BASE = 'http://127.0.0.1:18765'

async function fetchPages(page: any): Promise<any[]> {
  const res = await page.request.fetch(`${BASE}/api/pages`)
  expect(res.ok, `GET /api/pages failed: ${res.status()}`).toBeTruthy()
  return (await res.json()) as any[]
}

async function fetchPage(page: any, id: string): Promise<any> {
  const res = await page.request.fetch(`${BASE}/api/pages?id=${id}`)
  expect(res.ok, `GET /api/pages?id=${id} failed: ${res.status()}`).toBeTruthy()
  return (await res.json()) as any
}

async function openPageByTitle(page: any, title: string) {
  const item = page.locator('.tree-item').filter({ hasText: title }).first()
  try {
    await expect(item).toBeVisible({ timeout: 8000 })
  } catch {
    // A second browser has no live page-list subscription. When its initial
    // sidebar snapshot misses a page that the API already persisted, reload
    // once to fetch a fresh snapshot; still require the page to be visible.
    const pages = await fetchPages(page)
    expect(pages.some((p: any) => p.title === title), 'page must exist on server before retrying sidebar load').toBeTruthy()
    await page.reload()
    await expect(item).toBeVisible({ timeout: 20000 })
  }
  await item.click()
}

async function waitForAutosave(page: any) {
  await page.waitForTimeout(2500)
}

async function createPageWithTitle(page: any, title: string): Promise<string> {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  // 通过标题输入框设置唯一标题，失焦触发保存
  await page.locator('.page-title').fill(title)
  await page.locator('.bn-editor').click()

  await expect.poll(async () => {
    const pages = await fetchPages(page)
    const found = pages.find((p: any) => p.title === title)
    return found ? found.id : null
  }, { timeout: 20000 }).toBeTruthy()

  const pages = await fetchPages(page)
  const testPage = pages.find((p: any) => p.title === title)
  expect(testPage).toBeTruthy()
  return testPage.id
}

test('concurrency: Reload latest keeps server content, discards stale draft', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()

    const uniqueTitle = `Conc-Reload-${Date.now()}`
    const pageId = await createPageWithTitle(pageA, uniqueTitle)

    // 客户端 B: 在 A 保存正文之前打开同一页面 (version=1)
    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, uniqueTitle)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // 客户端 A: 写入内容 A 并自动保存 (version -> 2)
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type('Body from client A')

    await expect.poll(async () => {
      const p = await fetchPage(pageA, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('Body from client A')

    // 客户端 B: 写入内容 B，基于陈旧版本自动保存 -> 触发 409
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.press('ControlOrMeta+a')
    await pageB.keyboard.type('Body from client B')

    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })

    // B 点击 Reload latest
    await pageB.getByText('Reload latest').click()
    await pageB.waitForTimeout(2000)

    // 数据库必须保留 A 的正文，B 的旧草稿不得回写
    const reloaded = await fetchPage(pageB, pageId)
    expect(reloaded.content).toContain('Body from client A')
    expect(reloaded.content).not.toContain('Body from client B')
    expect(reloaded.version).toBeGreaterThanOrEqual(2)

    // 重新加载后 BlockNote 内容应为 A 的正文，状态为 saved
    await expect(pageB.locator('.bn-editor')).toContainText('Body from client A', { timeout: 20000 })
    await expect(pageB.getByText('saved')).toBeVisible()

    await contextA.close()
    await contextB.close()
  } finally {
    if (!contextA.isClosed()) await contextA.close()
    if (!contextB.isClosed()) await contextB.close()
  }
})

test('concurrency: Retry my changes saves client content', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()

    const uniqueTitle = `Conc-Retry-${Date.now()}`
    const pageId = await createPageWithTitle(pageA, uniqueTitle)

    // B 在 A 保存正文前打开同一页面
    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, uniqueTitle)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // A 保存内容 A
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type('Body from client A')

    await expect.poll(async () => {
      const p = await fetchPage(pageA, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('Body from client A')

    // B 写入内容 B，触发 409
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.press('ControlOrMeta+a')
    await pageB.keyboard.type('Body from client B')
    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })

    // B 选择 Retry my changes -> 最终正文为 B 的内容
    await pageB.getByText('Retry my changes').click()
    await pageB.waitForTimeout(3000)

    const retried = await fetchPage(pageB, pageId)
    expect(retried.content).toContain('Body from client B')
    expect(retried.content).not.toContain('Body from client A')
    expect(retried.version).toBeGreaterThanOrEqual(3)

    await contextA.close()
    await contextB.close()
  } finally {
    if (!contextA.isClosed()) await contextA.close()
    if (!contextB.isClosed()) await contextB.close()
  }
})

test('concurrency: metadata protected after Retry and continuous autosave', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()

    const uniqueTitle = `Conc-Meta-${Date.now()}`
    const pageId = await createPageWithTitle(pageA, uniqueTitle)

    // B 在 A 修改元数据前打开同一页面 (缓存旧元数据)
    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, uniqueTitle)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // A 修改标题并保存
    const newTitle = `Modified by A ${Date.now()}`
    await pageA.locator('.page-title').fill(newTitle)
    await pageA.locator('.bn-editor').click() // blur title -> save

    await expect.poll(async () => {
      const p = await fetchPage(pageA, pageId)
      return p.title
    }, { timeout: 20000 }).toBe(newTitle)

    const afterMeta = await fetchPage(pageA, pageId)
    expect(afterMeta.title).toBe(newTitle)

    // B 基于陈旧版本写入内容，触发 409
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.press('ControlOrMeta+a')
    await pageB.keyboard.type('Body from client B')
    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })

    // B 选择 Retry my changes：使用服务器最新元数据 (newTitle)，仅替换正文
    await pageB.getByText('Retry my changes').click()
    await pageB.waitForTimeout(3000)

    // Retry 后 B 继续编辑，连续自动保存不得恢复旧元数据
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.type(' extra text')

    await expect.poll(async () => {
      const p = await fetchPage(pageB, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('extra text')

    const final = await fetchPage(pageB, pageId)
    expect(final.title).toBe(newTitle)
    expect(final.title).not.toBe(uniqueTitle)
    expect(final.content).toContain('Body from client B')
    expect(final.version).toBeGreaterThanOrEqual(4)

    await contextA.close()
    await contextB.close()
  } finally {
    if (!contextA.isClosed()) await contextA.close()
    if (!contextB.isClosed()) await contextB.close()
  }
})

test('navigation: unsaved content persists after leaving before debounce', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()

  const beforePages = await fetchPages(page)
  const beforeIds = new Set(beforePages.map((p: any) => p.id))

  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

  const afterPages = await fetchPages(page)
  const newPage = afterPages.find((p: any) => !beforeIds.has(p.id))
  const pageId = newPage.id

  await page.locator('.bn-editor').click()
  await page.keyboard.type('Unsaved navigation content')

  await page.locator('.nav-item').filter({ hasText: 'Home' }).click()

  await expect.poll(async () => {
    const p = await fetchPage(page, pageId)
    return p.content
  }, { timeout: 20000 }).toContain('Unsaved navigation content')
})

test('navigation: conflict on leave preserves draft and shows failure', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()

    const uniqueTitle = `NavConflict-${Date.now()}`
    const pageId = await (async () => {
      await pageA.goto('/')
      await expect(pageA.getByText('Welcome back')).toBeVisible()
      const before = await fetchPages(pageA)
      const beforeIds = new Set(before.map((p: any) => p.id))
      await pageA.getByText('New page').click()
      await expect(pageA.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
      await pageA.locator('.page-title').fill(uniqueTitle)
      await pageA.locator('.bn-editor').click()
      await expect.poll(async () => {
        const pages = await fetchPages(pageA)
        const found = pages.find((p: any) => p.title === uniqueTitle)
        return found ? found.id : null
      }, { timeout: 20000 }).toBeTruthy()
      const pages = await fetchPages(pageA)
      return pages.find((p: any) => p.title === uniqueTitle).id
    })()

    // B opens the page before A saves body (so B caches a stale version)
    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, uniqueTitle)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // A saves body content (version advances)
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type('Body from client A')
    await expect.poll(async () => {
      const p = await fetchPage(pageA, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('Body from client A')

    // B types new content (unsaved) then attempts to navigate -> conflict
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.press('ControlOrMeta+a')
    await pageB.keyboard.type('Body from client B')

    await pageB.locator('.nav-item').filter({ hasText: 'Home' }).click()

    await expect(pageB.locator('.bn-editor')).toContainText('Body from client B')
    await expect(pageB.getByText(/Save failed|Version conflict/).first()).toBeVisible({ timeout: 20000 })

    // Wait through debounce (1s) + retry window (3s) + margin to ensure no auto-save overwrites
    await pageB.waitForTimeout(5000)

    const mid = await fetchPage(pageB, pageId)
    expect(mid.content).toContain('Body from client A')
    expect(mid.content).not.toContain('Body from client B')

    await expect(pageB.locator('.bn-editor')).toContainText('Body from client B')

    // Explicitly Retry -> B's content replaces
    await pageB.getByText('Retry my changes').click()
    await expect.poll(async () => {
      const p = await fetchPage(pageB, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('Body from client B')

    await contextA.close()
    await contextB.close()
  } finally {
    if (!contextA.isClosed()) await contextA.close()
    if (!contextB.isClosed()) await contextB.close()
  }
})
test('conflict lock survives parent rerender and keeps draft', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()

    const uniqueTitle = `LockRerender-${Date.now()}`
    const pageId = await (async () => {
      await pageA.goto('/')
      await expect(pageA.getByText('Welcome back')).toBeVisible()
      const before = await fetchPages(pageA)
      const beforeIds = new Set(before.map((p: any) => p.id))
      await pageA.getByText('New page').click()
      await expect(pageA.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
      await pageA.locator('.page-title').fill(uniqueTitle)
      await pageA.locator('.bn-editor').click()
      await expect.poll(async () => {
        const pages = await fetchPages(pageA)
        const found = pages.find((p: any) => p.title === uniqueTitle)
        return found ? found.id : null
      }, { timeout: 20000 }).toBeTruthy()
      const pages = await fetchPages(pageA)
      return pages.find((p: any) => p.title === uniqueTitle).id
    })()

    // A saves body content
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type('Body from client A')
    await expect.poll(async () => {
      const p = await fetchPage(pageA, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('Body from client A')

    // B opens page before A saves (stale version) - already opened after A? reorder: open now (A already saved, version current)
    // To make B stale, open before A's body save is not possible here; instead B opens now and we rely on autosave conflict from a forced stale version by not refetching.
    // Simpler: B opens the page now (current version), types, then we manually stale by reloading A? Instead, use the standard stale path: open B before A saves body.
    // Redo ordering: reopen flow not needed; instead verify lock behavior with a 409 triggered by B typing on stale cached page.
    // For this test, B must have a stale version. Achieve by opening B before A's body save: move B open above. (restructuring below)
    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, uniqueTitle)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // B types (autosave will conflict because B's cached version may be current; to force conflict, ensure A advanced version after B opened)
    // A already saved body before B opened, so B's version is current -> no conflict. Adjust: have A save again after B opens.
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type(' more from A')
    await expect.poll(async () => {
      const p = await fetchPage(pageA, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('more from A')

    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.press('ControlOrMeta+a')
    await pageB.keyboard.type('Body from client B')

    // wait for autosave to produce 409
    await expect.poll(async () => {
      const p = await fetchPage(pageB, pageId)
      // still A's content
      return p.content
    }, { timeout: 20000 }).toContain('more from A')

    // Trigger parent re-render by toggling theme
    await pageB.getByRole('button', {name: /Switch to .* theme/}).click()

    // Wait through debounce + retry
    await pageB.waitForTimeout(5000)

    const mid = await fetchPage(pageB, pageId)
    expect(mid.content).toContain('more from A')
    expect(mid.content).not.toContain('Body from client B')

    await expect(pageB.locator('.bn-editor')).toContainText('Body from client B')

    // Explicit Retry -> B replaces
    await pageB.getByText('Retry my changes').click()
    await expect.poll(async () => {
      const p = await fetchPage(pageB, pageId)
      return p.content
    }, { timeout: 20000 }).toContain('Body from client B')

    await contextA.close()
    await contextB.close()
  } finally {
    if (!contextA.isClosed()) await contextA.close()
    if (!contextB.isClosed()) await contextB.close()
  }
})

test('autosave never acknowledges an older write while a newer BlockNote draft is pending', async ({ page }) => {
  const title = `PendingAutosave-${Date.now()}`
  const pageId = await createPageWithTitle(page, title)
  const status = page.getByTestId('page-save-status')
  await expect(status).toContainText('saved')

  let firstSeen = false
  let secondSeen = false
  let releaseFirst: (() => void) | undefined
  let releaseSecond: (() => void) | undefined

  // Hold each PUT separately to reproduce a response for the older document
  // arriving after the user has already typed a newer draft.
  await page.route('**/api/pages', async (route) => {
    if (route.request().method() !== 'PUT') {
      await route.continue()
      return
    }
    const body = route.request().postDataJSON() as { content?: string }
    if (body.content?.includes('First save') && !body.content.includes('Second change') && !firstSeen) {
      firstSeen = true
      await new Promise<void>((resolve) => { releaseFirst = resolve })
    } else if (body.content?.includes('Second change') && !secondSeen) {
      secondSeen = true
      await new Promise<void>((resolve) => { releaseSecond = resolve })
    }
    await route.continue()
  })

  await page.evaluate(() => {
    const element = document.querySelector('[data-testid="page-save-status"]')
    if (!element) throw new Error('save status is missing')
    const history: string[] = []
    ;(window as Window & { __pageSaveHistory?: string[] }).__pageSaveHistory = history
    new MutationObserver(() => history.push(element.textContent || '')).observe(element, {
      childList: true, characterData: true, subtree: true,
    })
  })

  try {
    await page.locator('.bn-editor').click()
    await page.keyboard.type('First save')
    await expect.poll(() => firstSeen, { timeout: 15000 }).toBe(true)

    await page.keyboard.type(' Second change')
    await expect(status).toContainText('unsaved')
    releaseFirst?.()

    // The second PUT is blocked, so the UI must not claim that all edits
    // have been saved after only the first PUT completed.
    await expect.poll(() => secondSeen, { timeout: 15000 }).toBe(true)
    const history = await page.evaluate(() =>
      (window as Window & { __pageSaveHistory?: string[] }).__pageSaveHistory || []
    )
    expect(history).not.toContain('Status: saved')
    await expect(status).toContainText('saving')

    releaseSecond?.()
    await expect.poll(async () => (await fetchPage(page, pageId)).content, { timeout: 20000 })
      .toContain('Second change')
    await expect(status).toContainText('saved')
  } finally {
    releaseFirst?.()
    releaseSecond?.()
    await page.unroute('**/api/pages')
  }
})

test('concurrency: edits typed during explicit Retry are not silently acknowledged or discarded', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  let releaseRetry: (() => void) | undefined
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()
    const title = `RetryInFlight-${Date.now()}`
    const pageId = await createPageWithTitle(pageA, title)

    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, title)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // Advance A's version after B has opened the page.
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type('Server change')
    await expect.poll(async () => (await fetchPage(pageA, pageId)).content, { timeout: 20000 })
      .toContain('Server change')

    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.press('ControlOrMeta+a')
    await pageB.keyboard.type('Retry draft')
    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })

    let retrySeen = false
    await pageB.route('**/api/pages', async route => {
      if (route.request().method() !== 'PUT') {
        await route.continue()
        return
      }
      const body = route.request().postDataJSON() as { content?: string }
      if (body.content?.includes('Retry draft') && !body.content.includes('after retry') && !retrySeen) {
        retrySeen = true
        await new Promise<void>(resolve => { releaseRetry = resolve })
      }
      await route.continue()
    })

    await pageB.getByRole('button', { name: 'Retry my changes' }).click()
    await expect.poll(() => retrySeen, { timeout: 15000 }).toBe(true)
    await expect(pageB.getByRole('button', { name: 'Retrying…' })).toBeDisabled()

    // This edit happens after the Retry snapshot was captured but before it commits.
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.type(' after retry')
    await expect(pageB.locator('.bn-editor')).toContainText('Retry draft after retry')
    releaseRetry?.()

    // The first Retry must not mark the newer draft saved. The follow-up
    // autosave must persist the entire latest document, not the old snapshot.
    // BlockNote can represent consecutive typing as separate paragraph blocks.
    // Assert the complete ordered text across blocks, not raw JSON adjacency.
    await expect.poll(async () => {
      const saved = await fetchPage(pageB, pageId)
      const blocks = JSON.parse(saved.content) as Array<{ content?: Array<{ text?: string }> }>
      return blocks.flatMap(block => block.content || []).map(span => span.text || '').join('')
    }, { timeout: 20000 }).toBe('Retry draft after retry')
    await expect(pageB.getByTestId('page-save-status')).toContainText('saved', { timeout: 20000 })
    const persisted = await fetchPage(pageB, pageId)
    expect(persisted.content).not.toContain('Server change')
    expect(persisted.version).toBeGreaterThanOrEqual(4)
  } finally {
    releaseRetry?.()
    await contextA.close()
    await contextB.close()
  }
})


test('concurrency: stale title save exposes 409 and Retry preserves the local title', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()
    const original = `TitleConflict-${Date.now()}`
    const remoteTitle = `Remote title ${Date.now()}`
    const localTitle = `Local title ${Date.now()}`
    const pageId = await createPageWithTitle(pageA, original)

    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, original)
    await expect(pageB.locator('.page-title')).toHaveValue(original)

    // Advance both the server body and title while B has the old snapshot.
    // A title-only Retry must preserve A's newer body, not replay B's stale body.
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type('Remote body survives title retry')
    await expect.poll(async () => (await fetchPage(pageA, pageId)).content, { timeout: 20000 })
      .toContain('Remote body survives title retry')
    await pageA.locator('.page-title').fill(remoteTitle)
    await pageA.locator('.bn-editor').click()
    await expect.poll(async () => (await fetchPage(pageA, pageId)).title, { timeout: 20000 })
      .toBe(remoteTitle)

    // A title-only conflict must not be swallowed by the input's blur handler.
    await pageB.locator('.page-title').fill(localTitle)
    await pageB.locator('.bn-editor').click()
    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })
    await expect(pageB.locator('.page-title')).toHaveValue(localTitle)
    expect((await fetchPage(pageB, pageId)).title).toBe(remoteTitle)

    // Neither a second blur nor navigating away may silently retry a stale
    // title write before the user explicitly resolves the conflict.
    await pageB.locator('.page-title').focus()
    await pageB.locator('.bn-editor').click()
    await pageB.locator('.nav-item').filter({ hasText: 'Home' }).click()
    await expect(pageB.locator('.page-title')).toHaveValue(localTitle)
    await expect(pageB.getByText('Version conflict')).toBeVisible()
    expect((await fetchPage(pageB, pageId)).title).toBe(remoteTitle)

    // Explicit Retry applies B's title using the current server version.
    await pageB.getByRole('button', { name: 'Retry my changes' }).click()
    await expect.poll(async () => (await fetchPage(pageB, pageId)).title, { timeout: 20000 })
      .toBe(localTitle)
    await expect(pageB.locator('.page-title')).toHaveValue(localTitle)
    await expect(pageB.getByTestId('page-save-status')).toContainText('saved')
    const persisted = await fetchPage(pageB, pageId)
    expect(persisted.content).toContain('Remote body survives title retry')
    await expect(pageB.locator('.bn-editor')).toContainText('Remote body survives title retry')
    expect(persisted.version).toBeGreaterThanOrEqual(4)
  } finally {
    await contextA.close()
    await contextB.close()
  }
})

test('concurrency: Reload latest discards a conflicting title draft', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()
    const original = `TitleReload-${Date.now()}`
    const remoteTitle = `Remote reload ${Date.now()}`
    const localTitle = `Discarded draft ${Date.now()}`
    const pageId = await createPageWithTitle(pageA, original)

    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, original)
    await expect(pageB.locator('.page-title')).toHaveValue(original)

    await pageA.locator('.page-title').fill(remoteTitle)
    await pageA.locator('.bn-editor').click()
    await expect.poll(async () => (await fetchPage(pageA, pageId)).title, { timeout: 20000 })
      .toBe(remoteTitle)

    await pageB.locator('.page-title').fill(localTitle)
    await pageB.locator('.bn-editor').click()
    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })
    await expect(pageB.locator('.page-title')).toHaveValue(localTitle)

    await pageB.getByRole('button', { name: 'Reload latest' }).click()
    await expect(pageB.locator('.page-title')).toHaveValue(remoteTitle, { timeout: 20000 })
    await expect(pageB.getByTestId('page-save-status')).toContainText('saved')
    expect((await fetchPage(pageB, pageId)).title).toBe(remoteTitle)
  } finally {
    await contextA.close()
    await contextB.close()
  }
})


test('concurrency: body typed during title-only Retry requires explicit resolution', async ({ browser }) => {
  const contextA = await browser.newContext()
  const contextB = await browser.newContext()
  let releaseTitleRetry: (() => void) | undefined
  try {
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()
    const original = `TitleRetryDraft-${Date.now()}`
    const localTitle = `Local title ${Date.now()}`
    const remoteBody = 'Remote body must survive title-only retry'
    const localBody = 'New body typed during title retry'
    const pageId = await createPageWithTitle(pageA, original)

    await pageB.goto('/')
    await expect(pageB.getByText('Welcome back')).toBeVisible()
    await openPageByTitle(pageB, original)
    await expect(pageB.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

    // B holds an old version while A changes the body.
    await pageA.locator('.bn-editor').click()
    await pageA.keyboard.type(remoteBody)
    await expect.poll(async () => (await fetchPage(pageA, pageId)).content, { timeout: 20000 })
      .toContain(remoteBody)

    await pageB.locator('.page-title').fill(localTitle)
    await pageB.locator('.bn-editor').click()
    await expect(pageB.getByText('Version conflict')).toBeVisible({ timeout: 20000 })

    let titleRetrySeen = false
    await pageB.route('**/api/pages', async route => {
      if (route.request().method() !== 'PUT') {
        await route.continue()
        return
      }
      const body = route.request().postDataJSON() as { title?: string; content?: string }
      if (body.title === localTitle && body.content?.includes(remoteBody) && !titleRetrySeen) {
        titleRetrySeen = true
        await new Promise<void>(resolve => { releaseTitleRetry = resolve })
      }
      await route.continue()
    })

    await pageB.getByRole('button', { name: 'Retry my changes' }).click()
    await expect.poll(() => titleRetrySeen, { timeout: 15000 }).toBe(true)
    await expect(pageB.getByRole('button', { name: 'Retrying…' })).toBeDisabled()

    // The first Retry captured only the title. A new body draft must not be
    // silently submitted with the stale editor content after it completes.
    await pageB.locator('.bn-editor').click()
    await pageB.keyboard.type(localBody)
    await expect(pageB.locator('.bn-editor')).toContainText(localBody)
    releaseTitleRetry?.()

    await expect.poll(async () => (await fetchPage(pageB, pageId)).title, { timeout: 20000 })
      .toBe(localTitle)
    await expect(pageB.getByRole('button', { name: 'Retry my changes' })).toBeEnabled({ timeout: 20000 })
    await expect(pageB.getByTestId('page-save-status')).toContainText('Version conflict')
    await pageB.waitForTimeout(1800)
    const afterTitleRetry = await fetchPage(pageB, pageId)
    expect(afterTitleRetry.content).toContain(remoteBody)
    expect(afterTitleRetry.content).not.toContain(localBody)
    await expect(pageB.locator('.bn-editor')).toContainText(localBody)

    // A second deliberate Retry now includes the local body draft.
    await pageB.getByRole('button', { name: 'Retry my changes' }).click()
    await expect.poll(async () => (await fetchPage(pageB, pageId)).content, { timeout: 20000 })
      .toContain(localBody)
    const resolved = await fetchPage(pageB, pageId)
    expect(resolved.title).toBe(localTitle)
    expect(resolved.content).not.toContain(remoteBody)
    await expect(pageB.getByTestId('page-save-status')).toContainText('saved', { timeout: 20000 })
  } finally {
    releaseTitleRetry?.()
    await contextA.close()
    await contextB.close()
  }
})
