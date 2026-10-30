import { useEffect, useRef, useState } from 'react'
import Dialog from './Dialog'

type TrashKind = 'record' | 'property' | 'view' | 'database'
type TrashItem = {id:string;title:string;deletedAt:string}
const KINDS:TrashKind[]=['record','property','view','database']
async function responseError(res:Response) {
  const data=await res.json().catch(()=>({error:res.statusText})) as {error?:string}
  return new Error(data.error||String(res.status))
}

export default function DatabaseTrash({databaseId,onClose,onRestored}:{
  databaseId:string;onClose:()=>void;onRestored:()=>void
}) {
  const [kind,setKind]=useState<TrashKind>('record')
  const [result,setResult]=useState<{key:string;items:TrashItem[]}>({key:'',items:[]})
  const [error,setError]=useState('')
  const [status,setStatus]=useState('')
  const [busy,setBusy]=useState('')
  const [loadedKey,setLoadedKey]=useState('')
  const [reload,setReload]=useState(0)
  const category=useRef<HTMLSelectElement>(null)
  const loadKey=databaseId+':'+kind+':'+reload
  const loading=loadedKey!==loadKey
  const items=result.key===loadKey?result.items:[]
  useEffect(()=>{
    const controller=new AbortController()
    const params=new URLSearchParams({kind})
    if(kind!=='database')params.set('databaseId',databaseId)
    void (async()=>{
      try {
        const res=await fetch('/api/db/trash?'+params,{signal:controller.signal})
        if(!res.ok)throw await responseError(res)
        const data=await res.json() as TrashItem[]
        if(!controller.signal.aborted)setResult({key:loadKey,items:data})
      }catch(e){
        if(!controller.signal.aborted){
          // A failed category load must never expose the previous category's
          // IDs as restore actions for the newly selected kind.
          setResult({key:loadKey,items:[]})
          setError('Could not load trash: '+(e as Error).message)
        }
      }
      finally{if(!controller.signal.aborted)setLoadedKey(loadKey)}
    })()
    return()=>controller.abort()
  },[databaseId,kind,loadKey])
  useEffect(()=>{if(status&&!busy)category.current?.focus()},[status,busy])
  const restore=async(item:TrashItem)=>{
    if(busy||loading)return
    setBusy(item.id)
    setError('')
    setStatus('')
    try {
      const res=await fetch('/api/db/trash',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({kind,id:item.id})})
      if(!res.ok)throw await responseError(res)
      setResult(current=>({...current,items:current.items.filter(row=>row.id!==item.id)}))
      setStatus('Restored '+item.title+'.')
      onRestored()
      setReload(n=>n+1)
    }catch(e){setError('Restore failed: '+(e as Error).message)}
    finally{setBusy('')}
  }
  return <Dialog label="Database trash" onClose={()=>{if(!busy)onClose()}} instant>
    <section className="modal database-trash">
      <header className="section-heading"><h2>Database trash</h2><button className="btn" aria-label="Close database trash" disabled={!!busy} onClick={onClose}>Close</button></header>
      <p className="muted">Restore the database before its items, and input properties before formulas or rollups. Saved values and attachments are retained. Showing up to 250 items per category.</p>
      <label>Items <select ref={category} aria-label="Trash category" value={kind} disabled={!!busy} onChange={e=>{setError('');setStatus('');setKind(e.target.value as TrashKind)}}>{KINDS.map(k=><option key={k} value={k}>{k}</option>)}</select></label>
      {error&&<div role="alert"><p>{error}</p><button className="btn" disabled={!!busy||loading} onClick={()=>{setError('');setReload(n=>n+1)}}>Retry loading</button></div>}
      {status&&<p role="status">{status}</p>}
      {loading?<p role="status">Loading deleted items…</p>:!items.length&&!error&&<p>No deleted {kind} items.</p>}
      <ul className="database-trash-list">{!loading&&items.map(item=><li className="recovery-item" key={item.id}>
        <span><strong>{item.title}</strong><small className="muted">{new Date(item.deletedAt).toLocaleString()}</small></span>
        <button className="btn" aria-label={'Restore '+item.title} disabled={!!busy||loading} onClick={()=>{void restore(item)}}>{busy===item.id?'Restoring…':'Restore'}</button>
      </li>)}</ul>
    </section>
  </Dialog>
}
