import { expect, test } from '@playwright/test'

const API = 'http://127.0.0.1:18765'

async function propertyNamed(page: any, name: string) {
  const dbs = (await (await page.request.fetch(`${API}/api/databases`)).json()) as any[]
  for (const db of dbs) {
    const prop = db.properties.find((p: any) => p.name === name)
    if (prop) return { db, prop }
  }
  return null
}

test('property editor: rename, edit select options and toggle visibility', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  await page.getByText('Create database').click()
  await expect(page.getByText('+ Property')).toBeVisible({ timeout: 20000 })

  await page.getByText('+ Property').click()
  const create = page.getByRole('dialog', { name: 'New database property' })
  await create.getByLabel('Property name').fill('Stage')
  await create.getByLabel('Property type').selectOption('select')
  await create.getByLabel('Property options').fill('Todo,Done')
  await create.getByRole('button', { name: 'Create property' }).click()
  await expect(create).toBeHidden()
  await expect(page.locator('table th').filter({ hasText: 'Stage' })).toBeVisible()

  await page.getByRole('button', { name: 'Edit Stage' }).click()
  const edit = page.getByRole('dialog', { name: 'Edit database property' })
  await expect(edit).toBeVisible()
  await expect(edit.getByLabel('Property type')).toBeDisabled()
  await expect(edit.getByLabel('Property type')).toHaveValue('select')
  await edit.getByLabel('Property name').fill('Phase')
  await edit.getByLabel('Property options').fill('Todo, Doing, Done')
  await edit.getByLabel('Show property in database').uncheck()
  await edit.getByRole('button', { name: 'Save property' }).click()
  await expect(edit).toBeHidden()

  // Hidden properties leave the table but stay recoverable from the toolbar.
  await expect(page.locator('table th').filter({ hasText: 'Phase' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Show Phase property in database' })).toBeVisible()
  await page.getByRole('button', { name: 'Show Phase property in database' }).click()
  await expect(page.locator('table th').filter({ hasText: 'Phase' })).toBeVisible()

  const saved = await propertyNamed(page, 'Phase')
  expect(saved).not.toBeNull()
  expect(saved.prop.visible).toBe(true)
  expect(JSON.parse(saved.prop.config).options).toEqual(['Todo', 'Doing', 'Done'])
})

test('property editor: Escape closes the dialog and restores focus', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  await page.getByText('Create database').click()
  await expect(page.getByText('+ Property')).toBeVisible({ timeout: 20000 })

  await page.getByText('+ Property').click()
  const create = page.getByRole('dialog', { name: 'New database property' })
  await create.getByLabel('Property name').fill('Notes')
  await create.getByRole('button', { name: 'Create property' }).click()
  await expect(create).toBeHidden()

  const trigger = page.getByRole('button', { name: 'Edit Notes' })
  await trigger.click()
  await expect(page.getByRole('dialog', { name: 'Edit database property' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: 'Edit database property' })).toBeHidden()
  await expect(trigger).toBeFocused()
})

test('property editor: stale version is rejected instead of overwriting', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  await page.getByText('Create database').click()
  await expect(page.getByText('+ Property')).toBeVisible({ timeout: 20000 })

  await page.getByText('+ Property').click()
  const create = page.getByRole('dialog', { name: 'New database property' })
  await create.getByLabel('Property name').fill('Owner')
  await create.getByRole('button', { name: 'Create property' }).click()
  await expect(create).toBeHidden()

  await page.getByRole('button', { name: 'Edit Owner' }).click()
  const edit = page.getByRole('dialog', { name: 'Edit database property' })
  // Simulate a concurrent remote rename after the dialog captured its version.
  const target = await propertyNamed(page, 'Owner')
  const remote = await page.request.fetch(`${API}/api/db/properties`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' },
    data: { ...target.prop, name: 'Renamed remotely' },
  })
  expect(remote.ok()).toBeTruthy()

  await edit.getByLabel('Property name').fill('Local rename')
  await edit.getByRole('button', { name: 'Save property' }).click()
  await expect(edit.getByRole('alert')).toContainText('version conflict')
  await expect(edit).toBeVisible()
  expect((await propertyNamed(page, 'Renamed remotely'))?.prop.name).toBe('Renamed remotely')
  expect(await propertyNamed(page, 'Local rename')).toBeNull()
})
