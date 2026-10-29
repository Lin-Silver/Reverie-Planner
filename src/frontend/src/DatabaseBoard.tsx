import { useRef, useState } from 'react'
import type { DBProperty, DBRecord } from './DatabaseView'

export default function DatabaseBoard({ records, props, updateRecord, groupBy, onOpenRecord }: {
  records: DBRecord[]
  props: DBProperty[]
  updateRecord: (record: DBRecord, patch: { values: Record<string, unknown> }) => Promise<void>
  groupBy: string
  onOpenRecord?: (pageId: string) => void
}) {
  const [dragId, setDragId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const moving = useRef(false)
  const groupProp = props.find(p => p.id === groupBy && p.type === 'select') || props.find(p => p.type === 'select')
  if (!groupProp) return <p>Add a Select property to use Board view.</p>
  let options: string[] = []
  try { options = JSON.parse(groupProp.config || '{}').options || [] } catch { /* legacy config */ }
  // Map keeps user-defined names separate from Object.prototype members.
  // null is distinct from a real option called "(empty)".
  const groups = new Map<string | null, DBRecord[]>([[null, []]])
  for (const option of options) groups.set(option, [])
  for (const record of records) {
    const value = record.values?.[groupProp.id]
    const key = value == null ? null : String(value)
    const group = groups.get(key)
    if (group) group.push(record)
    else groups.set(key, [record])
  }
  const move = async (record: DBRecord, key: string | null) => {
    if (moving.current || (record.values?.[groupProp.id] ?? null) === key) return
    moving.current = true
    setBusy(true)
    setDragId(null)
    try { await updateRecord(record, { values: { [groupProp.id]: key } }) }
    finally { moving.current = false; setBusy(false) }
  }
  return <div style={{ display: 'flex', gap: 12, overflowX: 'auto' }}>
    {Array.from(groups, ([key, items]) => <section key={JSON.stringify(key)} data-testid="board-group" data-group={key ?? '(empty)'}
      aria-label={key === null ? 'Unassigned group' : 'Group ' + key}
      onDragOver={event => { if (!busy) event.preventDefault() }} onDrop={event => {
        event.preventDefault()
        const record = records.find(item => item.id === dragId)
        if (record) void move(record, key)
      }} style={{ minWidth: 220, padding: 8, background: 'var(--card)', border: '1px solid var(--border)', borderRadius: 8 }}>
      <strong>{key ?? '(empty)'} ({items.length})</strong>
      {items.map(item => <div data-testid="board-card" key={item.id} draggable={!busy}
        onDragStart={() => setDragId(item.id)} onDragEnd={() => setDragId(null)}
        style={{ padding: 8, marginTop: 8, border: '1px solid var(--border)', borderRadius: 6 }}>
        {item.title} <button onClick={() => item.pageId && onOpenRecord?.(item.pageId)}>Open</button>
        <label style={{ display: 'block', marginTop: 6 }}>Move to <select aria-label={'Move ' + item.title + ' to group'}
          disabled={busy} value={key === null ? '' : 'option:' + key} onChange={event => {
            void move(item, event.target.value === '' ? null : event.target.value.slice('option:'.length))
          }}>
          <option value="">Unassigned</option>
          {options.map(option => <option key={option} value={'option:' + option}>{option}</option>)}
          {key !== null && !options.includes(key) && <option value={'option:' + key}>{key} (unavailable option)</option>}
        </select></label>
      </div>)}
    </section>)}
  </div>
}
