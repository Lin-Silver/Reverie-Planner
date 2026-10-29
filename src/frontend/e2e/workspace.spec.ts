import { expect, test } from '@playwright/test'

test('projects and tasks persist through CRUD operations and reloads', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()

  await page.locator('.nav-item').filter({ hasText: 'Projects' }).click()
  await page.getByRole('button', { name: '+ New project' }).click()
  await page.locator('.modal').getByLabel('Name').fill('浏览器验收项目')
  await page.locator('.modal').getByLabel('Description').fill('真实 SQLite 端到端测试')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('浏览器验收项目')).toBeVisible()
  await page.reload()
  await page.locator('.nav-item').filter({ hasText: 'Projects' }).click()
  await expect(page.getByText('浏览器验收项目')).toBeVisible()

  await page.locator('.nav-item').filter({ hasText: 'Issues' }).click()
  await page.getByRole('button', { name: '+ New issue' }).click()
  await page.locator('.modal').getByLabel('Title').fill('持久化任务')
  await page.locator('.modal').getByLabel('Project').selectOption({ label: '浏览器验收项目' })
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('持久化任务')).toBeVisible()

  await page.reload()
  await page.locator('.nav-item').filter({ hasText: 'Issues' }).click()
  await expect(page.getByText('持久化任务')).toBeVisible()
})
