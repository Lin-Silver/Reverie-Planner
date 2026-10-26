import { expect, test } from '@playwright/test'

test('database: create, add property and record, edit cell, switch views', async ({ page }) => {
  page.on('dialog', async (d) => {
    if (d.message().toLowerCase().includes('name')) await d.accept('Status')
    else await d.accept('text')
  })
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })

  await page.getByText('Create database').click()
  await expect(page.getByText('+ Property')).toBeVisible({ timeout: 20000 })

  await page.getByText('+ Property').click()
  await expect(page.getByRole('dialog', {name:'New database property'})).toBeVisible()
  await page.getByLabel('Property name').fill('Status')
  await page.getByLabel('Property type').selectOption('text')
  await page.getByRole('button',{name:'Create property'}).click()
  await expect(page.getByRole('dialog', {name:'New database property'})).toBeHidden()

  await page.getByText('+ New').click()
  await page.waitForTimeout(300)

  const titleInput = page.locator('table input').first()
  await titleInput.fill('My record')
  await titleInput.blur()
  await expect(titleInput).toHaveValue('My record')

  await page.getByText('+ View').click()
  await page.waitForTimeout(300)
})