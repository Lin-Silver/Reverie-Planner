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
