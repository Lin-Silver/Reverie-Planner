import { expect, test } from '@playwright/test'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { createServer } from 'node:net'
import path from 'node:path'
import fs from 'node:fs/promises'

async function unusedPort() {
 const server=createServer();server.listen(0,'127.0.0.1');await once(server,'listening')
 const address=server.address();if(!address||typeof address==='string')throw new Error('no test port')
 await new Promise<void>((resolve,reject)=>server.close(err=>err?reject(err):resolve()))
 return address.port
}
test('workspace archive download previews, restores and reopens an independent database with attachments', async ({page})=>{
 await page.goto('/')
 const api=page.request
 const unique='Archive roundtrip '+Date.now()
 const parent=await (await api.post('/api/pages',{data:{title:unique,workspace:true,createdBy:'test'}})).json()
 const child=await (await api.post('/api/pages',{data:{title:unique+' child',parentId:parent.id,workspace:true,createdBy:'test'}})).json()
 const db=await (await api.post('/api/databases',{data:{title:unique+' database'}})).json()
 const record=await (await api.post('/api/db/records',{data:{databaseId:db.id,title:unique+' record'}})).json()
 const uploaded=await api.post('/api/db/files',{multipart:{databaseId:db.id,recordId:record.id,file:{name:'portable.txt',mimeType:'text/plain',buffer:Buffer.from('Portable attachment content')}}})
 expect(uploaded.status()).toBe(201)
 const file=await uploaded.json()
 expect((await api.post('/api/page-library',{data:{id:child.id,favorite:true}})).ok()).toBe(true)
 const source=await (await api.get('/api/workspace')).json()
 const beforeProviders=await (await api.get('/api/ai/providers')).json()
 await page.locator('.nav-item').filter({hasText:'Settings'}).click()
 const panel=page.getByRole('region',{name:'Workspace archive and restore'})
 const [download]=await Promise.all([page.waitForEvent('download'),panel.getByRole('button',{name:'Download workspace archive'}).click()])
 const archivePath=(await download.path())!
 await expect(panel.getByRole('status')).toContainText('Workspace archive downloaded')
 await panel.getByLabel('Workspace archive ZIP').setInputFiles(archivePath)
 await panel.getByRole('button',{name:'Preview archive',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Archive validated')
 await expect(panel.getByLabel('Archive contents')).toContainText('Attachments')
 await panel.getByRole('button',{name:'Restore as separate workspace',exact:true}).click()
 const rootNode=panel.getByTestId('restored-workspace-root')
 await expect(rootNode).toBeVisible({timeout:15000})
 const restoredRoot=(await rootNode.textContent())!
 expect(restoredRoot).not.toBe(source.root)
 // Generated fixture sibling only; never clean any user-chosen directory.
 const container=path.dirname(restoredRoot)
 expect(path.dirname(container)).toBe(path.dirname(source.root))
 expect(path.basename(container)).toMatch(/^Reverie-Restored-workspace-/)
 const cfgFile=path.join(restoredRoot,'config','app.json')
 const cfg=JSON.parse(await fs.readFile(cfgFile,'utf8'));cfg.port=await unusedPort();await fs.writeFile(cfgFile,JSON.stringify(cfg))
 const binary=path.join(source.root,'runtime','ReveriePlanner'+(process.platform==='win32'?'.exe':''))
 const proc=spawn(binary,['--root',restoredRoot],{stdio:'ignore'})
 try {
  const url='http://127.0.0.1:'+cfg.port
  await expect.poll(async()=>{try{return (await api.get(url+'/api/health')).status()}catch{return 0}},{timeout:15000}).toBe(200)
  expect(await (await api.get(url+'/')).text()).toContain('<div id="root">')
  const restoredChild=await (await api.get(url+'/api/pages?id='+child.id)).json()
  expect(restoredChild.parentId).toBe(parent.id)
  expect(restoredChild.title).toBe(child.title)
  const attachment=await api.get(url+'/api/db/files?id='+file.id)
  expect(attachment.status()).toBe(200);expect(await attachment.text()).toBe('Portable attachment content')
  expect((await (await api.get(url+'/api/page-library')).json()).favorites).toContain(child.id)
  expect(await (await api.get(url+'/api/ai/providers')).json()).toEqual([])
  expect((await api.put(url+'/api/pages',{data:{...restoredChild,title:'Restored only'}})).status()).toBe(200)
  expect((await (await api.get('/api/pages?id='+child.id)).json()).title).toBe(child.title)
  expect(await (await api.get('/api/ai/providers')).json()).toEqual(beforeProviders)
  const metadata=JSON.parse(await fs.readFile(path.join(restoredRoot,'runtime','service.json'),'utf8'))
  expect((await api.post(url+'/api/shutdown',{headers:{Authorization:'Bearer '+metadata.token}})).status()).toBe(202)
  await expect.poll(()=>proc.exitCode,{timeout:15000}).toBe(0)
 } finally {
  if(proc.exitCode===null){proc.kill();await once(proc,'exit')}
  await fs.rm(container,{recursive:true,force:true})
 }
})

test('archive preview rejects invalid files and permits retry without changing current pages',async({page})=>{
 await page.goto('/')
 const before=await (await page.request.get('/api/pages')).json()
 await page.locator('.nav-item').filter({hasText:'Settings'}).click()
 const panel=page.getByRole('region',{name:'Workspace archive and restore'})
 await panel.getByLabel('Workspace archive ZIP').setInputFiles({name:'invalid.zip',mimeType:'application/zip',buffer:Buffer.from('not a ZIP')})
 await panel.getByRole('button',{name:'Preview archive',exact:true}).click()
 await expect(panel.getByRole('alert')).toContainText('not a valid workspace ZIP')
 await expect(panel.getByRole('button',{name:'Restore as separate workspace'})).toHaveCount(0)
 expect(await (await page.request.get('/api/pages')).json()).toEqual(before)
 const [download]=await Promise.all([page.waitForEvent('download'),panel.getByRole('button',{name:'Download workspace archive'}).click()])
 await panel.getByLabel('Workspace archive ZIP').setInputFiles((await download.path())!)
 await panel.getByRole('button',{name:'Preview archive',exact:true}).click()
 await expect(panel.getByRole('status')).toContainText('Archive validated')
 await expect(panel.getByRole('button',{name:'Restore as separate workspace'})).toBeEnabled()
})
