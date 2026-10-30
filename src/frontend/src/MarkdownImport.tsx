import { useEffect, useRef, useState } from 'react'

type Source = {path: string; title: string; parentPath?: string; markdown: string}
type Preview = {digest: string; pages: Source[]; blockers: string[]}
type Result = {rootId: string; pages: Record<string, string>}
export default function MarkdownImport({onImported}: {onImported: () => Promise<void>}) {
 const [file, setFile] = useState<File | null>(null)
 const [preview, setPreview] = useState<Preview | null>(null)
 const [selected, setSelected] = useState('')
 const [blocks, setBlocks] = useState<{path: string; content: unknown}[]>([])
 const [busy, setBusy] = useState('')
 const [error, setError] = useState('')
 const [notice, setNotice] = useState('')
 const [complete, setComplete] = useState(false)
 const operation = useRef('')
 const abort = useRef<AbortController | null>(null)
 useEffect(() => () => abort.current?.abort(), [])
 const run = async (commit: boolean) => {
  if (!file) return
  const controller = new AbortController(); abort.current = controller
  setBusy(commit ? 'import' : 'preview'); setError(''); setNotice('')
  try {
   if (!commit) {
    setPreview(null); setBlocks([])
    const response = await fetch('/api/import/markdown-preview', {method: 'POST', body: file, signal: controller.signal})
    const data = await response.json()
    if (!response.ok) throw new Error(data.error || 'Package preview failed.')
    const plan = data as Preview
    setPreview(plan); setSelected(plan.pages[0].path)
    if (plan.blockers.length) return
    // Reuse the editor's parser, loaded only when importing. No remote service.
    const {BlockNoteEditor} = await import('@blocknote/core')
    const editor = BlockNoteEditor.create()
    const converted = plan.pages.map(p => {const content = editor.tryParseMarkdownToBlocks(p.markdown); return {path: p.path, content: content.length ? content : [{type: 'paragraph', content: []}]}})
    if (controller.signal.aborted) return
    const check = new FormData()
    check.set('file', file); check.set('digest', plan.digest); check.set('blocks', JSON.stringify(converted))
    const validation = await fetch('/api/import/markdown?validate=1', {method: 'POST', body: check, signal: controller.signal})
    if (!validation.ok) { const failure = await validation.json(); throw new Error(failure.error || 'Converted pages could not be validated.') }
    if (controller.signal.aborted) return
    setBlocks(converted)
    setNotice('Preview ready. Review the source and format limits before importing.')
   } else {
    if (!preview || !blocks.length) return
    const form = new FormData()
    form.set('file', file); form.set('digest', preview.digest)
    form.set('operationKey', operation.current); form.set('blocks', JSON.stringify(blocks))
    const response = await fetch('/api/import/markdown', {method: 'POST', body: form, signal: controller.signal})
    const data = await response.json()
    if (!response.ok) throw new Error(data.error || 'Import failed.')
    const result = data as Result
    setComplete(true)
    setNotice(`Imported ${Object.keys(result.pages).length} pages under Imported Markdown. Existing pages were kept.`)
    await onImported()
   }
  } catch (e) {
   if (!controller.signal.aborted) setError((e as Error).message)
  } finally { if (!controller.signal.aborted) setBusy(''); abort.current = null }
 }
 return <section className="archive-panel markdown-import" aria-label="Markdown package import" aria-busy={!!busy}>
  <div className="section-heading"><h3>Bring in Markdown pages</h3><span>Local text-only ZIP</span></div>
  <p>Preview a ZIP of UTF-8 Markdown pages. Each import creates a separate root; matching titles never replace your writing. Up to 100 pages, 1 MB per file and 16 MB per ZIP and expanded package.</p>
  <p className="empty">This first format excludes attachments, CSV databases and local heading anchors. HTML and editor-unsupported Markdown can be simplified or omitted. A folder uses its sibling Markdown page as parent, or the nearest parent page; other paths appear directly under the import root. Formatting uses the editor’s Markdown parser and may simplify rich content. Keep the original ZIP.</p>
  <label className="archive-file-label">Markdown package<input type="file" accept=".zip,application/zip" aria-label="Markdown package ZIP" disabled={!!busy} onChange={e => {
   const next=e.target.files?.[0] || null
   setPreview(null); setBlocks([]); setComplete(false); setNotice(''); setError('')
   operation.current=crypto.randomUUID()
   if (next && next.size > 16*1024*1024) {setFile(null);setError('Choose a ZIP smaller than 16 MB.');e.target.value='';return}
   setFile(next)
  }} /></label>
  <div className="row archive-actions">
   <button className="btn" disabled={!file || !!busy || complete} onClick={() => void run(false)}>{busy==='preview'?'Reading package…':'Preview Markdown package'}</button>
   {preview && !preview.blockers.length && <button className="btn primary" disabled={!!busy || !blocks.length || complete} onClick={() => void run(true)}>{busy==='import'?'Importing pages…':'Import as new pages'}</button>}
  </div>
  {preview && <div className="archive-preview" aria-label="Markdown package contents">
   <p><strong>{preview.pages.length} Markdown pages</strong></p>
   {!!preview.blockers.length && <div role="alert"><p>Import blocked. No pages were written.</p><ul>{preview.blockers.map(message => <li key={message}>{message}</li>)}</ul></div>}
   <label className="archive-file-label">Preview source page<select value={selected} onChange={e=>setSelected(e.target.value)}>{preview.pages.map(p=><option key={p.path} value={p.path}>{p.path} → {p.parentPath || 'Import root'}</option>)}</select></label>
   <pre className="markdown-source">{preview.pages.find(p=>p.path===selected)?.markdown}</pre>
  </div>}
  {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error} If the connection was interrupted, retry this import with the same selection; completed operations will not create duplicate pages.</p>}
 </section>
}
