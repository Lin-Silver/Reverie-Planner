import { useCallback, useEffect, useMemo, useState } from 'react'

const API = 'http://127.0.0.1:18765'

type DBProperty = { id: string; databaseId: string; name: string; type: string; config: string; position: number; visible: boolean; version: number }
type DBView = { id: string; databaseId: string; name: string; type: string; config: string; position: number; version: number }
type Database = { id: string; pageId: string; title: string; icon: string; properties: DBProperty[]; views: DBView[]; version: number }
type DBRecord = { id: string; databaseId: string; title: string; pageId: string; values: Record<string, any>; version: number; createdAt: string; updatedAt: string }

async function api(path: string, opts?: RequestInit) {
  const res = await fetch(`${API}${path}`, { headers: { 'Content-Type': 'application/json' }, ...opts })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  if (res.status === 204) return null
  return res.json()
}

export default function DatabaseView({ databaseId, onBack }: { databaseId: string; onBack: () => void }) {
  const [db, setDb] = useState<Database | null>(null)
  const [records, setRecords] = useState<DBRecord[]>([])
  const [viewId, setViewId] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const d = await api(`/api/databases?id=${databaseId}`) as Database
      setDb(d)
      const vs = d.views || []
      if (!viewId && vs.length) setViewId(vs[0].id)
      const recs = await api(`/api/db/records?databaseId=${databaseId}`) as DBRecord[]
      setRecords(recs || [])
    } catch (e) { setError((e as Error).message) }
    setLoading(false)
  }, [databaseId, viewId])

  useEffect(() => { load() }, [load])

  const view = db?.views.find(v => v.id === viewId) || db?.views[0]
  const props = db?.properties || []

  const updateRecord = async (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => {
    const body: any = { id: rec.id, title: patch.title ?? rec.title, values: patch.values ?? {}, version: rec.version }
    const updated = await api('/api/db/records', { method: 'PUT', body: JSON.stringify(body) }) as DBRecord
    setRecords(rs => rs.map(r => r.id === rec.id ? { ...r, ...updated, values: { ...r.values, ...(updated.values || {}) } } : r))
  }

  const addRecord = async () => {
    const rec = await api('/api/db/records', { method: 'POST', body: JSON.stringify({ databaseId, title: 'Untitled' }) }) as DBRecord
    setRecords(rs => [...rs, rec])
  }

  const addProperty = async () => {
    const name = prompt('Property name?') || 'Property'
    const type = prompt('Type (text/number/checkbox/select/date/url/email)?') || 'text'
    await api('/api/db/properties', { method: 'POST', body: JSON.stringify({ databaseId, name, type }) })
    await load()
  }

  const deleteProperty = async (pid: string) => {
    await api(`/api/db/properties?id=${pid}`, { method: 'DELETE' })
    await load()
  }

  const renameDb = async () => {
    if (!db) return
    const title = prompt('Database name?', db.title) || db.title
    const updated = await api('/api/databases', { method: 'PUT', body: JSON.stringify({ id: db.id, title, icon: db.icon, version: db.version }) }) as Database
    setDb(updated)
  }

  if (loading) return <div style={{ padding: 24 }}>Loading database…</div>
  if (error) return <div style={{ padding: 24, color: 'red' }}>{error}</div>
  if (!db) return <div style={{ padding: 24 }}>Not found</div>

  return (
    <div style={{ padding: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
        <button className="btn" onClick={onBack}>← Back</button>
        <h2 style={{ margin: 0 }}>{db.icon} {db.title}</h2>
        <button className="btn" onClick={renameDb}>Rename</button>
        <button className="btn" onClick={addProperty}>+ Property</button>
        <button className="btn primary" onClick={addRecord}>+ New</button>
        <div style={{ flex: 1 }} />
        <select value={viewId} onChange={e => setViewId(e.target.value)}>
          {db.views.map(v => <option key={v.id} value={v.id}>{v.name} ({v.type})</option>)}
        </select>
        <button className="btn" onClick={async () => {
          const v = await api('/api/db/views', { method: 'POST', body: JSON.stringify({ databaseId, name: 'New view', type: 'table' }) }) as DBView
          setDb({ ...db, views: [...db.views, v] })
          setViewId(v.id)
        }}>+ View</button>
      </div>
      {view?.type === 'table' && <TableView records={records} props={props} updateRecord={updateRecord} deleteProperty={deleteProperty} />}
      {view?.type === 'board' && <BoardView records={records} props={props} />}
      {view?.type === 'calendar' && <CalendarView records={records} props={props} />}
      {view?.type === 'list' && <ListView records={records} />}
      {view?.type === 'gallery' && <GalleryView records={records} />}
      {!['table', 'board', 'calendar', 'list', 'gallery'].includes(view?.type || '') && <div>Unknown view</div>}
    </div>
  )
}

function TableView({ records, props, updateRecord, deleteProperty }: { records: DBRecord[]; props: DBProperty[]; updateRecord: any; deleteProperty: any }) {
  const [sortProp, setSortProp] = useState<string>('')
  const [filter, setFilter] = useState('')
  const visibleProps = props.filter(p => p.visible)
  const filtered = records.filter(r => {
    if (!filter) return true
    const hay = (r.title + ' ' + Object.values(r.values || {}).join(' ')).toLowerCase()
    return hay.includes(filter.toLowerCase())
  }).sort((a, b) => {
    if (!sortProp) return 0
    const va = a.values?.[sortProp] ?? a.title
    const vb = b.values?.[sortProp] ?? b.title
    return String(va).localeCompare(String(vb))
  })

  return (
    <div style={{ overflowX: 'auto' }}>
      <div style={{ marginBottom: 8, display: 'flex', gap: 8 }}>
        <input placeholder="Filter…" value={filter} onChange={e => setFilter(e.target.value)} />
        <select value={sortProp} onChange={e => setSortProp(e.target.value)}>
          <option value="">Sort by…</option>
          {props.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      </div>
      <table style={{ borderCollapse: 'collapse', width: '100%' }}>
        <thead>
          <tr>
            {visibleProps.map(p => (
              <th key={p.id} style={{ border: '1px solid var(--border)', padding: 6, background: 'var(--card)', minWidth: 120 }}>
                {p.name}
                <button onClick={() => deleteProperty(p.id)} style={{ marginLeft: 6, fontSize: 10 }}>×</button>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {filtered.map(rec => (
            <tr key={rec.id}>
              {visibleProps.map(p => (
                <td key={p.id} style={{ border: '1px solid var(--border)', padding: 4 }}>
                  <Cell prop={p} value={p.type === 'title' ? rec.title : rec.values?.[p.id]} onChange={(v: any) => {
                    if (p.type === 'title') updateRecord(rec, { title: v })
                    else updateRecord(rec, { values: { [p.id]: v } })
                  }} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Cell({ prop, value, onChange }: { prop: DBProperty; value: any; onChange: (v: any) => void }) {
  if (prop.type === 'checkbox') {
    return <input type="checkbox" checked={!!value} onChange={e => onChange(e.target.checked)} />
  }
  if (prop.type === 'number') {
    return <input type="number" defaultValue={value ?? ''} onBlur={e => onChange(e.target.value === '' ? null : Number(e.target.value))} style={{ width: '100%' }} />
  }
  if (prop.type === 'select') {
    let opts: string[] = []
    try { opts = JSON.parse(prop.config || '{}').options || [] } catch {}
    return (
      <select value={value ?? ''} onChange={e => onChange(e.target.value)}>
        <option value="">—</option>
        {opts.map(o => <option key={o} value={o}>{o}</option>)}
      </select>
    )
  }
  return <input defaultValue={value ?? ''} onBlur={e => onChange(e.target.value)} style={{ width: '100%', border: 'none', background: 'transparent' }} />
}

function BoardView({ records, props }: any) {
  const groupProp = props.find((p: DBProperty) => p.type === 'select') || props[0]
  const groups = useMemo(() => {
    const g: Record<string, DBRecord[]> = {}
    records.forEach((r: DBRecord) => {
      const key = String(r.values?.[groupProp?.id] ?? '(empty)')
      ;(g[key] = g[key] || []).push(r)
    })
    return g
  }, [records, groupProp])
  return (
    <div style={{ display: 'flex', gap: 12, overflowX: 'auto' }}>
      {Object.entries(groups).map(([g, recs]) => (
        <div key={g} style={{ minWidth: 220, background: 'var(--card)', padding: 8, borderRadius: 8 }}>
          <div style={{ fontWeight: 600, marginBottom: 8 }}>{g} ({recs.length})</div>
          {recs.map((r: DBRecord) => (
            <div key={r.id} style={{ padding: 8, marginBottom: 6, background: 'var(--background)', borderRadius: 4, border: '1px solid var(--border)' }}>
              {r.title}
            </div>
          ))}
        </div>
      ))}
      {Object.keys(groups).length === 0 && <div>No records</div>}
    </div>
  )
}

function CalendarView({ records, props }: any) {
  const dateProp = props.find((p: DBProperty) => p.type === 'date')
  const byDate: Record<string, DBRecord[]> = {}
  records.forEach((r: DBRecord) => {
    const d = dateProp ? r.values?.[dateProp.id] : null
    const key = d ? String(d).slice(0, 10) : 'No date'
    ;(byDate[key] = byDate[key] || []).push(r)
  })
  return (
    <div>
      {Object.entries(byDate).map(([d, recs]) => (
        <div key={d} style={{ marginBottom: 12 }}>
          <div style={{ fontWeight: 600 }}>{d}</div>
          <ul>{recs.map((r: DBRecord) => <li key={r.id}>{r.title}</li>)}</ul>
        </div>
      ))}
    </div>
  )
}

function ListView({ records }: { records: DBRecord[] }) {
  return (
    <ul style={{ listStyle: 'none', padding: 0 }}>
      {records.map(r => (
        <li key={r.id} style={{ padding: 8, borderBottom: '1px solid var(--border)' }}>{r.title}</li>
      ))}
    </ul>
  )
}

function GalleryView({ records }: { records: DBRecord[] }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))', gap: 12 }}>
      {records.map(r => (
        <div key={r.id} style={{ padding: 12, background: 'var(--card)', borderRadius: 8, border: '1px solid var(--border)' }}>
          <div style={{ fontWeight: 600 }}>{r.title}</div>
        </div>
      ))}
    </div>
  )
}