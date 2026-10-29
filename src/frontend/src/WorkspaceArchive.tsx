import { useEffect, useRef, useState } from 'react'

type Preview = {createdAt:string;schemaVersion:number;pages:number;records:number;attachments:number;bytes:number;root?:string}
const archiveLimit=512*1024*1024
export default function WorkspaceArchive() {
 const [file,setFile]=useState<File|null>(null)
 const [preview,setPreview]=useState<Preview|null>(null)
 const [busy,setBusy]=useState<'export'|'preview'|'restore'|''>('')
 const [error,setError]=useState('')
 const [notice,setNotice]=useState('')
 const abort=useRef<AbortController|null>(null)
 useEffect(()=>()=>abort.current?.abort(),[])
 const exportArchive=async()=>{
  setBusy('export');setError('');setNotice('')
  const controller=new AbortController();abort.current=controller
  try {
   const response=await fetch('/api/workspace/archive',{method:'POST',signal:controller.signal})
   if(!response.ok){const data=await response.json();throw new Error(data.error || 'Workspace archive could not be created')}
   const blob=await response.blob()
   const url=URL.createObjectURL(blob)
   const link=document.createElement('a');link.href=url;link.download='Reverie-Workspace-'+new Date().toISOString().slice(0,10)+'.zip';link.click()
   setTimeout(()=>URL.revokeObjectURL(url),1000)
   setNotice('Workspace archive downloaded. A copy is also saved in data/backups.')
  }catch(e){setError(controller.signal.aborted?'Archive download cancelled. A completed backup may remain in data/backups.':(e as Error).message)}finally{setBusy('');abort.current=null}
 }
 const upload=async(restore:boolean)=>{
  if(!file)return
  setBusy(restore?'restore':'preview');setError('');setNotice('')
  const controller=new AbortController();abort.current=controller
  try {
   const response=await fetch('/api/workspace/'+(restore?'restore':'restore-preview'),{method:'POST',headers:{'Content-Type':'application/zip'},body:file,signal:controller.signal})
   const data=await response.json()
   if(!response.ok)throw new Error(data.error || 'Workspace archive validation failed')
   setPreview(data)
   setNotice(restore?'A separate workspace was restored. Your current workspace was not replaced.':'Archive validated. Review the contents before creating a separate workspace.')
  }catch(e){setError(controller.signal.aborted?'Request cancelled. If a restore finished before cancellation, check the adjacent Reverie-Restored folder.':(e as Error).message)}finally{setBusy('');abort.current=null}
 }
 return <section className="archive-panel" aria-label="Workspace archive and restore" aria-busy={!!busy}>
  <div className="section-heading"><h3>Take your workspace with you</h3><span>Database + attachments</span></div>
  <p>Download a consistent archive of your documents, databases, favorites and attachment files, including recoverable trash. Provider settings, API keys and running-service credentials are excluded.</p>
  <p className="empty">The archive contains your private workspace content. Current limits: 512 MB compressed, 2 GB expanded and 10,000 files.</p>
  <button className="btn primary" disabled={!!busy} onClick={()=>void exportArchive()}>{busy==='export'?'Preparing archive…':'Download workspace archive'}</button>
  <h4>Restore a separate workspace</h4>
  <p>Choose a Reverie workspace ZIP to validate it first. Restoring creates a new adjacent directory and keeps the current workspace intact. Keep the application folder to run the restored data.</p>
  <label className="archive-file-label">Workspace archive<input type="file" aria-label="Workspace archive ZIP" accept=".zip,application/zip" disabled={!!busy} onChange={e=>{
   const selected=e.target.files?.[0] || null
   setPreview(null);setNotice('');setError('')
   if(selected && selected.size>archiveLimit){setFile(null);setError('Choose an archive smaller than 512 MB.');e.target.value='';return}
   setFile(selected)
  }} /></label>
  <div className="row archive-actions"><button className="btn" disabled={!file || !!busy} onClick={()=>void upload(false)}>{busy==='preview'?'Validating archive…':'Preview archive'}</button>
  {preview && !preview.root && <button className="btn primary" disabled={!!busy} onClick={()=>void upload(true)}>{busy==='restore'?'Restoring…':'Restore as separate workspace'}</button>}
  {busy && <button className="btn quiet" onClick={()=>abort.current?.abort()}>Cancel request</button>}</div>
  {preview && <div className="archive-preview" aria-label="Archive contents">
   <dl><div><dt>Pages</dt><dd>{preview.pages}</dd></div><div><dt>Records</dt><dd>{preview.records}</dd></div><div><dt>Attachments</dt><dd>{preview.attachments}</dd></div><div><dt>Expanded size</dt><dd>{(preview.bytes/1024/1024).toFixed(1)} MB</dd></div></dl>
   <p className="empty">Created {new Date(preview.createdAt).toLocaleString()} · Database schema {preview.schemaVersion}</p>
   {preview.root && <><p>Restored data directory</p><code data-testid="restored-workspace-root">{preview.root}</code><p>Stop the current app, then run the existing executable with <code>--root</code> and this directory. Reconfigure AI providers if needed. See docs/WORKSPACE_ARCHIVES.md for platform commands.</p></>}
  </div>}
  {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error}</p>}
 </section>
}
