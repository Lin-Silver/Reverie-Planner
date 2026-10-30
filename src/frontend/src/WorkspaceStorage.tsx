import { useEffect, useState } from 'react'
import WorkspaceArchive from './WorkspaceArchive'
import MarkdownImport from './MarkdownImport'
import DocumentPackage from './DocumentPackage'
import CSVImport from './CSVImport'
import DatabasePackage from './DatabasePackage'

type Workspace = {version:string;root:string;database:string;attachments:string;backups:string}
export default function WorkspaceStorage({onImported}: {onImported: () => Promise<void>}) {
  const [workspace,setWorkspace]=useState<Workspace|null>(null)
  const [error,setError]=useState('')
  const [notice,setNotice]=useState('')
  const [busy,setBusy]=useState(false)
  useEffect(()=>{
    const controller=new AbortController()
    fetch('/api/workspace',{signal:controller.signal}).then(async response=>{
      if(!response.ok)throw new Error('Could not load workspace paths')
      return response.json() as Promise<Workspace>
    }).then(setWorkspace).catch(e=>{if(!controller.signal.aborted)setError(String(e))})
    return()=>controller.abort()
  },[])
  return <div className="storage-panel">
    <section aria-label="Portable workspace">
    <h3>Local workspace {workspace && <small>v{workspace.version}</small>}</h3>
    <p>Your documents stay in this folder. Stop the app before moving or copying the entire workspace.</p>
    {workspace && <dl>{(['root','database','attachments','backups'] as const).map(key=><div key={key}><dt>{key}</dt><dd><code>{workspace[key]}</code></dd></div>)}</dl>}
    <button className="btn primary" disabled={busy || !workspace} onClick={async()=>{
      setBusy(true);setError('');setNotice('')
      try {
        const response=await fetch('/api/backup',{method:'POST'})
        if(!response.ok)throw new Error('Could not create database snapshot')
        setNotice('Database snapshot created in data/backups. Copy attachments and configuration separately for a complete backup.')
      }catch(e){setError((e as Error).message)}finally{setBusy(false)}
    }}>{busy?'Creating snapshot…':'Create database snapshot'}</button>
    <p role="status">{notice}</p>{error && <p role="alert">{error}</p>}
    </section>
    <WorkspaceArchive />
    <MarkdownImport onImported={onImported} />
    <DocumentPackage onImported={onImported} />
    <CSVImport onImported={onImported} />
    <DatabasePackage onImported={onImported} />
  </div>
}
