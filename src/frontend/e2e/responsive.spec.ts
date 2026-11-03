import { expect, test } from '@playwright/test'

async function overflowX(page: any) {
  return page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
}

function newPageButton(page: any) {
  return page.getByRole('button', { name: /New page|Create a page/ }).first()
}

test('narrow viewport: home, page editor and database avoid horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 720 })
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  expect(await overflowX(page)).toBeLessThanOrEqual(1)

  await newPageButton(page).click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  expect(await overflowX(page)).toBeLessThanOrEqual(1)
  // Primary actions stay reachable without horizontal scrolling.
  for (const name of ['Full width', 'Add cover to Untitled']) {
    const control = page.getByRole('button', { name })
    await expect(control).toBeVisible()
    const box = await control.boundingBox()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(375)
  }

  await page.getByText('Create database').click()
  await expect(page.getByText('+ Property')).toBeVisible({ timeout: 20000 })
  expect(await overflowX(page)).toBeLessThanOrEqual(1)
})

test('narrow viewport: cover dialog fits and presets are reachable', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 720 })
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await newPageButton(page).click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  await page.getByRole('button', { name: 'Add cover to Untitled' }).click()
  const dialog = page.getByRole('dialog', { name: 'Page cover' })
  await expect(dialog).toBeVisible()
  const box = await dialog.boundingBox()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(375)
  const last = dialog.getByRole('button', { name: 'Use Ember cover' })
  await last.scrollIntoViewIfNeeded()
  await expect(last).toBeVisible()
})
