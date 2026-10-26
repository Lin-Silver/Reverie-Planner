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
  await page.getByRole('button',{name:'Save view settings',exact:true}).click()
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
  const row = page.locator('[data-testid="database-record-row"][data-record-id="'+original.id+'"]')
  await expect(row.getByLabel('Name')).toHaveValue('Shared source record')
  const input = row.getByLabel('Name')
  await input.fill('Shared edited from linked view')
  await input.blur()
  await expect.poll(async()=> (await rows(page,host.databaseId)).find(x=>x.id===original.id)?.title,{timeout:15000}).toBe('Shared edited from linked view')
  await reopen(page,host)
  await expect(page.locator('[data-testid="database-record-row"][data-record-id="'+original.id+'"] input[aria-label="Name"]')).toHaveValue('Shared edited from linked view')
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
    const stale=pageB.locator('[data-testid="database-record-row"][data-record-id="'+item.id+'"]')
    const input=stale.getByLabel('Name')
    await input.fill('Local version B')
    await input.blur()
    await expect(pageB.getByRole('alert')).toContainText('Save failed')
    await expect(pageB.getByText('Retry my changes')).toBeVisible()
    expect((await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title).toBe('Remote version A')
    await pageB.getByText('← Back').click()
    await expect(pageB.getByRole('alert')).toContainText('Unsaved database edit')
    await pageB.locator('.nav-item').filter({hasText:'Home'}).first().click()
    await expect(pageB.getByLabel('Active database view')).toBeVisible()
    expect((await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title).toBe('Remote version A')
    await pageB.getByText('Retry my changes').click()
    await expect.poll(async()=> (await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title,{timeout:15000}).toBe('Local version B')
    const afterRetry=(await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!
    expect(afterRetry.version).toBeGreaterThan(item.version+1)
    const second=await pageA.request.put(API+'/db/records',{data:{id:item.id,title:'Later remote version',version:afterRetry.version,values:{}}})
    expect(second.status()).toBe(200)
    const next=pageB.locator('[data-testid="database-record-row"][data-record-id="'+item.id+'"]')
    await next.getByLabel('Name').fill('Unwanted local revision')
    await next.getByLabel('Name').blur()
    await expect(pageB.getByText('Retry my changes')).toBeVisible()
    await pageB.getByRole('button', {name:'Reload latest', exact:true}).click()
    await expect(pageB.locator('[data-testid="database-record-row"][data-record-id="'+item.id+'"] input[aria-label="Name"]')).toHaveValue('Later remote version')
    expect((await rows(pageB,host.databaseId)).find(r=>r.id===item.id)!.title).toBe('Later remote version')
  } finally {await a.close(); await b.close()}
})


test('database typed AND/OR filters persist and apply consistently after reopening', async ({page}) => {
  const host = await makeDatabase(page)
  const score = await property(page, host.databaseId, 'Score', 'number')
  const alpha = await record(page, host.databaseId, 'Alpha')
  const beta = await record(page, host.databaseId, 'Beta')
  for (const [item, value] of [[alpha, 15], [beta, 7]] as const) {
    const result = await page.request.put(API + '/db/records', {data: {id:item.id, title:item.title, version:item.version, values:{[score.id]:value}}})
    expect(result.status()).toBe(200)
  }
  await reopen(page, host)
  const details = page.getByText('Advanced filters (AND / OR)')
  await details.click()
  await page.getByRole('button', {name:'Add filter condition'}).click()
  await page.getByLabel('Filter property').selectOption(score.id)
  await page.getByLabel('Filter operator').selectOption('gt')
  await page.getByLabel('Filter value').fill('10')
  await expect(page.getByTestId('database-record-row')).toHaveCount(1)
  await expect(page.locator('[data-record-id="'+alpha.id+'"]')).toBeVisible()
  await page.getByRole('button', {name:'Add filter condition'}).click()
  const schema = await (await page.request.get(API + '/databases?id=' + host.databaseId)).json() as {properties:{id:string;type:string}[]}
  const titleProperty = schema.properties.find(p=>p.type==='title')!
  await page.getByLabel('Filter property').nth(1).selectOption(titleProperty.id)
  await page.getByLabel('Filter operator').nth(1).selectOption('contains')
  await page.getByLabel('Filter value').nth(1).fill('beta')
  await expect(page.getByTestId('database-record-row')).toHaveCount(0)
  await page.getByLabel('Filter join').selectOption('or')
  await expect(page.getByTestId('database-record-row')).toHaveCount(2)
  await page.getByRole('button',{name:'Save view settings',exact:true}).click()
  await reopen(page, host)
  await expect(page.getByTestId('database-record-row')).toHaveCount(2)
  const response = await page.request.get(API + '/databases?id=' + host.databaseId)
  const database = await response.json() as {views: {config:string}[]}
  const filterTree = JSON.parse(database.views[0].config).filterTree
  expect(filterTree.join).toBe('or')
  expect(filterTree.children).toHaveLength(2)
})


test('database property dialog creates relation and formula with persisted computed results', async ({page}) => {
  const host=await makeDatabase(page)
  const targetRes=await page.request.post(API+'/databases',{data:{title:'Target DB for relations'}})
  expect(targetRes.status()).toBe(201)
  const target=await targetRes.json() as {id:string}
  const targetRecord=await record(page,target.id,'Target item')
  const a=await property(page,host.databaseId,'A','number')
  const b=await property(page,host.databaseId,'B','number')
  const source=await record(page,host.databaseId,'Computed record')
  await reopen(page,host)

  await page.getByText('+ Property').click()
  await expect(page.getByRole('dialog',{name:'New database property'})).toBeVisible()
  await page.getByLabel('Property name').fill('Links')
  await page.getByLabel('Property type').selectOption('relation')
  await page.getByLabel('Relation target database').selectOption(target.id)
  await page.getByRole('button',{name:'Create property'}).click()
  await expect(page.getByRole('dialog',{name:'New database property'})).toBeHidden()
  const sourceDb=await (await page.request.get(API+'/databases?id='+host.databaseId)).json() as {properties:{id:string;type:string;name:string}[]}
  const links=sourceDb.properties.find(x=>x.name==='Links')!
  expect(links.type).toBe('relation')
  const sourceRow=page.locator('[data-testid="database-record-row"][data-record-id="'+source.id+'"]')
  await sourceRow.getByRole('button',{name:'Edit Links'}).click()
  await sourceRow.getByRole('group',{name:'Relation Links'}).getByText('Target item').click()
  await expect.poll(async()=>(await rows(page,host.databaseId)).find(r=>r.id===source.id)?.values[links.id],{timeout:15000}).toEqual([targetRecord.id])

  await page.getByText('+ Property').click()
  await page.getByLabel('Property name').fill('Sum')
  await page.getByLabel('Property type').selectOption('formula')
  await page.getByLabel('Formula operation').selectOption('sum')
  await page.getByLabel('Formula first property').selectOption(a.id)
  await page.getByLabel('Formula second property').selectOption(b.id)
  await page.getByRole('button',{name:'Create property'}).click()
  const formulaSchema=await (await page.request.get(API+'/databases?id='+host.databaseId)).json() as {properties:{id:string;type:string;name:string}[]}
  const sumProp=formulaSchema.properties.find(p=>p.name==='Sum')!
  expect(sumProp.type).toBe('formula')
  const before=(await rows(page,host.databaseId)).find(r=>r.id===source.id)!
  const updated=await page.request.put(API+'/db/records',{data:{id:source.id,title:before.title,version:before.version,values:{[a.id]:7,[b.id]:9}}})
  expect(updated.status()).toBe(200)
  await reopen(page,host)
  const computed=(await rows(page,host.databaseId)).find(r=>r.id===source.id)!
  expect(computed.values[sumProp.id]).toBe(16)
  await expect(page.locator('[data-record-id="'+source.id+'"]').getByLabel('Sum')).toContainText('16')
})


test('database view rename duplicate and deletion preserve data and forbid removing final view', async ({page}) => {
  const host=await makeDatabase(page)
  const item=await record(page,host.databaseId,'Must remain')
  await reopen(page,host)
  page.on('dialog',async d=>{
    if(d.type()==='prompt')await d.accept('Custom table')
    else await d.accept()
  })
  await page.getByRole('button',{name:'Rename database view'}).click()
  await expect(page.getByLabel('Active database view').locator('option:checked')).toContainText('Custom table')
  await page.getByRole('button',{name:'Duplicate database view'}).click()
  await expect(page.getByLabel('Active database view').locator('option:checked')).toContainText('copy')
  await page.getByRole('button',{name:'Delete database view'}).click()
  await expect(page.getByLabel('Active database view').locator('option:checked')).toContainText('Custom table')
  await expect(page.getByRole('button',{name:'Delete database view'})).toBeDisabled()
  await reopen(page,host)
  const res=await page.request.get(API+'/databases?id='+host.databaseId)
  const db=await res.json() as {views:{name:string}[]}
  expect(db.views).toHaveLength(1)
  expect(db.views[0].name).toBe('Custom table')
  expect((await rows(page,host.databaseId)).some(r=>r.id===item.id)).toBe(true)
})


test('database trash restores deleted record and preserves its attributes',async({page})=>{
  const host=await makeDatabase(page)
  const prop=await property(page,host.databaseId,'Note','text')
  const item=await record(page,host.databaseId,'Restore this')
  const filled=await page.request.put(API+'/db/records',{data:{id:item.id,title:item.title,version:item.version,values:{[prop.id]:'Keep me'}}})
  expect(filled.status()).toBe(200)
  await reopen(page,host)
  page.on('dialog',async d=>{await d.accept()})
  const row=page.locator('[data-testid="database-record-row"][data-record-id="'+item.id+'"]')
  await expect(row).toBeVisible()
  await row.getByRole('button',{name:'Delete record Restore this'}).click()
  await expect(row).toHaveCount(0)
  const trash=await page.request.get(API+'/db/trash?kind=record&databaseId='+host.databaseId)
  expect(trash.ok()).toBe(true)
  expect((await trash.json() as {id:string}[]).some(x=>x.id===item.id)).toBe(true)
  await page.getByRole('button',{name:'Open database trash'}).click()
  await page.getByRole('dialog',{name:'Database trash'}).getByRole('button',{name:'Restore Restore this'}).click()
  await expect.poll(async()=>(await rows(page,host.databaseId)).find(r=>r.id===item.id)?.values[prop.id],{timeout:15000}).toBe('Keep me')
  await reopen(page,host)
  await expect(page.locator('[data-testid="database-record-row"][data-record-id="'+item.id+'"]')).toBeVisible()
})


test('database table column layout persists hidden fields order width and record selection', async ({page})=>{
  const host=await makeDatabase(page)
  const score=await property(page,host.databaseId,'Score','number')
  const note=await property(page,host.databaseId,'Note','text')
  const one=await record(page,host.databaseId,'First selection')
  const two=await record(page,host.databaseId,'Second selection')
  await reopen(page,host)
  await page.getByText('Table columns and layout').click()
  await page.getByRole('group',{name:'Column visibility'}).getByLabel('Note').uncheck()
  await expect(page.getByRole('columnheader').filter({hasText:'Note'})).toHaveCount(0)
  await page.getByRole('button',{name:'Move Score left'}).click()
  await page.getByLabel('Width Score').fill('250')
  await page.getByLabel('Select record First selection').check()
  await page.getByLabel('Select record Second selection').check()
  await expect(page.getByText('2 selected')).toBeVisible()
  await page.getByRole('button',{name:'Clear selected rows'}).click()
  await expect(page.getByText('2 selected')).toHaveCount(0)
  await page.getByRole('button',{name:'Save view settings',exact:true}).click()
  await reopen(page,host)
  await expect(page.getByRole('columnheader').filter({hasText:'Note'})).toHaveCount(0)
  await expect(page.getByLabel('Width Score')).toHaveValue('250')
  const schema=await (await page.request.get(API+'/databases?id='+host.databaseId)).json() as {views:{config:string}[]}
  const cfg=JSON.parse(schema.views[0].config)
  expect(cfg.hiddenColumns).toContain(note.id)
  expect(cfg.columnWidths[score.id]).toBe(250)
  expect(cfg.columnOrder[0]).toBe(score.id)
  expect((await rows(page,host.databaseId)).map(r=>r.id)).toEqual([one.id,two.id])
})


test('database calendar drag and unscheduled tray update actual dates',async({page})=>{
  const host=await makeDatabase(page)
  const due=await property(page,host.databaseId,'Due','date')
  const firstRec=await record(page,host.databaseId,'Drag calendar')
  const undated=await record(page,host.databaseId,'Unscheduled calendar')
  const current=await page.evaluate(()=>{const d=new Date();return d.getFullYear()+'-'+String(d.getMonth()+1).padStart(2,'0')})
  const date1=current+'-15',date2=current+'-16',date3=current+'-17'
  const setDate=await page.request.put(API+'/db/records',{data:{id:firstRec.id,title:firstRec.title,version:firstRec.version,values:{[due.id]:date1}}})
  expect(setDate.status()).toBe(200)
  const calendar=await makeView(page,host.databaseId,'calendar')
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(calendar)
  const origin=page.locator('[data-testid="calendar-cell"][data-date="'+date1+'"] [data-testid="calendar-drag-handle"]')
  const dest=page.locator('[data-testid="calendar-cell"][data-date="'+date2+'"]')
  await expect(origin).toBeVisible()
  await origin.dragTo(dest)
  await expect.poll(async()=>(await rows(page,host.databaseId)).find(r=>r.id===firstRec.id)?.values[due.id],{timeout:15000}).toBe(date2)
  await page.getByLabel('Schedule Unscheduled calendar').fill(date3)
  await expect.poll(async()=>(await rows(page,host.databaseId)).find(r=>r.id===undated.id)?.values[due.id],{timeout:15000}).toBe(date3)
})


test('database multi-key numeric and text sorting persists per view and keeps nulls last', async ({page}) => {
  const host = await makeDatabase(page)
  const score = await property(page, host.databaseId, 'Score', 'number')
  // Deliberately insert in a different order from both sort keys.
  const beta = await record(page, host.databaseId, 'Beta')
  const unset = await record(page, host.databaseId, 'No score')
  const alpha = await record(page, host.databaseId, 'Alpha')
  const lower = await record(page, host.databaseId, 'Low score')
  for (const [item,value] of [[beta,8],[alpha,8],[lower,3]] as const) {
    const saved = await page.request.put(API+'/db/records', {data:{
      id:item.id, title:item.title, version:item.version, values:{[score.id]:value}
    }})
    expect(saved.status()).toBe(200)
  }
  await reopen(page,host)
  const rowIds = () => page.getByTestId('database-record-row').evaluateAll(rows => rows.map(row => row.getAttribute('data-record-id')))
  await page.getByLabel('Database sort',{exact:true}).selectOption(score.id)
  await page.getByLabel('Sort direction').selectOption('desc')
  await page.getByLabel('Secondary database sort').selectOption('title')
  await page.getByLabel('Secondary sort direction').selectOption('asc')
  await expect.poll(rowIds).toEqual([alpha.id,beta.id,lower.id,unset.id])
  await page.getByRole('button',{name:'Save view settings',exact:true}).click()
  await reopen(page,host)
  await expect(page.getByLabel('Database sort',{exact:true})).toHaveValue(score.id)
  await expect(page.getByLabel('Sort direction')).toHaveValue('desc')
  await expect(page.getByLabel('Secondary database sort')).toHaveValue('title')
  await expect.poll(rowIds).toEqual([alpha.id,beta.id,lower.id,unset.id])
  const schema = await (await page.request.get(API+'/databases?id='+host.databaseId)).json() as {views:{config:string}[]}
  expect(JSON.parse(schema.views[0].config).sortRules).toEqual([
    {propertyId:score.id,direction:'desc'}, {propertyId:'title',direction:'asc'}
  ])
  await page.getByLabel('Sort direction').selectOption('asc')
  await expect.poll(rowIds).toEqual([lower.id,alpha.id,beta.id,unset.id])
  // A new view has independent persisted configuration.
  const second=await makeView(page,host.databaseId,'table')
  await reopen(page,host)
  await page.getByLabel('Active database view').selectOption(second)
  await expect(page.getByLabel('Database sort',{exact:true})).toHaveValue('')
  const apiOrder=(await rows(page,host.databaseId)).map(r=>r.id)
  await expect.poll(rowIds).toEqual(apiOrder)
})
