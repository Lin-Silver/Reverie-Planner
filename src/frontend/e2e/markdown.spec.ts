import { expect, test } from '@playwright/test'

test('import markdown creates a page with rendered blocks', async ({ page }) => {
  await page.goto('/')
  await page.locator('.nav-item').filter({ hasText: 'New page' }).click()
  const input = page.getByTestId('import-input')
  await expect(input).toBeAttached()
  const md = '# Hello\n\nThis is imported.\n\n- one\n- two\n'
  await input.setInputFiles({ name: 'Imported.md', mimeType: 'text/markdown', buffer: Buffer.from(md) })
  await expect(page.locator('.bn-editor')).toContainText('Hello', { timeout: 15000 })
  await expect(page.locator('.bn-editor')).toContainText('This is imported.')
})

test('export markdown triggers a download', async ({ page }) => {
  await page.goto('/')
  await page.locator('.nav-item').filter({ hasText: 'New page' }).click()
  await page.locator('.page-title').fill('ExportDoc')
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByTestId('export-md').click(),
  ])
  expect(download.suggestedFilename()).toBe('ExportDoc.md')
})