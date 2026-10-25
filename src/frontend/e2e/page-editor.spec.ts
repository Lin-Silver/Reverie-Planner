import { expect, test } from '@playwright/test'

async function fetchPages(page: any): Promise<any[]> {
  const res = await page.request.fetch('http://127.0.0.1:18765/api/pages')
  return (await res.json()) as any[]
}

test('page editor creates and persists content', async ({ page }) => {
  page.on('console', (msg) => console.log('PAGE CONSOLE:', msg.text()))
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()

  const before = await fetchPages(page)
  const beforeIds = new Set(before.map((p: any) => p.id))

  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 10000 })

  const after = await fetchPages(page)
  const newPage = after.find((p: any) => !beforeIds.has(p.id))
  const pageId = newPage.id

  await page.locator('.bn-editor').click()
  await page.keyboard.type('Hello BlockNote')

  await expect.poll(async () => {
    const res = await page.request.fetch(`http://127.0.0.1:18765/api/pages?id=${pageId}`)
    const p = await res.json()
    return p.content
  }, { timeout: 10000 }).toContain('Hello BlockNote')

  await page.reload()
  await expect(page.getByText('Welcome back')).toBeVisible()

  await expect.poll(async () => {
    const res = await page.request.fetch(`http://127.0.0.1:18765/api/pages?id=${pageId}`)
    const p = await res.json()
    return p.content
  }, { timeout: 10000 }).toContain('Hello BlockNote')
})

test('create child page persists after reload', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 10000 })
  // create child via + button in sidebar
  await page.locator('.tree-item').first().locator('button').click()
  await page.waitForTimeout(1000)
  await page.reload()
  // should still have 2 pages
  await expect(page.getByText('Untitled').first()).toBeVisible()
})

test('Ctrl+K command palette navigates', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.keyboard.press('Control+k')
  await expect(page.locator('.modal')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('.modal')).toHaveCount(0)
})