import {expect,test} from '@playwright/test'
import fs from 'node:fs/promises'

test.use({trace:'retain-on-failure',screenshot:'only-on-failure'})
async function settings(page:import('@playwright/test').Page){
 await page.goto('/')
 await page.locator('.nav-item').filter({hasText:'Settings'}).click()
 return page.getByRole('region',{name:'Document package transfer'})
}
test('Document package downloads, previews and imports a linked tree without replacing saved writing',async({page})=>{
 const blocks=[{type:'paragraph',content:[{type:'text',text:'Preserve bold writing',styles:{bold:true}}]}]
 const root=await(await page.request.post('/api/pages',{data:{title:'Transfer root '+Date.now(),workspace:true,content:JSON.stringify(blocks)}})).json()
 const child=await(await page.request.post('/api/pages',{data:{title:'Transfer child',parentId:root.id,workspace:true,content:JSON.stringify(blocks),icon:'📓'}})).json()
 const linked=[...blocks,{type:'paragraph',content:[{type:'link',href:'/?page='+child.id,content:[{type:'text',text:'Open copied child',styles:{}}]}]}]
 await page.request.put('/api/pages',{data:{...root,content:JSON.stringify(linked)}})
 const panel=await settings(page)
 await panel.getByLabel('Document tree to export',{exact:true}).selectOption(root.id)
 const downloadEvent=page.waitForEvent('download')
 await panel.getByRole('button',{name:'Download document package',exact:true}).click()
 const download=await downloadEvent
 const raw=await fs.readFile((await download.path())!)
 const parsed=JSON.parse(raw.toString())
 expect(parsed.pages).toHaveLength(2);expect(parsed.assets).toHaveLength(0)
 expect(parsed.pages.find((p:{id:string})=>p.id===child.id).icon).toBe('📓')
 const before=(await(await page.request.get('/api/pages')).json()).length
 await panel.getByLabel('Document package JSON').setInputFiles({name:download.suggestedFilename(),mimeType:'application/json',buffer:raw})
 await panel.getByRole('button',{name:'Preview document package',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Preview ready')
 await expect(panel.getByLabel('Document package preview')).toContainText('2 documents')
 expect((await(await page.request.get('/api/pages')).json()).length).toBe(before)
 let imported:{pages:Record<string,string>}|undefined
 await page.route('**/api/document-package?digest=*',async route=>{const response=await route.fetch();expect(response.status()).toBe(201);imported=await response.json();await route.abort('failed')},{times:1})
 await panel.getByRole('button',{name:'Import document copy',exact:true}).click()
 await expect(panel.getByRole('alert')).toBeVisible()
 await panel.getByRole('button',{name:'Import document copy',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Imported 2 documents')
 expect((await(await page.request.get('/api/pages')).json()).length).toBe(before+3)
 const copiedRoot=imported!.pages[root.id];const copiedChild=imported!.pages[child.id]
 expect(copiedRoot).not.toBe(root.id)
 await page.goto('/?page='+copiedRoot)
 await expect(page.locator('.bn-editor strong')).toContainText('Preserve bold writing')
 const link=page.locator('.bn-editor').getByRole('link',{name:'Open copied child',exact:true})
 await expect(link).toHaveAttribute('href','/?page='+copiedChild)
 await link.click()
 await expect(page.getByLabel('Page title')).toHaveValue('Transfer child')
 await page.reload();await expect(page.getByLabel('Page title')).toHaveValue('Transfer child')
 expect((await(await page.request.get('/api/pages?id='+root.id)).json()).content).toBe(JSON.stringify(linked))
})

test('Document package blockers allow retry and narrow keyboard preview stays readable',async({page})=>{
 const panel=await settings(page)
 const before=(await(await page.request.get('/api/pages')).json()).length
 const pack={format:'reverie-documents',version:1,rootId:'synthetic-root',pages:[{id:'synthetic-root',title:'Document preview',content:JSON.stringify([{type:'paragraph',content:[{type:'text',text:'Offline writing',styles:{}}]}])}],assets:[]}
 await panel.getByLabel('Document package JSON').setInputFiles({name:'invalid.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify({...pack,version:999}))})
 await panel.getByRole('button',{name:'Preview document package',exact:true}).click()
 await expect(panel.getByRole('alert')).toContainText('unsupported document package')
 await expect(panel.getByRole('button',{name:'Import document copy',exact:true})).toHaveCount(0)
 expect((await(await page.request.get('/api/pages')).json()).length).toBe(before)
 await page.setViewportSize({width:390,height:844});await page.emulateMedia({reducedMotion:'reduce',colorScheme:'light'})
 await page.getByRole('dialog',{name:'Workspace navigation',exact:true}).getByRole('button',{name:'Close navigation',exact:true}).click()
 await panel.getByLabel('Document package JSON').setInputFiles({name:'valid.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(pack))})
 await panel.getByRole('button',{name:'Preview document package',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Preview ready')
 await panel.getByText('View preserved editor blocks',{exact:true}).click()
 await expect(panel.locator('.markdown-source')).toContainText('Offline writing')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await fs.mkdir('test-results/import-evidence',{recursive:true})
 await panel.screenshot({path:'test-results/import-evidence/document-package-narrow.png'})
 await panel.getByRole('button',{name:'Import document copy',exact:true}).focus();await page.keyboard.press('Enter')
 await expect(panel.getByRole('status')).toContainText('Imported 1 documents')
})
