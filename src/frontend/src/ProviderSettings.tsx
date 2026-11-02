import {useEffect, useState} from 'react'
import type {FormEvent} from 'react'

export type Provider = {id:string;label:string;provider:string;baseURL:string;model:string;temperature:number;maxTokens:number;enabled:boolean;streaming:boolean;toolCalling:boolean;apiKeyConfigured:boolean}
type Snapshot = {revision:string;providers:Provider[]}
type Draft = Omit<Provider,'apiKeyConfigured'> & {apiKey:string;clearAPIKey:boolean}
const blank=():Draft=>({id:crypto.randomUUID(),label:'',provider:'openai-compatible',baseURL:'http://127.0.0.1:11434/v1',model:'',temperature:0.3,maxTokens:2048,enabled:true,streaming:true,toolCalling:true,apiKey:'',clearAPIKey:false})
const withoutSecretFlag=(provider:Provider):Omit<Provider,'apiKeyConfigured'>=>({id:provider.id,label:provider.label,provider:provider.provider,baseURL:provider.baseURL,model:provider.model,temperature:provider.temperature,maxTokens:provider.maxTokens,enabled:provider.enabled,streaming:provider.streaming,toolCalling:provider.toolCalling})
const fromProvider=(provider:Provider):Draft=>({...withoutSecretFlag(provider),apiKey:'',clearAPIKey:false})
async function request<T>(url:string,init?:RequestInit):Promise<T>{
 const response=await fetch(url,{...init,headers:{'Content-Type':'application/json',...init?.headers}})
 const data=await response.json().catch(()=>({error:'Request failed.'}))
 if(!response.ok)throw new Error(data.error||'Request failed.')
 return data as T
}
export default function ProviderSettings({onChanged}:{onChanged:(providers:Provider[])=>void}){
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null)
 const [draft,setDraft]=useState<Draft|null>(null)
 const [baseline,setBaseline]=useState('')
 const [busy,setBusy]=useState(false)
 const [error,setError]=useState('')
 const [notice,setNotice]=useState('')
 const dirty=!!draft && JSON.stringify(draft)!==baseline
 const editing=snapshot?.providers.find(p=>p.id===draft?.id)
 function edit(next:Draft|null){setDraft(next);setBaseline(next?JSON.stringify(next):'');setError('');setNotice('')}
 async function load(discard=false){
  if(discard&&dirty&&!window.confirm('Discard unsaved provider changes and reload?'))return
  setBusy(true);setError('');setNotice('')
  try{const next=await request<Snapshot>('/api/provider-management');setSnapshot(next);onChanged(next.providers);edit(next.providers[0]?fromProvider(next.providers[0]):null)}catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 useEffect(()=>{let active=true;request<Snapshot>('/api/provider-management').then(next=>{if(!active)return;setSnapshot(next);onChanged(next.providers);const first=next.providers[0]?fromProvider(next.providers[0]):null;setDraft(first);setBaseline(first?JSON.stringify(first):'')}).catch(e=>{if(active)setError((e as Error).message)});return()=>{active=false}},[onChanged])
 function choose(id:string){if(dirty&&!window.confirm('Discard unsaved provider changes?'))return;const p=snapshot?.providers.find(x=>x.id===id);edit(p?fromProvider(p):null)}
 function add(){if(dirty&&!window.confirm('Discard unsaved provider changes?'))return;const next=blank();setDraft(next);setBaseline('');setError('');setNotice('')}
 const change=<K extends keyof Draft>(key:K,value:Draft[K])=>setDraft(current=>current?{...current,[key]:value}:current)
 async function save(event:FormEvent){
  event.preventDefault();if(!draft||!snapshot||busy)return
  setBusy(true);setError('');setNotice('')
  const inputs=snapshot.providers.map(withoutSecretFlag)
  const index=inputs.findIndex(p=>p.id===draft.id)
  if(index>=0)inputs[index]=draft;else inputs.push(draft)
  try{const next=await request<Snapshot>('/api/provider-management',{method:'PUT',body:JSON.stringify({revision:snapshot.revision,providers:inputs})});setSnapshot(next);onChanged(next.providers);const saved=next.providers.find(p=>p.id===draft.id)!;edit(fromProvider(saved));setNotice('Provider saved.')}
  catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 async function remove(){
  if(!draft||!snapshot||!editing||busy||!window.confirm(`Delete provider “${draft.label}” and its saved credential?`))return
  setBusy(true);setError('');setNotice('')
  try{const providers=snapshot.providers.filter(p=>p.id!==draft.id).map(withoutSecretFlag);const next=await request<Snapshot>('/api/provider-management',{method:'PUT',body:JSON.stringify({revision:snapshot.revision,providers})});setSnapshot(next);onChanged(next.providers);edit(next.providers[0]?fromProvider(next.providers[0]):null);setNotice('Provider deleted.')}
  catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 async function testConnection(){
  if(!draft||!editing||dirty||busy)return
  setBusy(true);setError('');setNotice('Testing saved settings…')
  try{await request('/api/ai/test-connection',{method:'POST',body:JSON.stringify({providerId:draft.id})});setNotice('Connection successful. The model returned a response.')}
  catch(e){setNotice('');setError((e as Error).message)}finally{setBusy(false)}
 }
 return <section className="provider-panel" aria-label="AI provider settings">
  <div className="section-heading"><div><span className="eyebrow">Optional AI</span><h3>AI providers</h3></div><button type="button" className="btn" disabled={busy||!snapshot} onClick={add}>Add provider</button></div>
  <p className="appearance-intro">Connect a local or remote OpenAI-compatible model. Your workspace works without AI.</p>
  {snapshot&&snapshot.providers.length>0&&<label className="provider-field">Provider<select aria-label="Provider to edit" value={editing?.id||''} disabled={busy} onChange={e=>choose(e.target.value)}>{!editing&&<option value="">New provider</option>}{snapshot.providers.map(p=><option key={p.id} value={p.id}>{p.label}</option>)}</select></label>}
  {snapshot&&!draft&&<p>No providers configured. Add one when you need AI.</p>}
  {draft&&<form onSubmit={save} className="provider-form"><fieldset className="provider-form-fields" disabled={busy}>
   <label className="provider-field">Name<input required maxLength={200} value={draft.label} onChange={e=>change('label',e.target.value)} /></label>
   <label className="provider-field">Base URL<input required type="url" maxLength={2048} value={draft.baseURL} onChange={e=>change('baseURL',e.target.value)} /></label>
   <p className="appearance-hint">Use HTTPS for remote services; HTTP is allowed for loopback addresses. Include the API path, usually /v1.</p>
   <label className="provider-field">Model<input required maxLength={256} value={draft.model} onChange={e=>change('model',e.target.value)} /></label>
   <label className="provider-field">API key<input type="password" autoComplete="off" maxLength={8192} value={draft.apiKey} disabled={draft.clearAPIKey} onChange={e=>change('apiKey',e.target.value)} placeholder={editing?.apiKeyConfigured?'Saved key stays unchanged if left blank':'Optional for local models'} /></label>
   {editing?.apiKeyConfigured&&<label className="provider-check"><input type="checkbox" checked={draft.clearAPIKey} onChange={e=>setDraft({...draft,clearAPIKey:e.target.checked,apiKey:''})}/> Remove saved API key on save</label>}
   <div className="provider-numbers"><label className="provider-field">Temperature<input type="number" min="0" max="2" step="0.1" required value={draft.temperature} onChange={e=>change('temperature',e.target.valueAsNumber)} /></label><label className="provider-field">Maximum tokens<input type="number" min="1" max="131072" step="1" required value={draft.maxTokens} onChange={e=>change('maxTokens',e.target.valueAsNumber)} /></label></div>
   <div className="provider-flags"><label className="provider-check"><input type="checkbox" checked={draft.enabled} onChange={e=>change('enabled',e.target.checked)}/> Enabled</label><label className="provider-check"><input type="checkbox" checked={draft.streaming} onChange={e=>change('streaming',e.target.checked)}/> Streaming</label><label className="provider-check"><input type="checkbox" checked={draft.toolCalling} onChange={e=>change('toolCalling',e.target.checked)}/> Tool calling</label></div>
   <div className="provider-actions"><button className="btn primary" type="submit" disabled={busy||!dirty}>Save provider</button><button className="btn" type="button" disabled={busy||!editing||dirty} onClick={testConnection}>Test connection</button>{editing&&<button className="btn quiet" type="button" disabled={busy} onClick={remove}>Delete provider</button>}</div>
   <p className="appearance-hint">Testing sends a short request to the saved model and may use your provider's quota. Save changes first.</p>
  </fieldset></form>}
  {error&&<p role="alert">{error}</p>}{notice&&<p role="status">{notice}</p>}
  <button type="button" className="btn quiet" disabled={busy} onClick={()=>void load(true)}>Reload providers</button>
 </section>
}
