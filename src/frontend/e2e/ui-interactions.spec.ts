import { expect, test } from '@playwright/test'

test('collapsible sidebar and task dialog interactions work', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '工作概览' })).toBeVisible()

  const sidebar = page.locator('aside.sidebar')
  await expect(sidebar).not.toHaveClass(/collapsed/)
  await page.getByRole('button', { name: '折叠侧边栏' }).click()
  await expect(sidebar).toHaveClass(/collapsed/)
  await page.getByRole('button', { name: '展开侧边栏' }).click()
  await expect(sidebar).not.toHaveClass(/collapsed/)

  await page.getByRole('button', { name: '任务看板', exact: true }).click()
  await page.getByRole('button', { name: '新建任务', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
})

test('task drag and drop updates status', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: /^项目/ }).click()
  await page.getByRole('button', { name: '新建项目', exact: true }).click()
  await page.getByLabel('项目名称').fill('拖拽测试项目')
  await page.getByRole('button', { name: '创建项目', exact: true }).click()

  await page.getByRole('button', { name: '任务看板', exact: true }).click()
  await page.getByRole('button', { name: '新建任务', exact: true }).click()
  await page.getByLabel('任务名称').fill('拖拽任务')
  await page.getByLabel('所属项目').selectOption({ label: '拖拽测试项目' })
  await page.getByRole('button', { name: '创建任务', exact: true }).click()
  await expect(page.getByRole('heading', { name: '拖拽任务' })).toBeVisible()

  const taskCard = page.locator('.task-card').filter({ hasText: '拖拽任务' })
  const targetColumn = page.locator('.column').nth(2)
  await taskCard.dragTo(targetColumn)
  await expect(page.getByLabel('更新 拖拽任务 状态')).toHaveValue('done')
})
