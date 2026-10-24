import { expect, test } from '@playwright/test'

test('projects and tasks persist through CRUD operations and reloads', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '工作概览' })).toBeVisible()

  await page.getByRole('button', { name: /^项目/ }).click()
  await page.getByRole('button', { name: '新建项目', exact: true }).click()
  await page.getByLabel('项目名称').fill('浏览器验收项目')
  await page.getByLabel('描述').fill('真实 SQLite 端到端测试')
  await page.getByRole('button', { name: '创建项目', exact: true }).click()
  await expect(page.getByRole('article').filter({ hasText: '浏览器验收项目' })).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: /^项目/ }).click()
  await expect(page.getByRole('article').filter({ hasText: '浏览器验收项目' })).toBeVisible()

  await page.getByRole('button', { name: '任务看板', exact: true }).click()
  await page.getByRole('button', { name: '新建任务', exact: true }).click()
  await page.getByLabel('任务名称').fill('持久化任务')
  await page.getByLabel('所属项目').selectOption({ label: '浏览器验收项目' })
  await page.getByRole('button', { name: '创建任务', exact: true }).click()
  await expect(page.getByRole('heading', { name: '持久化任务' })).toBeVisible()

  await page.getByLabel('更新 持久化任务 状态').selectOption('done')
  await page.reload()
  await page.getByRole('button', { name: '任务看板', exact: true }).click()
  await expect(page.getByRole('heading', { name: '持久化任务' })).toBeVisible()
  await expect(page.getByLabel('更新 持久化任务 状态')).toHaveValue('done')

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('heading', { name: '持久化任务' })).toHaveCount(0)
})
