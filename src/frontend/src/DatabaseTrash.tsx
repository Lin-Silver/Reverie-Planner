import { useCallback, useEffect, useState } from 'react'

type TrashKind = 'record' | 'property' | 'view' | 'database'
type TrashItem = {id:string;title:string;deletedAt:string}
const KINDS:TrashKind[]=['record','property','view','database']

export default function DatabaseTrash({databaseId,onClose,onRestored}:{
  databaseId:string;onClose:()=>void;onRestored:()=>void
}) {
  const [kind,setKind]=useState<TrashKind>('record')
  const [items,setItems]=useState<TrashItem[]>([])
  const [error,setError]=useState('')
  const [busy,setBusy]=useState('')
  const fetchTrash=useCallback(async()=>{
    const params=new URLSearchParams({kind})
    if(kind!=='database')params.set('databaseId',databaseId)
    const res=await fetch('/api/db/trash?'+params)
    if(!res.ok)throw new Error(await res.text())
    setItems(await res.json())
  },[databaseId,kind])
  useEffect(()=>{let live=true
    void fetchTrash().catch(e=>{if(live)setError('Could not load trash: '+(e as Error).message)})
    return()=>{live=false}
  },[fetchTrash])
  const restore=async(item:TrashItem)=>{
    if(busy)return
    setBusy(item.id)
    setError('')
    try {
      const res=await fetch('/api/db/trash',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({kind,id:item.id})})
      if(!res.ok){
        const data=await res.json().catch(()=>({error:res.statusText})) as {error?:string}
        throw new Error(data.error||String(res.status))
      }
      await fetchTrash()
      onRestored()
    }catch(e){setError('Restore failed: '+(e as Error).message)}
    finally{setBusy('')}
  }
  return <div role="dialog" aria-label="Database trash" aria-modal="true" style={{position:'fixed',inset:0,zIndex:105,display:'grid',placeItems:'center',background:'rgba(0,0,0,.55)',padding:16}}>
    <section style={{background:'var(--card)',border:'1px solid var(--border)',borderRadius:12,width:'min(560px,100%)',maxHeight:'85vh',overflowY:'auto',padding:20}}>
      <header style={{display:'flex',justifyContent:'space-between',alignItems:'center'}}><h3>Database trash</h3><button aria-label="Close database trash" onClick={onClose}>Close</button></header>
      <label>Items <select aria-label="Trash category" value={kind} onChange={e=>setKind(e.target.value as TrashKind)}>{KINDS.map(k=><option key={k} value={k}>{k}</option>)}</select></label>
      {error&&<div role="alert" style={{color:'#dc2626'}}>{error}</div>}
      {!items.length&&<p>No deleted {kind} items.</p>}
      <ul style={{padding:0,listStyle:'none'}}>{items.map(item=><li key={item.id} style={{display:'flex',gap:10,alignItems:'center',justifyContent:'space-between',padding:8,borderBottom:'1px solid var(--border)'}}>
        <span><strong>{item.title}</strong><small style={{display:'block'}}>{item.deletedAt}</small></span>
        <button aria-label={'Restore '+item.title} disabled={!!busy} onClick={()=>{void restore(item)}}>{busy===item.id?'Restoring…':'Restore'}</button>
      </li>)}</ul>
    </section>
  </div>
}
