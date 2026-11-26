import { expect, test } from '@playwright/test'

test('save page as template and create a new page from it', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()

  // Create a page with a title.
  await page.locator('.nav-item').filter({ hasText: 'New page' }).click()
  await page.locator('.page-title').fill('PlanDoc')

  // Save it as a template (name supplied via the prompt dialog).
  page.on('dialog', (d) => d.accept('MyTpl'))
  await page.getByTestId('save-template').click()
  await expect(page.getByTestId('templates-section')).toContainText('MyTpl')

  // Template persists across reload.
  await page.reload()
  const section = page.getByTestId('templates-section')
  await expect(section).toContainText('MyTpl')

  // Creating a page from the template opens the editor with the template title.
  await page.getByTestId('template-item').filter({ hasText: 'MyTpl' }).click()
  await expect(page.locator('.page-title')).toBeVisible()
  await expect(page.locator('.page-title')).toHaveValue('PlanDoc')
})