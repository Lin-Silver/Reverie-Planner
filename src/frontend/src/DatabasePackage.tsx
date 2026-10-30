import {useEffect, useRef, useState} from 'react'

type Property = {id:string;name:string;type:string;visible:boolean}
type Preview = {digest:string;title:string;properties:Property[];views:{name:string;type:string}[];records:number;sample:{page:{title:string};values:Record<string,unknown>}[]}
export default function DatabasePackage({onImported}:{onImported:()=>Promise<void>}) {
 const [databases,setDatabases]=useState<{id:string;title:string}[]>([])
 const [selected,setSelected]=useState('')
 const [file,setFile]=useState<File|null>(null)
 const [preview,setPreview]=useState<Preview|null>(null)
 const [busy,setBusy]=useState('')
 const [error,setError]=useState('')
 const [notice,setNotice]=useState('')
 const [complete,setComplete]=useState(false)
 const [loadAttempt,setLoadAttempt]=useState(0)
 const [inputMode,setInputMode]=useState<'keyboard'|'pointer'>('keyboard')
 const status=useRef<HTMLParagraphElement>(null)
 const operation=useRef('')
 const active=useRef<AbortController|null>(null)
 useEffect(()=>()=>active.current?.abort(),[])
 useEffect(()=>{if(complete)status.current?.focus({preventScroll:true})},[complete])
 useEffect(()=>{
  const controller=new AbortController()
  fetch('/api/databases',{signal:controller.signal}).then(async response=>{
   if(!response.ok)throw new Error('Could not load database choices. Retry loading databases.')
   return response.json() as Promise<{id:string;title:string}[]>
  }).then(setDatabases).catch(e=>{if(!controller.signal.aborted)setError((e as Error).message)})
  return ()=>controller.abort()
 },[loadAttempt])
 const run=async(kind:'export'|'preview'|'import')=>{
  const controller=new AbortController();active.current=controller;setBusy(kind);setError('');setNotice('')
  try{
   if(kind==='preview')setPreview(null)
   let endpoint='/api/database-package'
   if(kind==='export')endpoint+='?databaseId='+encodeURIComponent(selected)
   if(kind==='preview')endpoint+='?preview=1'
   if(kind==='import')endpoint+='?digest='+encodeURIComponent(preview!.digest)+'&operationKey='+encodeURIComponent(operation.current)
   const response=await fetch(endpoint,{method:kind==='export'?'GET':'POST',body:kind==='export'?undefined:file,signal:controller.signal})
   if(!response.ok){const data=await response.json();throw new Error(data.error||'Database transfer failed.')}
   if(kind==='export'){
    const url=URL.createObjectURL(await response.blob());const link=document.createElement('a');link.href=url;link.download='Reverie-database.reverie.json';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
    setNotice('Database package downloaded. Keep a workspace archive for attachments, history and trash.')
   }else if(kind==='preview'){
    setPreview(await response.json() as Preview);setNotice('Preview ready. Confirm to create an independent database with new record and view IDs.')
   }else{
    const result=await response.json() as {records:number};setComplete(true);setNotice(`Imported ${result.records} records with independent fields and views.`)
    try{await onImported()}catch{setError('The database copy was saved, but the page list could not refresh. Reload the app to see it.')}
    setLoadAttempt(value=>value+1)
   }
  }catch(e){if(!controller.signal.aborted)setError((e as Error).message)}
  finally{if(!controller.signal.aborted){setBusy('');active.current=null}}
 }
 return <section className="archive-panel database-transfer" aria-label="Database package transfer" aria-busy={!!busy} data-input={inputMode} onPointerDown={()=>setInputMode('pointer')} onKeyDown={()=>setInputMode('keyboard')}>
  <div className="section-heading"><h3>Transfer a database</h3><span>Typed records and independent views</span></div>
  <p>Move a saved database as a readable JSON file. Field types, display order, visibility, record writing and supported view settings stay together. Preview before creating an independent copy.</p>
  <details><summary>Supported content and limits</summary><p>Up to 1,000 live records, 32 fields, 32 views and 16 MB per file. Title, Text, Number, Checkbox, Date, URL and Email fields with default configuration are supported. Relations, formulas, select fields, attachments, nested pages, covers and unknown view settings block export. Only links between included backing pages travel. History, trash, project membership and linked pages elsewhere stay in the original workspace. Use a full workspace archive for them. Unsaved writing is not included.</p></details>
  <h4>Download a saved database</h4>
  <label className="archive-file-label">Database to export<select aria-label="Database to export" value={selected} disabled={!!busy} onChange={e=>setSelected(e.target.value)}><option value="">Choose a saved database</option>{databases.map(database=><option key={database.id} value={database.id}>{database.title} · {database.id}</option>)}</select></label>
  <div className="row archive-actions"><button className="btn" disabled={!selected||!!busy} onClick={()=>void run('export')}>{busy==='export'?'Preparing database…':'Download database package'}</button><button className="btn quiet" disabled={!!busy} onClick={()=>{setError('');setLoadAttempt(value=>value+1)}}>Reload database choices</button></div>
  <h4>Import a separate copy</h4>
  <ol className="transfer-steps" aria-label="Database import progress">{['Choose a file','Review preview','Copy created'].map((step,index)=>{const current=complete?2:preview?1:0;return <li key={step} aria-current={current===index?'step':undefined} data-complete={index<current||undefined}><span aria-hidden="true">{index<current?'✓':index+1}</span>{step}</li>})}</ol>
  <label className="archive-file-label">Database package<input type="file" accept=".json,application/json" aria-label="Database package JSON" disabled={!!busy} onChange={e=>{
   setPreview(null);setComplete(false);setNotice('');setError('');operation.current=crypto.randomUUID()
   const next=e.target.files?.[0]||null
   if(next&&next.size>16*1024*1024){setFile(null);setError('Choose a database package no larger than 16 MB.');e.target.value='';return}
   setFile(next)
  }}/></label>
  <div className="row archive-actions"><button className="btn" disabled={!file||!!busy||complete} onClick={()=>void run('preview')}>{busy==='preview'?'Reading database…':'Preview database package'}</button>{preview&&<button className="btn primary" disabled={!!busy||complete} onClick={()=>void run('import')}>{busy==='import'?'Importing database…':'Import database copy'}</button>}</div>
  {preview&&<div className="archive-preview" aria-label="Database package preview"><h4>{preview.title}</h4><p><strong>{preview.records} records</strong> · {preview.properties.length} fields · {preview.views.length} views</p><ul>{preview.properties.map(property=><li key={property.id}>{property.name} · {property.type}{property.visible?'':' · hidden'}</li>)}</ul><p>Views: {preview.views.map(view=>`${view.name} (${view.type})`).join(', ')}</p><div className="csv-sample" tabIndex={0} role="region" aria-label="Database package sample"><table><caption>First {preview.sample.length} records</caption><thead><tr>{preview.properties.map(property=><th scope="col" key={property.id}>{property.name}</th>)}</tr></thead><tbody>{preview.sample.map((record,index)=><tr key={index}>{preview.properties.map(property=><td key={property.id}>{property.type==='title'?record.page.title:record.values[property.id]===null||record.values[property.id]===undefined?'∅ (empty)':String(record.values[property.id])}</td>)}</tr>)}</tbody></table></div></div>}
  <p className="transfer-status" role="status" tabIndex={-1} ref={status}>{busy==='export'?'Preparing the saved database…':busy==='preview'?'Checking fields, records and view references…':busy==='import'?'Creating the independent copy…':notice||'Choose a database to download, or a package to preview.'}</p>{error&&<p role="alert">{error}{!complete&&' If an import response was interrupted, retry with the same selected file to retrieve the completed copy.'}</p>}
 </section>
}
