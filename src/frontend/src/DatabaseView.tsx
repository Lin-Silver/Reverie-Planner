import { useCallback, useEffect, useState } from 'react'



type ViewType = 'table' | 'board' | 'calendar' | 'list' | 'gallery'
const VIEW_TYPES: ViewType[] = ['table', 'board', 'calendar', 'list', 'gallery']
type ViewConfig = { filter?: string; sort?: string; groupBy?: string; dateProperty?: string }

type DBProperty = { id: string; databaseId: string; name: string; type: string; config: string; position: number; visible: boolean; version: number }
type DBView = { id: string; databaseId: string; name: string; type: string; config: string; position: number; version: number }
type Database = { id: string; pageId: string; title: string; icon: string; properties: DBProperty[]; views: DBView[]; version: number }
type DBRecord = { id: string; databaseId: string; title: string; pageId: string; values: Record<string, any>; version: number; createdAt: string; updatedAt: string }

async function api(path: string, opts?: RequestInit) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' }, ...opts })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  if (res.status === 204) return null
  return res.json()
}

export default function DatabaseView({ databaseId, viewId: initialViewId, onBack, onOpenRecord, onLinkedPageCreated }: { databaseId: string; viewId?: string; onBack: () => void; onOpenRecord?: (pageId: string) => void; onLinkedPageCreated?: () => void }) {
  const [db, setDb] = useState<Database | null>(null)
  const [records, setRecords] = useState<DBRecord[]>([])
  const [viewId, setViewId] = useState<string>(initialViewId || '')
  const [newViewType, setNewViewType] = useState<ViewType>('table')
  const [filterText, setFilterText] = useState('')
  const [sortProp, setSortProp] = useState('')
  const [groupBy, setGroupBy] = useState('')
  const [dateProperty, setDateProperty] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const d = await api(`/api/databases?id=${databaseId}`) as Database
      setDb(d)
      const vs = d.views || []
      setViewId(old => vs.some(v => v.id === old) ? old : (vs.find(v => v.id === initialViewId)?.id || vs[0]?.id || ''))
      const recs = await api(`/api/db/records?databaseId=${databaseId}`) as DBRecord[]
      setRecords(recs || [])
    } catch (e) { setError((e as Error).message) }
    setLoading(false)
  }, [databaseId, initialViewId])

  useEffect(() => { load() }, [load])

  const view = db?.views.find(v => v.id === viewId) || db?.views[0]
  const props = db?.properties || []
  useEffect(() => {
    let cfg: ViewConfig = {}
    try { cfg = JSON.parse(view?.config || '{}') as ViewConfig } catch { /* old invalid view */ }
    setFilterText(cfg.filter || '')
    setSortProp(cfg.sort || '')
    setGroupBy(cfg.groupBy || '')
    setDateProperty(cfg.dateProperty || '')
  }, [view?.id, view?.config])

  const saveViewSettings = async () => {
    if (!view) return
    try {
      const updated = await api('/api/db/views', { method: 'PUT', body: JSON.stringify({ ...view, config: JSON.stringify({ filter: filterText, sort: sortProp, groupBy, dateProperty }) }) }) as DBView
      setDb(current => current && ({ ...current, views: current.views.map(v => v.id === updated.id ? updated : v) }))
      setError('')
    } catch (e) { setError((e as Error).message) }
  }
  const visibleRecords = records.filter(rec => !filterText || (rec.title + ' ' + Object.values(rec.values || {}).join(' ')).toLowerCase().includes(filterText.toLowerCase())).sort((a,b) => {
    if (!sortProp) return 0
    const left = sortProp === 'title' ? a.title : a.values?.[sortProp]
    const right = sortProp === 'title' ? b.title : b.values?.[sortProp]
    return typeof left === 'number' && typeof right === 'number' ? left-right : String(left ?? '').localeCompare(String(right ?? ''), undefined, {numeric:true})
  })

  const updateRecord = async (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => {
    const body: any = { id: rec.id, title: patch.title ?? rec.title, values: patch.values ?? {}, version: rec.version }
    const updated = await api('/api/db/records', { method: 'PUT', body: JSON.stringify(body) }) as DBRecord
    setError('')
    setRecords(rs => rs.map(r => r.id === rec.id ? { ...r, ...updated, values: { ...r.values, ...(updated.values || {}) } } : r))
  }

  const safelyUpdateRecord = async (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => {
    try { await updateRecord(rec, patch) } catch (e) { setError(`Save failed: ${(e as Error).message}`) }
  }
  const addRecord = async () => {
    const rec = await api('/api/db/records', { method: 'POST', body: JSON.stringify({ databaseId, title: 'Untitled' }) }) as DBRecord
    setRecords(rs => [...rs, rec])
  }

  const addProperty = async () => {
    const name = prompt('Property name?') || 'Property'
    const type = prompt('Type (text/number/checkbox/select/date/url/email)?') || 'text'
    let config = '{}'
    if (type === 'select' || type === 'multi_select') {
      const raw = prompt('Options, comma-separated (for example: Todo,Done)', 'Todo,Done')
      if (raw === null) return
      config = JSON.stringify({ options: [...new Set(raw.split(',').map(x => x.trim()).filter(Boolean))] })
    }
    await api('/api/db/properties', { method: 'POST', body: JSON.stringify({ databaseId, name, type, config }) })
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
  // Save errors remain visible without hiding the editor or unsaved input.
  if (!db) return <div style={{ padding: 24 }}>Not found</div>

  return (
    <div style={{ padding: 16 }}>
      {error && <div role="alert" style={{ marginBottom: 8, color: '#dc2626' }}>{error} <button onClick={() => { setError(''); void load() }}>Reload latest</button></div>}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
        <button className="btn" onClick={onBack}>← Back</button>
        <h2 style={{ margin: 0 }}>{db.icon} {db.title}</h2>
        <button className="btn" onClick={renameDb}>Rename</button>
        <button className="btn" onClick={addProperty}>+ Property</button>
        <button className="btn primary" onClick={addRecord}>+ New</button>
        <div style={{ flex: 1 }} />
        <select aria-label="Active database view" value={viewId} onChange={e => setViewId(e.target.value)}>
          {db.views.map(v => <option key={v.id} value={v.id}>{v.name} ({v.type})</option>)}
        </select>
        <select aria-label="New view type" value={newViewType} onChange={e => setNewViewType(e.target.value as ViewType)}>{VIEW_TYPES.map(t => <option key={t} value={t}>{t}</option>)}</select>
        <button className="btn" onClick={async () => {
          try {
            const v = await api('/api/db/views', { method: 'POST', body: JSON.stringify({ databaseId, name: newViewType[0].toUpperCase()+newViewType.slice(1), type: newViewType }) }) as DBView
            setDb(current => current && ({ ...current, views: [...current.views, v] }))
            setViewId(v.id)
            setError('')
          } catch (e) { setError((e as Error).message) }
        }}>+ View</button>
        <button className="btn" onClick={async () => {
          try {
            const pg = await api('/api/pages', { method: 'POST', body: JSON.stringify({ title: `${db.title} (linked)`, workspace: true, createdBy: 'You' }) }) as any
            await api('/api/db/linked-view', { method: 'POST', body: JSON.stringify({ databaseId, pageId: pg.id, name: 'Linked view' }) })
            onLinkedPageCreated?.()
            alert('Linked view page created: open it from the sidebar.')
          } catch (e) { setError((e as Error).message) }
        }}>+ Linked view page</button>
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
        <input aria-label="Database filter" placeholder="Filter records" value={filterText} onChange={e => setFilterText(e.target.value)} />
        <select aria-label="Database sort" value={sortProp} onChange={e => setSortProp(e.target.value)}><option value="">No sort</option><option value="title">Title</option>{props.filter(p => p.type !== 'title').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
        {view?.type === 'board' && <select aria-label="Board group by" value={groupBy} onChange={e => setGroupBy(e.target.value)}><option value="">First select property</option>{props.filter(p => p.type === 'select').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>}
        {view?.type === 'calendar' && <select aria-label="Calendar date property" value={dateProperty} onChange={e => setDateProperty(e.target.value)}><option value="">First date property</option>{props.filter(p => p.type === 'date').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>}
        <button onClick={() => void saveViewSettings()}>Save view settings</button>
      </div>
      {view?.type === 'table' && <TableView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} deleteProperty={deleteProperty} onOpenRecord={onOpenRecord} />}
      {view?.type === 'board' && <BoardView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} groupBy={groupBy} onOpenRecord={onOpenRecord} />}
      {view?.type === 'calendar' && <CalendarView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} dateProperty={dateProperty} onOpenRecord={onOpenRecord} />}
      {view?.type === 'list' && <ListView records={visibleRecords} onOpenRecord={onOpenRecord} />}
      {view?.type === 'gallery' && <GalleryView records={visibleRecords} onOpenRecord={onOpenRecord} />}
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

function BoardView({ records, props, updateRecord }: any) {
  const groupProp = props.find((p: DBProperty) => p.type === 'select') || props.find((p: DBProperty) => p.type === 'text')
  const groups: Record<string, DBRecord[]> = {}
  records.forEach((r: DBRecord) => {
    const key = String(r.values?.[groupProp?.id] ?? '(empty)')
    ;(groups[key] = groups[key] || []).push(r)
  })
  // ensure empty group exists
  if (groupProp?.type === 'select') {
    let opts: string[] = []
    try { opts = JSON.parse(groupProp.config || '{}').options || [] } catch {}
    opts.forEach(o => { if (!groups[o]) groups[o] = [] })
    if (!groups['(empty)']) groups['(empty)'] = groups['(empty)'] || []
  }
  const [dragId, setDragId] = useState<string | null>(null)
  const onDrop = async (groupKey: string) => {
    if (!dragId || !groupProp) return
    const rec = records.find((r: DBRecord) => r.id === dragId)
    if (!rec) return
    const newVal = groupKey === '(empty)' ? null : groupKey
    await updateRecord(rec, { values: { [groupProp.id]: newVal } })
    setDragId(null)
  }
  return (
    <div style={{ display: 'flex', gap: 12, overflowX: 'auto' }}>
      {Object.entries(groups).map(([g, recs]) => (
        <div key={g} style={{ minWidth: 220, background: 'var(--card)', padding: 8, borderRadius: 8 }}
          onDragOver={e => e.preventDefault()}
          onDrop={() => onDrop(g)}>
          <div style={{ fontWeight: 600, marginBottom: 8 }}>{g} ({recs.length})</div>
          {recs.map((r: DBRecord) => (
            <div key={r.id} draggable onDragStart={() => setDragId(r.id)}
              style={{ padding: 8, marginBottom: 6, background: 'var(--background)', borderRadius: 4, border: '1px solid var(--border)', cursor: 'grab' }}>
              {r.title}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

function CalendarView({ records, props }: any) {
  const dateProp = props.find((p: DBProperty) => p.type === 'date')
  const [cursor, setCursor] = useState(new Date())
  const year = cursor.getFullYear()
  const month = cursor.getMonth()
  const firstDay = new Date(year, month, 1)
  const startWeekday = firstDay.getDay()
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells: (number | null)[] = []
  for (let i = 0; i < startWeekday; i++) cells.push(null)
  for (let d = 1; d <= daysInMonth; d++) cells.push(d)
  const byDay: Record<number, DBRecord[]> = {}
  records.forEach((r: DBRecord) => {
    const v = dateProp ? r.values?.[dateProp.id] : null
    if (v) {
      const dt = new Date(String(v))
      if (dt.getFullYear() === year && dt.getMonth() === month) {
        const day = dt.getDate()
        ;(byDay[day] = byDay[day] || []).push(r)
      }
    }
  })
  return (
    <div>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
        <button className="btn" onClick={() => setCursor(new Date(year, month - 1, 1))}>‹</button>
        <div style={{ fontWeight: 600 }}>{cursor.toLocaleString('default', { month: 'long', year: 'numeric' })}</div>
        <button className="btn" onClick={() => setCursor(new Date(year, month + 1, 1))}>›</button>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 4 }}>
        {['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].map(d => <div key={d} style={{ fontWeight: 600, textAlign: 'center' }}>{d}</div>)}
        {cells.map((c, i) => (
          <div key={i} style={{ minHeight: 60, border: '1px solid var(--border)', padding: 4, background: 'var(--card)' }}>
            {c && <div style={{ fontSize: 12, color: 'var(--muted)' }}>{c}</div>}
            {c && (byDay[c] || []).map(r => <div key={r.id} style={{ fontSize: 12 }}>{r.title}</div>)}
          </div>
        ))}
      </div>
    </div>
  )
}

function ListView({ records }: { records: DBRecord[] }) {
  const [open, setOpen] = useState<string | null>(null)
  return (
    <ul style={{ listStyle: 'none', padding: 0 }}>
      {records.map(r => (
        <li key={r.id} style={{ padding: 8, borderBottom: '1px solid var(--border)', cursor: 'pointer' }} onClick={() => setOpen(open === r.id ? null : r.id)}>
          <div style={{ fontWeight: 600 }}>{r.title}</div>
          {open === r.id && (
            <div style={{ fontSize: 12, color: 'var(--muted)', marginTop: 4 }}>
              {Object.entries(r.values || {}).map(([k, v]) => <div key={k}>{k}: {String(v)}</div>)}
            </div>
          )}
        </li>
      ))}
    </ul>
  )
}

function GalleryView({ records }: { records: DBRecord[] }) {
  const [open, setOpen] = useState<string | null>(null)
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))', gap: 12 }}>
      {records.map(r => (
        <div key={r.id} onClick={() => setOpen(open === r.id ? null : r.id)} style={{ padding: 12, background: 'var(--card)', borderRadius: 8, border: '1px solid var(--border)', cursor: 'pointer' }}>
          <div style={{ fontWeight: 600 }}>{r.title}</div>
          {open === r.id && <div style={{ fontSize: 12, color: 'var(--muted)', marginTop: 4 }}>{Object.entries(r.values || {}).map(([k, v]) => <div key={k}>{k}: {String(v)}</div>)}</div>}
        </div>
      ))}
    </div>
  )
}
