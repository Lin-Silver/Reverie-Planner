import { expect, test } from '@playwright/test'

test('page editor creates and persists content', async ({ page }) => {
  page.on('console', (msg) => console.log('PAGE CONSOLE:', msg.text()))
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 10000 })
  await page.locator('.bn-editor').click()
  await page.keyboard.type('Hello BlockNote')
  await page.waitForTimeout(2000)
  await page.reload()
  await page.getByText('Untitled').first().click()
  await expect(page.getByText('Hello BlockNote')).toBeVisible({ timeout: 10000 })
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
