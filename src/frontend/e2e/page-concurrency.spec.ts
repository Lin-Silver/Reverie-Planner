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
  await expect(item).toBeVisible({ timeout: 20000 })
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
    await pageB.keyboard.press('Control+a')
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
    await pageB.keyboard.press('Control+a')
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
    await pageB.keyboard.press('Control+a')
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
    await pageB.keyboard.press('Control+a')
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
    await pageB.keyboard.press('Control+a')
    await pageB.keyboard.type('Body from client B')

    // wait for autosave to produce 409
    await expect.poll(async () => {
      const p = await fetchPage(pageB, pageId)
      // still A's content
      return p.content
    }, { timeout: 20000 }).toContain('more from A')

    // Trigger parent re-render by toggling theme
    await pageB.locator('button').filter({ hasText: /🌙|☀️/ }).click()

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
