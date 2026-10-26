import { useEffect, useState } from 'react'
type Property = {id:string;name:string;databaseId:string}
type FileMeta = {id:string;name:string;size:number;contentType:string}

export default function DatabaseFileCell({prop,recordId,value,onChange}:{
  prop:Property;recordId:string;value:unknown;onChange:(next:string[])=>void
}) {
  const ids=Array.isArray(value)?value as string[]:[]
  const [files,setFiles]=useState<FileMeta[]>([])
  const [busy,setBusy]=useState(false)
  const [error,setError]=useState('')
  const refresh=async()=>{
    const res=await fetch('/api/db/files?recordId='+encodeURIComponent(recordId))
    if(!res.ok)throw new Error('Could not load attachments: '+res.status)
    setFiles(await res.json())
  }
  useEffect(()=>{let active=true
    fetch('/api/db/files?recordId='+encodeURIComponent(recordId)).then(async res=>{
      if(!res.ok)throw new Error(String(res.status))
      return await res.json() as FileMeta[]
    }).then(items=>{if(active)setFiles(items)}).catch(e=>{if(active)setError(String(e))})
    return ()=>{active=false}
  },[recordId])
  const upload=async(file:File)=>{
    if(file.size>(8<<20)){setError('File exceeds the 8 MiB limit');return}
    const form=new FormData()
    form.set('databaseId',prop.databaseId)
    form.set('recordId',recordId)
    form.set('file',file)
    setBusy(true)
    setError('')
    try{
      const res=await fetch('/api/db/files',{method:'POST',body:form})
      if(!res.ok){const body=await res.json().catch(()=>({error:res.statusText})) as {error?:string};throw new Error(body.error||String(res.status))}
      const info=await res.json() as FileMeta
      onChange([...ids,info.id])
      await refresh()
    }catch(e){setError((e as Error).message)}
    finally{setBusy(false)}
  }
  return <div style={{display:'grid',gap:4}}>
    {ids.map(id=>{
      const file=files.find(f=>f.id===id)
      return <div key={id} style={{display:'flex',gap:4}}>
        <a href={'/api/db/files?id='+encodeURIComponent(id)} download>{file?.name||id}</a>
        <button aria-label={'Remove attachment '+(file?.name||id)} onClick={()=>onChange(ids.filter(x=>x!==id))}>×</button>
      </div>
    })}
    <label style={{fontSize:12}}>Attach file <input aria-label={'Upload '+prop.name} type="file" disabled={busy} onChange={e=>{
      const file=e.target.files?.[0]
      if(file)void upload(file)
      e.target.value=''
    }}/></label>
    {error&&<span role="alert">{error}</span>}
    {busy&&<small>Uploading…</small>}
  </div>
}
