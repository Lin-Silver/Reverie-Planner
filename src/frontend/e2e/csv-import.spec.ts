import {expect,test} from '@playwright/test'
import fs from 'node:fs/promises'

test.use({trace:'retain-on-failure',screenshot:'only-on-failure'})
async function settings(page:import('@playwright/test').Page){
 await page.goto('/')
 await page.locator('.nav-item').filter({hasText:'Settings'}).click()
 return page.getByRole('region',{name:'CSV database import',exact:true})
}
test('CSV preview mapping commits typed records once and keeps new database views independent',async({page})=>{
 const panel=await settings(page)
 const title='CSV typed import '+Date.now()
 const source='Name,Notes,Amount,Done,Date,Website,Email\r\n"First, row","line one\nline two",12.5,true,2026-10-30,https://example.com,a@example.com\r\n第二行,=SUM(A1:A2),0,false,2026-10-31,,\r\nThird,  literal  ,,,,,\r\n'
 const before=(await(await page.request.get('/api/databases')).json()).length
 await panel.getByLabel('CSV file',{exact:true}).setInputFiles({name:'synthetic.csv',mimeType:'text/csv',buffer:Buffer.from(source)})
 await panel.getByLabel('New CSV database title',{exact:true}).fill(title)
 await panel.getByRole('button',{name:'Preview CSV',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Preview ready')
 await expect(panel.getByLabel('Type for Amount',{exact:true})).toHaveValue('text')
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before)
 for(const [name,kind] of [['Amount','number'],['Done','checkbox'],['Date','date'],['Website','url'],['Email','email']]){
  await panel.getByLabel('Type for '+name,{exact:true}).selectOption(kind)
 }
 await expect(panel.getByRole('button',{name:'Import CSV copy',exact:true})).toHaveCount(0)
 await panel.getByRole('button',{name:'Preview CSV',exact:true}).click()
 await expect(panel.getByLabel('CSV preview',{exact:true})).toContainText('3 records')
 let result:{databaseId:string;pageId:string}|undefined
 await page.route('**/api/import/csv',async route=>{
  const response=await route.fetch();expect(response.status()).toBe(201);result=await response.json();await route.abort('failed')
 },{times:1})
 await panel.getByRole('button',{name:'Import CSV copy',exact:true}).click()
 await expect(panel.getByRole('alert')).toBeVisible()
 await panel.getByRole('button',{name:'Import CSV copy',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Imported 3 records')
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before+1)
 const database=await(await page.request.get('/api/databases?id='+result!.databaseId)).json()
 expect(database.views).toHaveLength(1);expect(database.views[0].config).toBe('{}')
 const records=await(await page.request.get('/api/db/records?databaseId='+database.id)).json()
 const first=records.find((r:{title:string})=>r.title==='First, row')
 const field=(name:string)=>database.properties.find((p:{name:string})=>p.name===name).id
 expect(first.values[field('Notes')]).toBe('line one\nline two')
 expect(first.values[field('Amount')]).toBe(12.5);expect(first.values[field('Done')]).toBe(true)
 const second=records.find((r:{title:string})=>r.title==='第二行')
 expect(second.values[field('Notes')]).toBe('=SUM(A1:A2)');expect(second.values[field('Amount')]).toBe(0);expect(second.values[field('Done')]).toBe(false)
 const third=records.find((r:{title:string})=>r.title==='Third')
 expect(third.values[field('Amount')]).toBeUndefined();expect(third.values[field('Notes')]).toBe('  literal  ')
 await page.goto('/?page='+result!.pageId)
 const firstRow=page.locator('[data-record-id="'+first.id+'"]')
 await expect(firstRow.getByRole('textbox',{name:'Name',exact:true})).toHaveValue('First, row')
 const notes=firstRow.getByRole('textbox',{name:'Notes',exact:true})
 await expect(notes).toHaveValue('line one\nline two')
 await notes.focus();await firstRow.getByRole('textbox',{name:'Name',exact:true}).focus()
 expect((await(await page.request.get('/api/db/records?databaseId='+database.id)).json()).find((record:{id:string})=>record.id===first.id).values[field('Notes')]).toBe('line one\nline two')
 await page.reload()
 await expect(page.locator('[data-record-id="'+second.id+'"]').getByRole('textbox',{name:'Name',exact:true})).toHaveValue('第二行')
})

test('CSV validation blocks partial import and permits keyboard correction in narrow reduced-motion layout',async({page})=>{
 await page.setViewportSize({width:390,height:844})
 await page.emulateMedia({reducedMotion:'reduce',colorScheme:'light'})
 await page.goto('/')
 // The native mobile drawer is deliberately modal; navigate inside it first.
 const drawer=page.getByRole('dialog',{name:'Workspace navigation',exact:true})
 if(!await drawer.isVisible())await page.getByRole('button',{name:'Toggle sidebar',exact:true}).click()
 await page.locator('.nav-item').filter({hasText:'Settings'}).click()
 const panel=page.getByRole('region',{name:'CSV database import',exact:true})
 const before=(await(await page.request.get('/api/databases')).json()).length
 const source='Name,Amount\nSynthetic long preview title for a narrow window,not-a-number\nAnother,10\n'
 await panel.getByLabel('CSV file',{exact:true}).setInputFiles({name:'errors.csv',mimeType:'text/csv',buffer:Buffer.from(source)})
 await panel.getByRole('button',{name:'Preview CSV',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Preview ready')
 await panel.getByLabel('Type for Amount',{exact:true}).selectOption('number')
 await panel.getByRole('button',{name:'Preview CSV',exact:true}).focus();await page.keyboard.press('Enter')
 await expect(panel.getByRole('alert')).toContainText('Row 2, Amount')
 await expect(panel.getByRole('button',{name:'Import CSV copy',exact:true})).toHaveCount(0)
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await fs.mkdir('test-results/import-evidence',{recursive:true})
 await panel.getByLabel('CSV preview',{exact:true}).scrollIntoViewIfNeeded()
 await page.screenshot({path:'test-results/import-evidence/csv-preview-narrow.png'})
 await panel.getByLabel('Type for Amount',{exact:true}).selectOption('text')
 await panel.getByRole('button',{name:'Preview CSV',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Preview ready')
 await panel.getByRole('button',{name:'Import CSV copy',exact:true}).focus();await page.keyboard.press('Enter')
 await expect(panel.getByRole('status')).toContainText('Imported 2 records')
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before+1)
})

test('CSV rejects malformed headers and non-UTF8 source without creating a database',async({page})=>{
 const panel=await settings(page)
 const before=(await(await page.request.get('/api/databases')).json()).length
 await panel.getByLabel('CSV file',{exact:true}).setInputFiles({name:'invalid.csv',mimeType:'text/csv',buffer:Buffer.from([0xff,0xfe,0x61])})
 await expect(panel.getByRole('alert')).toContainText('UTF-8')
 await expect(panel.getByRole('button',{name:'Preview CSV',exact:true})).toBeDisabled()
 await panel.getByLabel('CSV file',{exact:true}).setInputFiles({name:'duplicate.csv',mimeType:'text/csv',buffer:Buffer.from('Name,name\nRow,1')})
 await panel.getByRole('button',{name:'Preview CSV',exact:true}).click()
 await expect(panel.getByRole('alert')).toContainText('headers must be unique')
 expect((await(await page.request.get('/api/databases')).json()).length).toBe(before)
})
