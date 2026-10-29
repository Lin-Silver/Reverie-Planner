import { useEffect, useState } from 'react'
import Dialog from './Dialog'

type Revision = {id: number; version: number; title: string; savedAt: string}
type Snapshot = {title: string; content: string; icon?: string}
type Trash = {id: string; title: string; version: number; deletedAt: string}
async function request<T>(url: string, body?: unknown): Promise<T> {
 const response = await fetch(url, body ? {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)} : undefined)
 if (!response.ok) { const error = await response.json(); throw new Error(error.error || 'Page recovery failed.') }
 return response.json()
}
// Preview saved text as text, never execute stored HTML or mount an editable
// BlockNote instance that could autosave the historical snapshot accidentally.
function previewText(content: string): string {
 try {
  const result: string[] = []
  const visit = (value: unknown) => {
   if (Array.isArray(value)) { value.forEach(visit); return }
   if (!value || typeof value !== 'object') return
   const block = value as {text?: unknown; content?: unknown; children?: unknown}
   if (typeof block.text === 'string') result.push(block.text)
   if (block.content) { visit(block.content); result.push('\n') }
   if (block.children) visit(block.children)
  }
  visit(JSON.parse(content))
  return result.join('') || '(No text blocks; download the saved blocks for complete content.)'
 } catch { return content || '(Empty page)' }
}
export default function PageRecovery({pageId, version, returnFocus, onClose, onRestored}: {pageId?: string; version?: number; returnFocus?: HTMLElement | null; onClose: () => void; onRestored: () => Promise<void>}) {
 const [revisions, setRevisions] = useState<Revision[]>([])
 const [trash, setTrash] = useState<Trash[]>([])
 const [selected, setSelected] = useState<Revision | null>(null)
 const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
 const [error, setError] = useState('')
 const [loading, setLoading] = useState(true)
 const [busy, setBusy] = useState(false)
 const [more, setMore] = useState(false)
 const [reload, setReload] = useState(0)
 useEffect(() => {
  let cancelled = false
  const load = pageId ? request<Revision[]>(`/api/page-history?pageId=${encodeURIComponent(pageId)}`) : request<Trash[]>('/api/page-trash')
  void load.then(items => { if (cancelled) return; if (pageId) { setRevisions(items as Revision[]); setMore(items.length === 100) } else setTrash(items as Trash[]) }).catch(e => { if (!cancelled) setError((e as Error).message) }).finally(() => {if (!cancelled) setLoading(false)})
  return () => {cancelled = true}
 }, [pageId, reload])
 useEffect(() => {
  let cancelled = false
  if (selected && pageId) void request<{snapshot: Snapshot}>(`/api/page-history?pageId=${encodeURIComponent(pageId)}&revisionId=${selected.id}`).then(item => {if (!cancelled) setSnapshot(item.snapshot)}).catch(e => {if (!cancelled) setError((e as Error).message)})
  return () => {cancelled = true}
 }, [selected, pageId, reload])
 const restore = async (item?: Trash) => {
  setBusy(true); setError('')
  try {
   await request(pageId ? '/api/page-history' : '/api/page-trash', pageId ? {pageId, version, revisionId: selected!.id} : {id: item!.id, version: item!.version})
   await onRestored()
   if (pageId) onClose(); else setReload(n => n + 1)
  } catch (e) { setError((e as Error).message) }
  finally {setBusy(false)}
 }
 return <Dialog label={pageId ? 'Page history' : 'Page trash'} onClose={() => {if (!busy) onClose()}} returnFocus={returnFocus} instant>
  <section className="modal page-recovery">
   <div className="section-heading"><h2>{pageId ? 'Page history' : 'Page trash'}</h2><button className="btn" disabled={busy} onClick={onClose}>Close</button></div>
   <p className="muted">{pageId ? 'Restore saved writing, title, icon and cover. The current version is kept in history; page location, links and database-owned titles stay current.' : 'Deleted pages stay here until restored. Restore parents before their child pages. Database items use Database trash. Showing up to 250 pages.'}</p>
   {error && <div role="alert"><p>{error}</p><button className="btn" disabled={busy} onClick={() => {setLoading(true); setError(''); setReload(n => n + 1)}}>Retry loading</button></div>}
   {loading ? <p role="status">Loading…</p> : pageId ? <>
    {!revisions.length && <p>No saved revisions yet. Earlier versions appear after the page changes.</p>}
    <div className="recovery-layout"><div className="recovery-list" role="group" aria-label="Saved revisions">{revisions.map(item => <button className="btn recovery-item" aria-pressed={selected?.id === item.id} key={item.id} onClick={() => {setSnapshot(null); setError(''); setSelected({...item})}} disabled={busy}><strong>{item.title}</strong><span>Version {item.version} · {new Date(item.savedAt).toLocaleString()}</span></button>)}</div>
    <div className="recovery-preview">{selected && !snapshot ? <p role="status">Loading preview…</p> : snapshot ? <><h3>{snapshot.icon} {snapshot.title}</h3><pre>{previewText(snapshot.content)}</pre><button className="btn" onClick={() => {const url=URL.createObjectURL(new Blob([snapshot.content],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download=`page-revision-${selected!.id}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}}>Download saved blocks</button><button className="btn primary" disabled={busy} onClick={() => void restore()}>{busy ? 'Restoring…' : 'Restore this revision'}</button></> : <p>Select a revision to preview it. Rich blocks are shown as plain text; download blocks for the full structure.</p>}</div></div>
    {more && <button className="btn" disabled={busy} onClick={async () => {setBusy(true);try{const items=await request<Revision[]>(`/api/page-history?pageId=${encodeURIComponent(pageId)}&before=${revisions.at(-1)!.id}`);setRevisions(current=>[...current,...items]);setMore(items.length===100)}catch(e){setError((e as Error).message)}finally{setBusy(false)}}}>Load older revisions</button>}
   </> : <>{!trash.length && <p>Page trash is empty.</p>}{trash.map(item => <div className="recovery-item" key={item.id}><div><strong>{item.title}</strong><p className="muted">{new Date(item.deletedAt).toLocaleString()}</p></div><button className="btn" disabled={busy} onClick={() => void restore(item)}>Restore page</button></div>)}</>}
  </section>
 </Dialog>
}
