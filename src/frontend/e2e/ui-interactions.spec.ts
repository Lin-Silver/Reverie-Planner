import { expect, test } from '@playwright/test'

test('collapsible sidebar and task dialog interactions work', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()

  const sidebar = page.locator('aside.sidebar')
  await expect(sidebar).not.toHaveClass(/collapsed/)
  await page.getByRole('button', { name: '☰' }).click()
  await expect(sidebar).toHaveClass(/collapsed/)
  await page.getByRole('button', { name: '☰' }).click()
  await expect(sidebar).not.toHaveClass(/collapsed/)

  await page.locator('.nav-item').filter({ hasText: 'Issues' }).click()
  await page.getByRole('button', { name: '+ New issue' }).click()
  const dialog = page.getByRole('dialog').or(page.locator('.modal'))
  await expect(page.locator('.modal')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('.modal')).toHaveCount(0)
})

test('task drag and drop updates status', async ({ page }) => {
  await page.goto('/')
  await page.locator('.nav-item').filter({ hasText: 'Projects' }).click()
  await page.getByRole('button', { name: '+ New project' }).click()
  await page.locator('.modal').getByLabel('Name').fill('拖拽测试项目')
  await page.getByRole('button', { name: 'Save' }).click()

  await page.locator('.nav-item').filter({ hasText: 'Issues' }).click()
  await page.getByRole('button', { name: '+ New issue' }).click()
  await page.locator('.modal').getByLabel('Title').fill('拖拽任务')
  await page.locator('.modal').getByLabel('Project').selectOption({ label: '拖拽测试项目' })
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('拖拽任务')).toBeVisible()

  const taskCard = page.locator('.task-card').filter({ hasText: '拖拽任务' })
  const targetColumn = page.locator('.column').nth(2)
  await taskCard.dragTo(targetColumn)
  await expect(targetColumn.locator('.task-card').filter({ hasText: '拖拽任务' })).toBeVisible()
})
