import { expect, test } from '@playwright/test'
import { COVER_PRESETS } from '../src/pageCoverPresets'

const API = 'http://127.0.0.1:18765'

async function pageByTitle(page: any, title: string) {
  const res = await page.request.fetch(`${API}/api/pages`)
  const pages = (await res.json()) as any[]
  return pages.find((p) => p.title === title)
}

test('page cover: preset saves, survives reload, and can be removed', async ({ page }) => {
  const title = 'Cover page test'
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

  await page.getByLabel('Page title').fill(title)
  await page.getByLabel('Page title').blur()

  await page.getByRole('button', { name: `Add cover to ${title}` }).click()
  const dialog = page.getByRole('dialog', { name: 'Page cover' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Use Dawn cover' }).click()
  await expect(dialog.locator('.cover-preview')).toBeVisible()
  await dialog.getByRole('button', { name: 'Save cover' }).click()
  await expect(dialog).toBeHidden()

  await expect(page.getByTestId('page-cover')).toBeVisible()
  await expect(page.getByTestId('page-cover')).toHaveAttribute('style', /linear-gradient/)

  const saved = await pageByTitle(page, title)
  expect(saved.cover).toBe(COVER_PRESETS[0].value)

  await page.reload()
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.locator('.home-page-list button').filter({ hasText: title }).first().click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  await expect(page.getByTestId('page-cover')).toBeVisible()

  await page.getByRole('button', { name: 'Remove cover' }).click()
  await expect(page.getByTestId('page-cover')).toHaveCount(0)
  await expect.poll(async () => (await pageByTitle(page, title))?.cover ?? '').toBe('')
})

test('page cover: rejects unsafe URLs without saving', async ({ page }) => {
  const title = 'Cover URL guard'
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  await page.getByLabel('Page title').fill(title)
  await page.getByLabel('Page title').blur()

  await page.getByRole('button', { name: `Add cover to ${title}` }).click()
  const dialog = page.getByRole('dialog', { name: 'Page cover' })
  await dialog.getByLabel('Cover image URL').fill('javascript:alert(1)')
  await dialog.getByRole('button', { name: 'Save cover' }).click()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('alert')).toContainText('Choose a preset')
  expect((await pageByTitle(page, title))?.cover ?? '').toBe('')

  // A same-origin attachment path is accepted.
  await dialog.getByLabel('Cover image URL').fill('/wallpapers/sakura.webp')
  await dialog.getByRole('button', { name: 'Save cover' }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('page-cover').locator('img')).toHaveAttribute('src', '/wallpapers/sakura.webp')
})
