import { useRef, useState } from 'react'
import type { DBProperty, DBRecord } from './DatabaseView'

export type BulkRecordEdit = { id: string; version: number; title?: string; values?: Record<string, unknown> }

function parseCell(property: DBProperty, raw: string): unknown {
  const text = raw.trim()
  switch (property.type) {
    case 'title':
      if (!text) throw new Error('Page titles cannot be empty.')
      return raw
    case 'text':
    case 'email':
    case 'url':
      return raw
    case 'number': {
      if (!text) return null
      const value = Number(text)
      if (!Number.isFinite(value)) throw new Error('Invalid number in ' + property.name)
      return value
    }
    case 'checkbox':
      if (['true', 'yes', '1'].includes(text.toLowerCase())) return true
      if (['false', 'no', '0', ''].includes(text.toLowerCase())) return false
      throw new Error('Use true/false, yes/no or 1/0 for ' + property.name)
    case 'date':
    case 'select':
      return text || null
    case 'multi_select':
      return text ? raw.split(',').map(value => value.trim()).filter(Boolean) : []
    default:
      throw new Error('Bulk paste is not supported for ' + property.name + ' (' + property.type + ').')
  }
}

// A TSV rectangle is mapped onto explicitly selected visible rows, in their
// current display order. Never silently truncate cells, rows or unsupported types.
function buildBulkPasteEdits(
  draft: string, startPropertyId: string, properties: DBProperty[],
  records: DBRecord[], selectedIds: string[]
): BulkRecordEdit[] {
  if (!draft || draft.length > 100000) throw new Error('Paste 1-100 rows (at most 100 KB).')
  const normalized = draft.replace(/\r\n?/g, '\n').replace(/\n$/, '')
  const lines = normalized.split('\n').map(line => line.split('\t'))
  const selected = records.filter(record => selectedIds.includes(record.id))
  if (!selected.length || selected.length > 100 || selected.length !== selectedIds.length) {
    throw new Error('Select 1-100 visible records before pasting.')
  }
  if (lines.length !== selected.length) throw new Error('Pasted row count must equal selected visible rows.')
  const width = lines[0].length
  if (lines.some(row => row.length !== width)) throw new Error('Every pasted row must have the same number of columns.')
  const start = properties.findIndex(property => property.id === startPropertyId)
  if (start < 0 || width > properties.length - start) throw new Error('Pasted columns exceed the visible table columns.')
  const columns = properties.slice(start, start + width)
  return selected.map((record, index) => {
    const edit: BulkRecordEdit = { id: record.id, version: record.version }
    const values: Record<string, unknown> = {}
    columns.forEach((property, column) => {
      const value = parseCell(property, lines[index][column])
      if (property.type === 'title') edit.title = value as string
      else values[property.id] = value
    })
    if (Object.keys(values).length) edit.values = values
    return edit
  })
}

export default function DatabaseBulkPaste({
  records, properties, selectedIds, draft, onDraftChange, startPropertyId, onStartPropertyChange,
  onApply, onRefreshVersions, disabled
}: {
  records: DBRecord[]
  properties: DBProperty[]
  selectedIds: string[]
  draft: string
  onDraftChange: (next: string) => void
  startPropertyId: string
  onStartPropertyChange: (next: string) => void
  onApply: (edits: BulkRecordEdit[]) => Promise<boolean>
  onRefreshVersions: () => Promise<void>
  disabled: boolean
}) {
  const [busy, setBusy] = useState(false)
  const [readingClipboard, setReadingClipboard] = useState(false)
  const [localError, setLocalError] = useState('')
  const start = properties.some(property => property.id === startPropertyId) ? startPropertyId : (properties[0]?.id || '')
  // Clipboard reads are asynchronous. Do not overwrite a draft or use a stale
  // row/column selection if the user edits the table while permission is pending.
  const latestDraft = useRef(draft)
  latestDraft.current = draft
  const latestSource = useRef({ start, selectedIds, records, properties })
  latestSource.current = { start, selectedIds, records, properties }
  const changeDraft = (next: string) => {
    latestDraft.current = next
    onDraftChange(next)
  }
  let edits: BulkRecordEdit[] = []
  let validation = ''
  if (draft) {
    try { edits = buildBulkPasteEdits(draft, start, properties, records, selectedIds) }
    catch (error) { validation = (error as Error).message }
  }
  // Reading the clipboard only prepares a draft. Never commit clipboard data
  // without the existing explicit review/confirmation and atomic API call.
  const readClipboard = async () => {
    if (busy || disabled || readingClipboard || draft) return
    setReadingClipboard(true)
    setLocalError('')
    try {
      if (!navigator.clipboard?.readText) throw new Error('Clipboard reading is unavailable in this browser.')
      const text = await navigator.clipboard.readText()
      if (latestDraft.current) throw new Error('A paste draft was entered while reading; clipboard data was not loaded.')
      const source = latestSource.current
      if (source.start !== start || source.selectedIds.join('\\0') !== selectedIds.join('\\0')) {
        throw new Error('Row or column selection changed while reading; retry clipboard paste.')
      }
      // Validate against the latest records before locking their visible order.
      buildBulkPasteEdits(text, source.start, source.properties, source.records, source.selectedIds)
      changeDraft(text)
    } catch (error) {
      setLocalError('Clipboard paste failed: ' + (error as Error).message)
    } finally {
      setReadingClipboard(false)
    }
  }
  const apply = async () => {
    if (!edits.length || disabled || busy) return
    if (!confirm('Apply ' + edits.length + ' record edits (' + draft.split(/\r?\n/)[0].split('\t').length + ' columns) atomically? Any conflict or invalid cell will roll back the entire paste.')) return
    setBusy(true)
    setLocalError('')
    try {
      const ok = await onApply(edits)
      if (ok) changeDraft('')
    } catch (error) {
      setLocalError((error as Error).message)
    } finally { setBusy(false) }
  }
  return <div role="group" aria-label="Bulk table paste" style={{ margin: '8px 0', padding: 8, border: '1px solid var(--border)' }}>
    <label>Start column{' '}
      <select aria-label="Bulk paste start column" value={start} onChange={event => onStartPropertyChange(event.target.value)}>
        {properties.map(property => <option key={property.id} value={property.id}>{property.name}</option>)}
      </select>
    </label>
    <label style={{ display: 'block' }}>Paste tab-separated cells (one row per selected record, in visible order)
      <textarea aria-label="Bulk paste TSV" value={draft} onChange={event => { changeDraft(event.target.value); setLocalError('') }} rows={3} style={{ display: 'block', width: '100%', maxWidth: 600 }} />
    </label>
    {draft && <small role="status">{validation || ('Ready to update ' + edits.length + ' records. All changes commit together.')}</small>}
    {localError && <div role="alert">{localError}</div>}
    <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
      <button aria-label="Read clipboard into bulk paste draft" disabled={busy || disabled || readingClipboard || !!draft} onClick={() => void readClipboard()}>{readingClipboard ? 'Reading clipboard…' : 'Paste from clipboard'}</button>
      <button aria-label="Apply bulk paste" disabled={busy || disabled || readingClipboard || !edits.length || !!validation} onClick={() => void apply()}>{busy ? 'Applying…' : 'Review and apply paste'}</button>
      <button aria-label="Refresh bulk record versions" disabled={busy || disabled || readingClipboard} onClick={() => void onRefreshVersions()}>Refresh record versions</button>
      <button aria-label="Discard bulk paste draft" disabled={!draft || busy || readingClipboard} onClick={() => { changeDraft(''); setLocalError('') }}>Discard draft</button>
    </div>
  </div>
}
