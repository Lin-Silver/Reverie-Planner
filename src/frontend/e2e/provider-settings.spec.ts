import {expect,test} from '@playwright/test'

type Provider={id:string;label:string;provider:string;baseURL:string;model:string;temperature:number;maxTokens:number;enabled:boolean;streaming:boolean;toolCalling:boolean;apiKeyConfigured:boolean}
type Snapshot={revision:string;providers:Provider[]}
const input=(p:Provider)=>({id:p.id,label:p.label,provider:p.provider,baseURL:p.baseURL,model:p.model,temperature:p.temperature,maxTokens:p.maxTokens,enabled:p.enabled,streaming:p.streaming,toolCalling:p.toolCalling})
async function snapshot(request:import('@playwright/test').APIRequestContext):Promise<Snapshot>{const r=await request.get('/api/provider-management');expect(r.ok()).toBe(true);return await r.json()}
async function clean(request:import('@playwright/test').APIRequestContext){const s=await snapshot(request);const remaining=s.providers.filter(p=>!p.label.startsWith('E2E provider'));if(remaining.length!==s.providers.length)expect((await request.put('/api/provider-management',{data:{revision:s.revision,providers:remaining.map(input)}})).ok()).toBe(true)}
test.afterEach(async({request})=>{await clean(request)})
async function settings(page:import('@playwright/test').Page){await page.goto('/');await page.getByRole('button',{name:'Settings',exact:true}).click();const panel=page.getByRole('region',{name:'AI provider settings'});await expect(panel.getByRole('button',{name:'Add provider'})).toBeEnabled();return panel}
test('create, test, edit, retain and remove a provider without revealing its key',async({page,request})=>{
 const panel=await settings(page)
 await panel.getByRole('button',{name:'Add provider'}).click()
 await panel.getByLabel('Name',{exact:true}).fill('E2E provider local')
 await panel.getByLabel('Base URL').fill('http://127.0.0.1:11434/v1')
 await panel.getByLabel('Model',{exact:true}).fill('local-model')
 await panel.getByLabel('API key').fill('synthetic-provider-key')
 await panel.getByRole('button',{name:'Save provider'}).click()
 await expect(panel.getByRole('status')).toContainText('Provider saved')
 await expect(panel.getByLabel('API key')).toHaveValue('')
 await expect(panel.getByLabel('API key')).toHaveAttribute('placeholder',/Saved key/)
 const current=await snapshot(request);const created=current.providers.find(p=>p.label==='E2E provider local')!
 expect(created.apiKeyConfigured).toBe(true);expect(JSON.stringify(current)).not.toContain('synthetic-provider-key')
 let tests=0
 await page.route('**/api/ai/test-connection',async route=>{tests++;expect(route.request().postDataJSON()).toEqual({providerId:created.id});await route.fulfill({status:200,contentType:'application/json',body:'{"ok":true}'})})
 await panel.getByRole('button',{name:'Test connection'}).click()
 await expect(panel.getByRole('status')).toContainText('Connection successful');expect(tests).toBe(1)
 await panel.getByLabel('Model',{exact:true}).fill('local-model-updated')
 await expect(panel.getByRole('button',{name:'Test connection'})).toBeDisabled()
 await panel.getByRole('button',{name:'Save provider'}).click()
 await expect(panel.getByRole('status')).toContainText('Provider saved')
 expect((await snapshot(request)).providers.find(p=>p.id===created.id)?.apiKeyConfigured).toBe(true)
 await panel.getByLabel('Remove saved API key on save').check()
 await panel.getByRole('button',{name:'Save provider'}).click()
 await expect(panel.getByRole('status')).toContainText('Provider saved')
 expect((await snapshot(request)).providers.find(p=>p.id===created.id)?.apiKeyConfigured).toBe(false)
 page.once('dialog',dialog=>dialog.accept())
 await panel.getByRole('button',{name:'Delete provider'}).click()
 await expect(panel.getByRole('status')).toContainText('Provider deleted')
 expect((await snapshot(request)).providers.some(p=>p.id===created.id)).toBe(false)
})
test('stale provider settings preserve draft until explicit reload',async({page,request})=>{
 let s=await snapshot(request)
 const newProvider={id:'e2e-conflict',label:'E2E provider conflict',provider:'openai-compatible',baseURL:'http://127.0.0.1:11434/v1',model:'before',temperature:0.3,maxTokens:256,enabled:true,streaming:true,toolCalling:true}
 expect((await request.put('/api/provider-management',{data:{revision:s.revision,providers:[...s.providers.map(input),newProvider]}})).ok()).toBe(true)
 const panel=await settings(page);await panel.getByLabel('Provider to edit').selectOption(newProvider.id)
 await panel.getByLabel('Model',{exact:true}).fill('my draft')
 s=await snapshot(request)
 expect((await request.put('/api/provider-management',{data:{revision:s.revision,providers:s.providers.map(p=>p.id===newProvider.id?{...input(p),model:'remote change'}:input(p))}})).ok()).toBe(true)
 await panel.getByRole('button',{name:'Save provider'}).click()
 await expect(panel.getByRole('alert')).toContainText('changed in another window')
 await expect(panel.getByLabel('Model',{exact:true})).toHaveValue('my draft')
 expect((await snapshot(request)).providers.find(p=>p.id===newProvider.id)?.model).toBe('remote change')
 page.once('dialog',dialog=>dialog.accept());await panel.getByRole('button',{name:'Reload providers'}).click()
 await panel.getByLabel('Provider to edit').selectOption(newProvider.id)
 await expect(panel.getByLabel('Model',{exact:true})).toHaveValue('remote change')
})
