import {expect,test} from '@playwright/test'
import fs from 'node:fs/promises'

test.use({trace:'retain-on-failure',screenshot:'only-on-failure'})
async function sourceDatabase(page:import('@playwright/test').Page){
 const body={csv:'Name,Notes,Amount,Done\nFirst,"line one\nline two",12.5,true\n第二行,=literal,0,false',title:'Database package '+Date.now(),types:['title','text','number','checkbox']}
 const preview=await(await page.request.post('/api/import/csv?preview=1',{data:body})).json()
 const response=await page.request.post('/api/import/csv',{data:{...body,digest:preview.digest,operationKey:crypto.randomUUID()}})
 expect(response.status()).toBe(201)
 return response.json() as Promise<{databaseId:string;pageId:string}>
}
async function settings(page:import('@playwright/test').Page){
 await page.goto('/')
 if(await page.getByRole('button',{name:'Toggle sidebar',exact:true}).isVisible()&&!await page.locator('.nav-item').filter({hasText:'Settings'}).isVisible())await page.getByRole('button',{name:'Toggle sidebar',exact:true}).click()
 await page.locator('.nav-item').filter({hasText:'Settings'}).click()
 return page.getByRole('region',{name:'Database package transfer',exact:true})
}
test('database download preview and interrupted-response retry preserve types, writing and independent views',async({page})=>{
 await page.goto('/')
 const source=await sourceDatabase(page)
 const database=await(await page.request.get('/api/databases?id='+source.databaseId)).json()
 const amount=database.properties.find((p:{name:string})=>p.name==='Amount').id
 const config=JSON.stringify({sortRules:[{propertyId:amount,direction:'desc'}],columnWidths:{[amount]:180},hiddenColumns:[amount],filter:'literal '+amount})
 const update=await page.request.put('/api/db/views',{data:{...database.views[0],config}});expect(update.ok()).toBe(true)
 const panel=await settings(page)
 await panel.getByLabel('Database to export',{exact:true}).selectOption(source.databaseId)
 const downloadPromise=page.waitForEvent('download')
 await panel.getByRole('button',{name:'Download database package',exact:true}).click()
 const download=await downloadPromise
 const downloaded=await download.path();expect(downloaded).not.toBeNull()
 const bytes=await fs.readFile(downloaded!)
 const before=(await(await page.request.get('/api/databases')).json()).length
 await panel.getByLabel('Database package JSON',{exact:true}).setInputFiles({name:download.suggestedFilename(),mimeType:'application/json',buffer:bytes})
 await panel.getByRole('button',{name:'Preview database package',exact:true}).click()
 await expect(panel.getByLabel('Database package preview',{exact:true})).toContainText('2 records')
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before)
 let imported:{databaseId:string;pageId:string}|undefined
 await page.route('**/api/database-package?digest=*',async route=>{const response=await route.fetch();expect(response.status()).toBe(201);imported=await response.json();await route.abort('failed')},{times:1})
 await panel.getByRole('button',{name:'Import database copy',exact:true}).click()
 await expect(panel.getByRole('alert')).toBeVisible()
 await panel.getByRole('button',{name:'Import database copy',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Imported 2 records')
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before+1)
 const copy=await(await page.request.get('/api/databases?id='+imported!.databaseId)).json()
 expect(copy.id).not.toBe(source.databaseId)
 const copiedAmount=copy.properties.find((p:{name:string})=>p.name==='Amount').id
 const viewSettings=JSON.parse(copy.views[0].config)
 expect(viewSettings.sortRules[0].propertyId).toBe(copiedAmount);expect(viewSettings.columnWidths[copiedAmount]).toBe(180);expect(viewSettings.hiddenColumns).toEqual([copiedAmount]);expect(viewSettings.filter).toBe('literal '+amount)
 const records=await(await page.request.get('/api/db/records?databaseId='+copy.id)).json()
 const first=records.find((r:{title:string})=>r.title==='First')
 const notes=copy.properties.find((p:{name:string})=>p.name==='Notes').id
 expect(first.values[notes]).toBe('line one\nline two');expect(first.values[copiedAmount]).toBe(12.5)
 const originalView=await(await page.request.get('/api/db/views?databaseId='+source.databaseId)).json()
 expect(originalView[0].id).not.toBe(copy.views[0].id)
 await page.goto('/?page='+imported!.pageId)
 await expect(page.getByRole('textbox',{name:'Database filter',exact:true})).toHaveValue('literal '+amount)
 await page.getByRole('textbox',{name:'Database filter',exact:true}).fill('')
 await page.getByRole('button',{name:'Save view settings',exact:true}).click()
 await expect.poll(async()=>JSON.parse((await(await page.request.get('/api/db/views?databaseId='+copy.id)).json())[0].config).filter).toBe('')
 expect(JSON.parse((await(await page.request.get('/api/db/views?databaseId='+source.databaseId)).json())[0].config).filter).toBe('literal '+amount)
 await expect(page.locator('[data-record-id="'+first.id+'"]').getByRole('textbox',{name:'Notes',exact:true})).toHaveValue('line one\nline two')
 await page.reload()
 await expect(page.locator('[data-record-id="'+first.id+'"]').getByRole('textbox',{name:'Name',exact:true})).toHaveValue('First')
})

test('database package narrow keyboard preview and malformed source never partly import',async({page})=>{
 await page.goto('/');const source=await sourceDatabase(page)
 const response=await page.request.get('/api/database-package?databaseId='+source.databaseId);expect(response.status()).toBe(200);const raw=await response.body()
 await page.setViewportSize({width:390,height:844});await page.emulateMedia({reducedMotion:'reduce',colorScheme:'light'})
 const panel=await settings(page)
 const before=(await(await page.request.get('/api/databases')).json()).length
 const input=panel.getByLabel('Database package JSON',{exact:true})
 await input.setInputFiles({name:'bad.json',mimeType:'application/json',buffer:Buffer.from(raw.toString().replace('"version":1','"version":999'))})
 await panel.getByRole('button',{name:'Preview database package',exact:true}).click()
 await expect(panel.getByRole('alert')).toContainText('unsupported database package')
 await expect(panel.getByRole('button',{name:'Import database copy',exact:true})).toHaveCount(0)
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before)
 await input.setInputFiles({name:'valid.json',mimeType:'application/json',buffer:raw})
 await panel.getByRole('button',{name:'Preview database package',exact:true}).focus();await page.keyboard.press('Enter')
 await expect(panel.getByRole('status')).toContainText('Preview ready')
 await expect(panel.locator('[aria-current="step"]')).toHaveText('2Review preview')
 expect(await panel.getByRole('button',{name:'Import database copy',exact:true}).evaluate(element=>getComputedStyle(element).transitionDuration)).toBe('0s')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await panel.getByLabel('Database package preview',{exact:true}).scrollIntoViewIfNeeded()
 await fs.mkdir('test-results/import-evidence',{recursive:true});await page.screenshot({path:'test-results/import-evidence/database-package-narrow.png'})
 await panel.getByRole('button',{name:'Import database copy',exact:true}).focus();await page.keyboard.press('Enter')
 await expect(panel.getByRole('status')).toContainText('Imported 2 records')
 await expect(panel.getByRole('status')).toBeFocused()
 await expect(panel.locator('[aria-current="step"]')).toHaveText('3Copy created')
 await input.setInputFiles({name:'another-copy.json',mimeType:'application/json',buffer:raw})
 await expect(panel.getByRole('button',{name:'Import database copy',exact:true})).toHaveCount(0)
 await expect(panel.locator('[aria-current="step"]')).toHaveText('1Choose a file')
})
