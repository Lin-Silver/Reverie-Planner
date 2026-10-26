import { expect, test, type Page } from '@playwright/test'

const API = 'http://127.0.0.1:18765/api'
type RecordRow = { id: string; pageId: string; title: string; version: number; values: Record<string, unknown> }
type Host = { databaseId: string; title: string }

async function makeDatabase(page: Page): Promise<Host> {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.getByText('New page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({ timeout: 20000 })
  const title = 'Database feature ' + Date.now() + '-' + Math.random().toString(36).slice(2,6)
  await page.locator('.page-title').fill(title)
  await page.locator('.bn-editor').click()
  await expect.poll(async () => {
    const res = await page.request.get(API + '/pages')
    const all: {id: string;title: string}[] = await res.json()
    return all.some(p => p.title === title)
  }, {timeout: 15000}).toBe(true)
  await page.getByText('Create database').click()
  await expect(page.getByText('+ Property')).toBeVisible({ timeout: 15000 })
  const res = await page.request.get(API + '/databases')
  expect(res.ok()).toBe(true)
  const all: {id: string;pageId:string}[] = await res.json()
  expect(all.length).toBeGreaterThan(0)
  return {databaseId: all[all.length-1].id, title}
}
async function reopen(page: Page, host: Host) {
  await page.goto('/')
  await expect(page.getByText('Welcome back')).toBeVisible()
  await page.locator('.tree-item').filter({hasText: host.title}).first().click()
  await expect(page.getByLabel('Active database view')).toBeVisible({timeout: 15000})
}
async function property(page: Page, db: string, name: string, type: string, config = '{}') {
  const res = await page.request.post(API + '/db/properties', {data: {databaseId:db,name,type,config}})
  expect(res.status()).toBe(201)
  return (await res.json()) as {id:string}
}
async function record(page: Page, db: string, title: string): Promise<RecordRow> {
  const res = await page.request.post(API + '/db/records', {data: {databaseId:db,title}})
  expect(res.status()).toBe(201)
  return await res.json() as RecordRow
}
async function rows(page: Page, db: string): Promise<RecordRow[]> {
  const res = await page.request.get(API + '/db/records?databaseId=' + encodeURIComponent(db))
  expect(res.ok()).toBe(true)
  return await res.json() as RecordRow[]
}
async function makeView(page:Page, db:string, type:string):Promise<string> {
  const res = await page.request.post(API + '/db/views',{data:{databaseId:db,name:type+' test',type,config:'{}'}})
  expect(res.status()).toBe(201)
  return (await res.json() as {id:string}).id
}
test('database board dragging updates the same SQLite record and survives reopening',async({page})=>{
  const host=await makeDatabase(page)
  const status=await property(page,host.databaseId,'Status','select',JSON.stringify({options:['Todo','Done']}))
  const item=await record(page,host.databaseId,'Drag me')
  const put=await page.request.put(API+'/db/records',{data:{id:item.id,version:item.version,title:'Drag me',values:{[status.id]:'Todo'}}})
  expect(put.status()).toBe(200)
  const board=await makeView(page,host.databaseId,'board')
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(board)
  const source=page.locator('[data-testid="board-group"][data-group="Todo"] [data-testid="board-card"]').filter({hasText:'Drag me'})
  const target=page.locator('[data-testid="board-group"][data-group="Done"]')
  await expect(source).toBeVisible()
  await source.dragTo(target)
  await expect.poll(async()=> (await rows(page,host.databaseId)).find(r=>r.id===item.id)?.values[status.id],{timeout:15000}).toBe('Done')
  const after=(await rows(page,host.databaseId)).find(r=>r.id===item.id)!
  expect(after.version).toBeGreaterThan(item.version)
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(board)
  await expect(page.locator('[data-testid="board-group"][data-group="Done"]')).toContainText('Drag me')
})

test('database calendar date edit and view configuration persist after reload',async({page})=>{
  const host=await makeDatabase(page)
  const due=await property(page,host.databaseId,'Due','date')
  const item=await record(page,host.databaseId,'Calendar item')
  const now=await page.evaluate(()=>{const d=new Date();return {year:d.getFullYear(),month:d.getMonth()+1}})
  const ym=String(now.year)+'-'+String(now.month).padStart(2,'0')
  const first=ym+'-15',second=ym+'-16'
  const put=await page.request.put(API+'/db/records',{data:{id:item.id,version:item.version,title:'Calendar item',values:{[due.id]:first}}})
  expect(put.status()).toBe(200)
  const viewId=await makeView(page,host.databaseId,'calendar')
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(viewId)
  await expect(page.locator('[data-testid="calendar-cell"][data-date="'+first+'"]')).toContainText('Calendar item')
  await page.getByLabel('Calendar date property').selectOption(due.id)
  await page.getByText('Save view settings').click()
  await page.getByLabel('Date for Calendar item').fill(second)
  await expect.poll(async()=> (await rows(page,host.databaseId)).find(r=>r.id===item.id)?.values[due.id],{timeout:15000}).toBe(second)
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(viewId)
  await expect(page.locator('[data-testid="calendar-cell"][data-date="'+second+'"]')).toContainText('Calendar item')
  const saved=await (await page.request.get(API+'/databases?id='+host.databaseId)).json() as {views:{id:string;config:string}[]}
  expect(JSON.parse(saved.views.find(v=>v.id===viewId)!.config).dateProperty).toBe(due.id)
})

test('database list record opens and saves its real BlockNote page',async({page})=>{
  const host=await makeDatabase(page)
  const item=await record(page,host.databaseId,'Open linked record')
  const list=await makeView(page,host.databaseId,'list')
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(list)
  const card=page.getByTestId('list-record').filter({hasText:'Open linked record'})
  await expect(card).toBeVisible()
  await card.getByText('Open page').click()
  await expect(page.locator('.bn-editor')).toBeVisible({timeout:15000})
  await page.locator('.bn-editor').click()
  await page.keyboard.type('Saved through BlockNote record editor')
  await expect.poll(async()=>{
    const res=await page.request.get(API+'/pages?id='+item.pageId)
    const pg=await res.json() as {content:string}
    return pg.content
  },{timeout:15000}).toContain('Saved through BlockNote record editor')
})

test('database creates each view type through the visible interface',async({page})=>{
  const host=await makeDatabase(page)
  for(const type of ['board','calendar','list','gallery'] as const){
    await page.getByLabel('New view type').selectOption(type)
    await page.getByText('+ View').click()
    await expect(page.getByLabel('Active database view').locator('option:checked')).toContainText(type)
  }
  const res=await page.request.get(API+'/databases?id='+host.databaseId)
  const db=await res.json() as {views:{type:string}[]}
  for(const type of ['table','board','calendar','list','gallery'])expect(db.views.some(v=>v.type===type)).toBe(true)
})


test('linked database page reopens and shares its source records', async ({page}) => {
  const host = await makeDatabase(page)
  const original = await record(page, host.databaseId, 'Shared source record')
  await reopen(page, host)
  const dbResponse = await page.request.get(API + '/databases?id=' + host.databaseId)
  const db = await dbResponse.json() as {title:string}
  page.on('dialog', async dialog => { await dialog.accept() })
  await page.getByText('+ Linked view page').click()
  const linkedTitle = db.title + ' (linked)'
  await expect.poll(async () => {
    const response = await page.request.get(API + '/pages')
    const all: {id:string;title:string}[] = await response.json()
    return all.find(x => x.title === linkedTitle)?.id || ''
  }, {timeout:15000}).not.toBe('')
  const response = await page.request.get(API + '/pages')
  const all: {id:string;title:string}[] = await response.json()
  const linked = all.find(x => x.title === linkedTitle)
  expect(linked?.id).toBeTruthy()
  const detail = await (await page.request.get(API + '/pages?id=' + linked!.id)).json() as {linkedDatabaseId:string;linkedViewId:string}
  expect(detail.linkedDatabaseId).toBe(host.databaseId)
  expect(detail.linkedViewId).toBeTruthy()
  await page.reload()
  await page.locator('.tree-item').filter({hasText: linkedTitle}).first().click()
  await expect(page.getByLabel('Active database view')).toBeVisible({timeout:15000})
  const row = page.getByTestId('database-record-row').filter({hasText:'Shared source record'})
  await expect(row).toBeVisible()
  const input = row.locator('input').first()
  await input.fill('Shared edited from linked view')
  await input.blur()
  await expect.poll(async()=> (await rows(page,host.databaseId)).find(x=>x.id===original.id)?.title,{timeout:15000}).toBe('Shared edited from linked view')
  await reopen(page,host)
  await expect(page.getByTestId('database-record-row')).toContainText('Shared edited from linked view')
})


test('database concurrent edits preserve stale draft until explicit Retry or Reload latest', async ({browser}) => {
  const a=await browser.newContext()
  const b=await browser.newContext()
  try {
    const pageA=await a.newPage(), pageB=await b.newPage()
    const host=await makeDatabase(pageA)
    const item=await record(pageA,host.databaseId,'Original concurrent record')
    await reopen(pageB,host)
    const first=await pageA.request.put(API+'/db/records',{data:{id:item.id,title:'Remote version A',version:item.version,values:{}}})
    expect(first.status()).toBe(200)
    const stale=pageB.getByTestId('database-record-row').filter({hasText:'Original concurrent record'})
    const input=stale.locator('input').first()
    await input.fill('Local version B')
    await input.blur()
    await expect(pageB.getByRole('alert')).toContainText('Save failed')
    await expect(pageB.getByText('Retry my changes')).toBeVisible()
    expect((await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title).toBe('Remote version A')
    await pageB.getByText('← Back').click()
    await expect(pageB.getByRole('alert')).toContainText('Unsaved database edit')
    await pageB.getByText('Retry my changes').click()
    await expect.poll(async()=> (await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title,{timeout:15000}).toBe('Local version B')
    const afterRetry=(await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!
    expect(afterRetry.version).toBeGreaterThan(item.version+1)
    const second=await pageA.request.put(API+'/db/records',{data:{id:item.id,title:'Later remote version',version:afterRetry.version,values:{}}})
    expect(second.status()).toBe(200)
    const next=pageB.getByTestId('database-record-row').filter({hasText:'Local version B'})
    await next.locator('input').first().fill('Unwanted local revision')
    await next.locator('input').first().blur()
    await expect(pageB.getByText('Retry my changes')).toBeVisible()
    await pageB.getByText('Reload latest').click()
    await expect(pageB.getByTestId('database-record-row')).toContainText('Later remote version')
    expect((await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title).toBe('Later remote version')
  } finally {await a.close(); await b.close()}
})
