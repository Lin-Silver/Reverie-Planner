import { expect, test } from '@playwright/test'

// The e2e backend keeps one database for the whole run, so tests must not
// assume empty global favorites/recent state; assertions are scoped to the
// page each test creates.

test('favorites and recent pages persist and populate the sidebar', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()

  // Create a page and give it a title (blur on the favorite click saves it).
  await page.locator('.nav-item').filter({ hasText: 'New page' }).click()
  await expect(page.getByTestId('favorite-toggle')).toBeVisible()
  await page.locator('.page-title').fill('FavDoc')
  await page.getByTestId('favorite-toggle').click()
  await expect(page.getByTestId('favorite-toggle')).toHaveText('★ Favorited')

  // Title + favorite persist across a reload; the section lists the page.
  await page.reload()
  const favSection = page.getByTestId('favorites-section')
  await expect(favSection).toContainText('FavDoc')

  // Opening the page records it in Recent.
  await favSection.getByText('FavDoc').click()
  await expect(page.getByTestId('recent-section')).toContainText('FavDoc')

  // Clearing recent removes the section.
  await page.getByTestId('clear-recent').click()
  await expect(page.getByTestId('recent-section')).toHaveCount(0)
})

test('favorite toggle removes a page from favorites', async ({ page }) => {
  await page.goto('/')
  await page.locator('.nav-item').filter({ hasText: 'New page' }).click()
  await page.locator('.page-title').fill('TempDoc')
  await page.getByTestId('favorite-toggle').click()
  await expect(page.getByTestId('favorite-toggle')).toHaveText('★ Favorited')
  await page.getByTestId('favorite-toggle').click()
  await expect(page.getByTestId('favorite-toggle')).toHaveText('☆ Favorite')

  // The unfavorited page is gone from the section (other favorites may remain).
  await page.reload()
  const favSection = page.getByTestId('favorites-section')
  if (await favSection.count()) {
    await expect(favSection).not.toContainText('TempDoc')
  }
})