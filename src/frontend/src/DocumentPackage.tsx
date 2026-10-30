import {useEffect, useRef, useState} from 'react'

type Source = {id:string; parentId?:string; title:string; content:string}
type Preview = {digest:string; pages:Source[]; attachments:number; attachmentBytes:number}
export default function DocumentPackage({onImported}:{onImported:()=>Promise<void>}) {
 const [pages,setPages]=useState<Source[]>([])
 const [root,setRoot]=useState('')
 const [file,setFile]=useState<File|null>(null)
 const [preview,setPreview]=useState<Preview|null>(null)
 const [busy,setBusy]=useState('')
 const [error,setError]=useState('')
 const [notice,setNotice]=useState('')
 const [complete,setComplete]=useState(false)
 const [loadAttempt,setLoadAttempt]=useState(0)
 const operation=useRef('')
 const active=useRef<AbortController|null>(null)
 useEffect(()=>()=>active.current?.abort(),[])
 useEffect(()=>{
  const controller=new AbortController()
  fetch('/api/pages',{signal:controller.signal}).then(async response=>{
   if(!response.ok)throw new Error('Could not load document choices. Retry loading documents.')
   return response.json() as Promise<Source[]>
  }).then(setPages).catch(e=>{if(!controller.signal.aborted)setError((e as Error).message)})
  return ()=>controller.abort()
 },[loadAttempt])
 const run=async(kind:'export'|'preview'|'import')=>{
  const controller=new AbortController();active.current=controller
  setBusy(kind);setError('');setNotice('')
  try {
   if(kind==='preview')setPreview(null)
   let endpoint='/api/document-package'
   if(kind==='export')endpoint+='?rootId='+encodeURIComponent(root)
   if(kind==='preview')endpoint+='?preview=1'
   if(kind==='import')endpoint+='?digest='+encodeURIComponent(preview!.digest)+'&operationKey='+encodeURIComponent(operation.current)
   const response=await fetch(endpoint,{method:kind==='export'?'GET':'POST',body:kind==='export'?undefined:file,signal:controller.signal})
   if(!response.ok){const data=await response.json();throw new Error(data.error||'Document transfer failed.')}
   if(kind==='export'){
    const blob=await response.blob();const url=URL.createObjectURL(blob)
    const link=document.createElement('a');link.href=url;link.download='Reverie-documents.reverie.json';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
    setNotice('Document package downloaded. It contains saved documents and local attachments; keep a workspace archive for history and databases.')
   }else if(kind==='preview'){
    setPreview(await response.json() as Preview);setNotice('Preview ready. Import creates a separate copy; existing pages stay unchanged.')
   }else{
    const result=await response.json() as {pages:Record<string,string>}
    setComplete(true);setNotice(`Imported ${Object.keys(result.pages).length} documents under Imported documents.`)
    await onImported();setLoadAttempt(value=>value+1)
   }
  }catch(e){if(!controller.signal.aborted)setError((e as Error).message)}
  finally{if(!controller.signal.aborted){setBusy('');active.current=null}}
 }
 return <section className="archive-panel" aria-label="Document package transfer" aria-busy={!!busy}>
  <div className="section-heading"><h3>Transfer a document tree</h3><span>Editor blocks and local attachments</span></div>
  <p>Move saved personal documents without Markdown conversion. Titles, icons, nested pages, supported editor blocks, internal links and local page attachments travel together in a readable JSON package. Import creates new IDs and a separate root.</p>
  <p className="empty">Up to 100 pages, 2 MB per document, 16 MB total writing and 16 MB attachments; 48 MB per file. Covers, legacy plain text, database-owned or linked pages, external media and links outside the selected tree block transfer. Project membership, history, trash, favorites and databases require a complete workspace archive. Unsaved writing is not included.</p>
  <label className="archive-file-label">Document tree to export<select aria-label="Document tree to export" value={root} disabled={!!busy} onChange={e=>setRoot(e.target.value)}><option value="">Choose a saved page</option>{pages.map(page=><option value={page.id} key={page.id}>{page.title||'Untitled'} · {page.id}</option>)}</select></label>
  <div className="row archive-actions"><button className="btn" disabled={!root||!!busy} onClick={()=>void run('export')}>{busy==='export'?'Preparing documents…':'Download document package'}</button><button className="btn quiet" disabled={!!busy} onClick={()=>{setError('');setLoadAttempt(value=>value+1)}}>Reload document choices</button></div>
  <label className="archive-file-label">Document package<input type="file" accept=".json,application/json" aria-label="Document package JSON" disabled={!!busy} onChange={e=>{
   setPreview(null);setComplete(false);setNotice('');setError('');operation.current=crypto.randomUUID()
   const next=e.target.files?.[0]||null
   if(next&&next.size>48*1024*1024){setFile(null);setError('Choose a document package no larger than 48 MB.');e.target.value='';return}
   setFile(next)
  }}/></label>
  <div className="row archive-actions"><button className="btn" disabled={!file||!!busy||complete} onClick={()=>void run('preview')}>{busy==='preview'?'Reading documents…':'Preview document package'}</button>{preview&&<button className="btn primary" disabled={!!busy||complete} onClick={()=>void run('import')}>{busy==='import'?'Importing documents…':'Import document copy'}</button>}</div>
  {preview&&<div className="archive-preview" aria-label="Document package preview"><p><strong>{preview.pages.length} documents</strong> · {preview.attachments} attachments · {preview.attachmentBytes.toLocaleString()} attachment bytes</p><ul>{preview.pages.map(page=><li key={page.id}>{page.title||'Untitled'}{page.parentId?' · Child page':' · Tree root'}</li>)}</ul><details><summary>View preserved editor blocks</summary>{preview.pages.map(page=><div key={page.id}><h4>{page.title||'Untitled'}</h4><pre className="markdown-source">{page.content}</pre></div>)}</details></div>}
  {notice&&<p role="status">{notice}</p>}{error&&<p role="alert">{error} If a successful response was interrupted, retry with the same selected file to retrieve the completed copy.</p>}
 </section>
}
