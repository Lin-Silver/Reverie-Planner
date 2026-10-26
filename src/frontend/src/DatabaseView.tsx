import { useCallback, useEffect, useState } from 'react'
import { matchesFilters, newFilterGroup, type FilterGroup } from './databaseFilters'
import DatabaseFilterBuilder from './DatabaseFilterBuilder'
import DatabasePropertyDialog from './DatabasePropertyDialog'
import DatabaseTrash from './DatabaseTrash'
import DatabaseFileCell from './DatabaseFileCell'



type ViewType = 'table' | 'board' | 'calendar' | 'list' | 'gallery'
const VIEW_TYPES: ViewType[] = ['table', 'board', 'calendar', 'list', 'gallery']
type ViewConfig = { filter?: string; sort?: string; groupBy?: string; dateProperty?: string; filterTree?: FilterGroup; columnOrder?: string[]; hiddenColumns?: string[]; columnWidths?: Record<string, number> }

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

export default function DatabaseView({ databaseId, viewId: initialViewId, onBack, onOpenRecord, onLinkedPageCreated, onPendingEditChange }: { databaseId: string; viewId?: string; onBack: () => void; onOpenRecord?: (pageId: string) => void; onLinkedPageCreated?: () => void; onPendingEditChange?: (pending: boolean) => void }) {
  const [db, setDb] = useState<Database | null>(null)
  const [records, setRecords] = useState<DBRecord[]>([])
  const [viewId, setViewId] = useState<string>(initialViewId || '')
  const [newViewType, setNewViewType] = useState<ViewType>('table')
  const [propertyDialogOpen, setPropertyDialogOpen] = useState(false)
  const [trashOpen, setTrashOpen] = useState(false)
  const [filterText, setFilterText] = useState('')
  const [filterTree, setFilterTree] = useState<FilterGroup>(() => newFilterGroup())
  const [sortProp, setSortProp] = useState('')
  const [groupBy, setGroupBy] = useState('')
  const [columnOrder,setColumnOrder] = useState<string[]>([])
  const [hiddenColumns,setHiddenColumns] = useState<string[]>([])
  const [columnWidths,setColumnWidths] = useState<Record<string,number>>({})
  const [selectedRecords,setSelectedRecords] = useState<string[]>([])
  const [dateProperty, setDateProperty] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [failedEdit, setFailedEdit] = useState<{recordId: string; patch: {title?: string; values?: Record<string, any>}} | null>(null)
  useEffect(() => { onPendingEditChange?.(!!failedEdit) }, [failedEdit, onPendingEditChange])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const d = await api(`/api/databases?id=${databaseId}`) as Database
      setDb(d)
      const vs = d.views || []
      setViewId(old => vs.some(v => v.id === old) ? old : (vs.find(v => v.id === initialViewId)?.id || vs[0]?.id || ''))
      const recs = await api(`/api/db/records?databaseId=${databaseId}`) as DBRecord[]
      setRecords(recs || [])
      setFailedEdit(null)
      setError('')
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
    setFilterTree(cfg.filterTree?.kind === 'group' && Array.isArray(cfg.filterTree.children) ? cfg.filterTree : newFilterGroup())
    setSortProp(cfg.sort || '')
    setGroupBy(cfg.groupBy || '')
    setColumnOrder(Array.isArray(cfg.columnOrder)?cfg.columnOrder:[])
    setHiddenColumns(Array.isArray(cfg.hiddenColumns)?cfg.hiddenColumns:[])
    setColumnWidths(cfg.columnWidths && typeof cfg.columnWidths==='object'?cfg.columnWidths:{})
    setDateProperty(cfg.dateProperty || '')
  }, [view?.id, view?.config])

  const saveViewSettings = async () => {
    if (!view) return
    try {
      const updated = await api('/api/db/views', { method: 'PUT', body: JSON.stringify({ ...view, config: JSON.stringify({ filter: filterText, filterTree, sort: sortProp, groupBy, dateProperty, columnOrder, hiddenColumns, columnWidths }) }) }) as DBView
      setDb(current => current && ({ ...current, views: current.views.map(v => v.id === updated.id ? updated : v) }))
      setError('')
    } catch (e) { setError((e as Error).message) }
  }
  const visibleRecords = records.filter(rec => matchesFilters(rec, props, filterTree) && (!filterText || (rec.title + ' ' + Object.values(rec.values || {}).join(' ')).toLowerCase().includes(filterText.toLowerCase()))).sort((a,b) => {
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
    if (failedEdit) { setError('Resolve the outstanding edit using Retry or Reload latest first.'); return }
    try { await updateRecord(rec, patch) }
    catch (e) {
      setFailedEdit({recordId: rec.id, patch})
      setError('Save failed: ' + (e as Error).message + '. Your draft is retained until Retry or Reload latest.')
    }
  }
  const retryFailedEdit = async () => {
    if (!failedEdit) return
    try {
      const latest = await api('/api/db/records?databaseId=' + encodeURIComponent(databaseId)) as DBRecord[]
      const remote = latest.find(r => r.id === failedEdit.recordId)
      if (!remote) { setError('Cannot retry: the record no longer exists.'); return }
      const body = { id: remote.id, version: remote.version, title: failedEdit.patch.title ?? remote.title, values: failedEdit.patch.values ?? {} }
      const saved = await api('/api/db/records', {method:'PUT', body:JSON.stringify(body)}) as DBRecord
      setRecords(rows => rows.map(r => r.id === saved.id ? saved : r))
      setFailedEdit(null)
      setError('')
    } catch (e) { setError('Retry failed; your draft remains available: ' + (e as Error).message) }
  }
  const guardedBack = () => {
    if (failedEdit) { setError('Unsaved database edit: resolve it with Retry or Reload latest before leaving.'); return }
    onBack()
  }
  const guardedOpen = (pageId: string) => {
    if (failedEdit) { setError('Unsaved database edit: resolve it before opening another page.'); return }
    onOpenRecord?.(pageId)
  }
  const addRecord = async () => {
    const rec = await api('/api/db/records', { method: 'POST', body: JSON.stringify({ databaseId, title: 'Untitled' }) }) as DBRecord
    setRecords(rs => [...rs, rec])
  }

  const deleteRecord = async (id:string) => {
    if(failedEdit){setError('Resolve the unsaved edit before deleting a record');return}
    if(!confirm('Move this record to Trash? Its data can be restored.'))return
    try {
      await api('/api/db/records?id='+encodeURIComponent(id),{method:'DELETE'})
      setRecords(rows=>rows.filter(r=>r.id!==id))
      setError('')
    }catch(e){setError('Could not delete record: '+(e as Error).message)}
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
      {trashOpen && <DatabaseTrash databaseId={databaseId} onClose={()=>setTrashOpen(false)} onRestored={()=>{void load()}} />}
      {propertyDialogOpen && <DatabasePropertyDialog databaseId={databaseId} properties={props} onClose={() => setPropertyDialogOpen(false)} onCreated={() => {setPropertyDialogOpen(false);void load()}} />}
      {error && <div role="alert" style={{ marginBottom: 8, color: '#dc2626' }}>{error} {failedEdit && <button onClick={() => void retryFailedEdit()}>Retry my changes</button>} <button onClick={() => { void load() }}>Reload latest</button></div>}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
        <button className="btn" onClick={guardedBack}>← Back</button>
        <h2 style={{ margin: 0 }}>{db.icon} {db.title}</h2>
        <button className="btn" onClick={renameDb}>Rename</button>
        <button className="btn" onClick={() => {if(failedEdit) {setError("Resolve unsaved edit before changing schema");return};setPropertyDialogOpen(true)}}>+ Property</button>
        <button className="btn primary" onClick={addRecord}>+ New</button>
        <button className="btn" aria-label="Open database trash" onClick={()=>setTrashOpen(true)}>Trash</button>
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
        <button className="btn" aria-label="Rename database view" disabled={!view} onClick={async () => {
          if (!view) return
          const name=prompt('New view name',view.name)
          if (!name?.trim() || name.trim()===view.name) return
          try {
            const updated=await api('/api/db/views',{method:'PUT',body:JSON.stringify({...view,name:name.trim()})}) as DBView
            setDb(current=>current&&({...current,views:current.views.map(v=>v.id===updated.id?updated:v)}))
          } catch(e){setError((e as Error).message)}
        }}>Rename view</button>
        <button className="btn" aria-label="Duplicate database view" disabled={!view} onClick={async () => {
          if (!view) return
          try {
            const copy=await api('/api/db/views',{method:'POST',body:JSON.stringify({databaseId,name:view.name+' copy',type:view.type,config:view.config})}) as DBView
            setDb(current=>current&&({...current,views:[...current.views,copy]}))
            setViewId(copy.id)
          } catch(e){setError((e as Error).message)}
        }}>Duplicate</button>
        <button className="btn" aria-label="Delete database view" disabled={!view || db.views.length<=1} onClick={async () => {
          if (!view || !confirm('Delete this view? Records will be kept.')) return
          try {
            await api('/api/db/views?id='+encodeURIComponent(view.id),{method:'DELETE'})
            const remaining=db.views.filter(v=>v.id!==view.id)
            setDb(current=>current&&({...current,views:remaining}))
            setViewId(remaining[0]?.id||'')
          } catch(e){setError((e as Error).message)}
        }}>Delete view</button>
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
      <details style={{ marginBottom: 10 }}><summary>Advanced filters (AND / OR)</summary><DatabaseFilterBuilder group={filterTree} properties={props} onChange={setFilterTree} /></details>
      {view?.type === 'table' && <TableView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} deleteProperty={deleteProperty} deleteRecord={deleteRecord} onOpenRecord={guardedOpen} columnOrder={columnOrder} setColumnOrder={setColumnOrder} hiddenColumns={hiddenColumns} setHiddenColumns={setHiddenColumns} columnWidths={columnWidths} setColumnWidths={setColumnWidths} selectedRecords={selectedRecords} setSelectedRecords={setSelectedRecords} />}
      {view?.type === 'board' && <BoardView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} groupBy={groupBy} onOpenRecord={guardedOpen} />}
      {view?.type === 'calendar' && <CalendarView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} dateProperty={dateProperty} onOpenRecord={guardedOpen} />}
      {view?.type === 'list' && <ListView records={visibleRecords} onOpenRecord={guardedOpen} />}
      {view?.type === 'gallery' && <GalleryView records={visibleRecords} onOpenRecord={guardedOpen} />}
      {!['table', 'board', 'calendar', 'list', 'gallery'].includes(view?.type || '') && <div>Unknown view</div>}
    </div>
  )
}


type RecordUpdate = (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => Promise<void>
type OpenRecord = (pageId: string) => void
function TableView({ records, props, updateRecord, deleteProperty, deleteRecord, onOpenRecord, columnOrder, setColumnOrder, hiddenColumns, setHiddenColumns, columnWidths, setColumnWidths, selectedRecords, setSelectedRecords }: {
  records: DBRecord[]; props: DBProperty[]; updateRecord: RecordUpdate; deleteProperty: (id: string) => Promise<void>;
  deleteRecord: (id: string) => Promise<void>; onOpenRecord?: OpenRecord;
  columnOrder: string[]; setColumnOrder: (next: string[]) => void;
  hiddenColumns: string[]; setHiddenColumns: (next: string[]) => void;
  columnWidths: Record<string,number>; setColumnWidths: (next: Record<string,number>) => void;
  selectedRecords: string[]; setSelectedRecords: (next: string[]) => void;
}) {
  const visibleProps=props.filter(p=>p.visible && !hiddenColumns.includes(p.id)).sort((a,b)=>{
    const ai=columnOrder.indexOf(a.id),bi=columnOrder.indexOf(b.id)
    return (ai<0?props.indexOf(a)+columnOrder.length:ai)-(bi<0?props.indexOf(b)+columnOrder.length:bi)
  })
  const reorder=(id:string,offset:number)=>{
    const ids=visibleProps.map(p=>p.id),index=ids.indexOf(id),target=index+offset
    if(target<0 || target>=ids.length)return
    const swapped=[...ids];[swapped[index],swapped[target]]=[swapped[target],swapped[index]]
    setColumnOrder([...swapped,...columnOrder.filter(id=>!swapped.includes(id))])
  }
  const toggle=(id:string)=>setHiddenColumns(hiddenColumns.includes(id)?hiddenColumns.filter(x=>x!==id):[...hiddenColumns,id])
  return <div style={{overflowX:'auto'}}>
    <details style={{marginBottom:10}}>
      <summary>Table columns and layout</summary>
      <div role="group" aria-label="Column visibility" style={{display:'flex',gap:10,flexWrap:'wrap',padding:8}}>
        {props.map(p=><label key={p.id}><input type="checkbox" checked={!hiddenColumns.includes(p.id)} disabled={p.type==='title'} onChange={()=>toggle(p.id)} />{p.name}</label>)}
      </div>
      <small>Column order and widths are stored with this view when you click Save view settings.</small>
    </details>
    {selectedRecords.length>0&&<div style={{marginBottom:8}}>
      <strong>{selectedRecords.length} selected</strong>
      <button aria-label="Clear selected rows" onClick={()=>setSelectedRecords([])}>Clear selection</button>
      <button aria-label="Delete selected rows" onClick={async()=>{
        if(!confirm('Move '+selectedRecords.length+' records to Trash?'))return
        for(const id of selectedRecords)await deleteRecord(id)
        setSelectedRecords([])
      }}>Move selected to Trash</button>
    </div>}
    <table style={{borderCollapse:'collapse',width:'100%'}}>
      <thead><tr>
        <th><input aria-label="Select all visible records" type="checkbox" checked={records.length>0 && records.every(r=>selectedRecords.includes(r.id))} onChange={e=>setSelectedRecords(e.target.checked?[...new Set([...selectedRecords,...records.map(r=>r.id)])]:selectedRecords.filter(id=>!records.some(r=>r.id===id)))} /></th>
        <th>Page</th>{visibleProps.map((p,index)=><th key={p.id} style={{border:'1px solid var(--border)',padding:5,width:columnWidths[p.id]||160,minWidth:100}}>
          <div style={{display:'flex',alignItems:'center',gap:4}}><span>{p.name}</span>
            <button aria-label={'Move '+p.name+' left'} disabled={index===0} onClick={()=>reorder(p.id,-1)}>‹</button>
            <button aria-label={'Move '+p.name+' right'} disabled={index===visibleProps.length-1} onClick={()=>reorder(p.id,1)}>›</button>
            {p.type!=='title'&&<button aria-label={'Delete '+p.name} onClick={()=>{if(confirm('Move property '+p.name+' to Trash?'))void deleteProperty(p.id)}}>×</button>}
          </div>
          <input type="number" aria-label={'Width '+p.name} title={'Column width: '+p.name} min={100} max={700} step={10} value={columnWidths[p.id]||160} onChange={e=>{
            const width=Math.min(700,Math.max(100,Number(e.target.value)||160))
            setColumnWidths({...columnWidths,[p.id]:width})
          }} style={{width:64}} />
        </th>)}</tr></thead>
      <tbody>{records.map(rec=><tr key={rec.id} data-testid="database-record-row" data-record-id={rec.id}>
        <td><input aria-label={'Select record '+rec.title} type="checkbox" checked={selectedRecords.includes(rec.id)} onChange={e=>setSelectedRecords(e.target.checked?[...selectedRecords,rec.id]:selectedRecords.filter(id=>id!==rec.id))}/></td>
        <td><button onClick={()=>rec.pageId&&onOpenRecord?.(rec.pageId)}>Open page</button><button aria-label={'Delete record '+rec.title} onClick={()=>{void deleteRecord(rec.id)}}>Delete</button></td>
        {visibleProps.map(p=><td key={p.id} style={{border:'1px solid var(--border)',padding:4,width:columnWidths[p.id]||160}}>
          <Cell prop={p} recordId={rec.id} value={p.type==='title'?rec.title:rec.values?.[p.id]} onChange={value=>{
            void updateRecord(rec,p.type==='title'?{title:String(value)}:{values:{[p.id]:value}})
          }} />
        </td>)}
      </tr>)}</tbody>
    </table>
  </div>
}
function Cell({ prop, recordId, value, onChange }: { prop: DBProperty; recordId:string; value: any; onChange: (value: any) => void }) {
  if(prop.type==='file')return <DatabaseFileCell prop={prop} recordId={recordId} value={value} onChange={onChange} />
  if (['formula','rollup','created_time','last_edited_time'].includes(prop.type)) return <span aria-label={prop.name}>{value == null ? '—' : String(value)}</span>
  if (prop.type === 'relation') return <RelationCell prop={prop} value={value} onChange={onChange} />
  if (prop.type === 'person') return <PersonCell prop={prop} value={value} onChange={onChange} />
  if (prop.type === 'checkbox') return <input type="checkbox" checked={!!value} onChange={e => onChange(e.target.checked)} />
  if (prop.type === 'number') return <input aria-label={prop.name} type="number" key={String(value)} defaultValue={value ?? ''} onBlur={e => onChange(e.target.value === '' ? null : Number(e.target.value))} />
  if (prop.type === 'date') return <input aria-label={prop.name} type="date" value={String(value ?? '').slice(0, 10)} onChange={e => onChange(e.target.value || null)} />
  if (prop.type === 'select') {
    let options: string[] = []
    try { options = JSON.parse(prop.config || '{}').options || [] } catch { /* invalid legacy config */ }
    return <select aria-label={prop.name} value={value ?? ''} onChange={e => onChange(e.target.value || null)}>
      <option value="">—</option>{options.map(o => <option key={o} value={o}>{o}</option>)}
    </select>
  }
  if (prop.type === 'multi_select') return <input aria-label={prop.name} key={JSON.stringify(value)} defaultValue={Array.isArray(value) ? value.join(',') : ''} onBlur={e => onChange(e.target.value.split(',').map(x => x.trim()).filter(Boolean))} />
  return <input aria-label={prop.name} key={String(value)} defaultValue={value ?? ''} onBlur={e => { if (String(value ?? '') !== e.target.value) onChange(e.target.value) }} style={{ width: '100%', border: 'none', background: 'transparent' }} />
}

function RelationCell({ prop, value, onChange }: {prop:DBProperty;value:unknown;onChange:(v:unknown)=>void}) {
  const [records, setRecords] = useState<{id:string;title:string}[]>([])
  const [open, setOpen] = useState(false)
  const [error, setError] = useState('')
  let target=''
  try {target = JSON.parse(prop.config).targetDatabaseId || ''} catch { /* bad legacy config */ }
  useEffect(() => {
    let live=true
    if (!target) return
    fetch('/api/db/records?databaseId='+encodeURIComponent(target)).then(res=>{
      if (!res.ok) throw new Error(String(res.status))
      return res.json()
    }).then((result:{id:string;title:string}[])=>{if(live)setRecords(result)})
      .catch(e=>{if(live)setError(String(e))})
    return ()=>{live=false}
  },[target])
  const selection=Array.isArray(value)?value as string[]:[]
  return <div>
    <button type="button" aria-label={'Edit '+prop.name} onClick={()=>setOpen(v=>!v)}>{selection.length ? selection.map(id=>records.find(r=>r.id===id)?.title || id).join(', ') : 'Add relations'}</button>
    {open&&<div role="group" aria-label={'Relation '+prop.name} style={{maxHeight:160,overflowY:'auto',background:'var(--card)',border:'1px solid var(--border)',padding:8}}>
      {records.map(rec=><label key={rec.id} style={{display:'block'}}><input type="checkbox" checked={selection.includes(rec.id)} onChange={e=>onChange(e.target.checked ? [...selection,rec.id] : selection.filter(id=>id!==rec.id))} />{rec.title}</label>)}
      {!records.length&&<span>No records available</span>}
      {error&&<span role="alert">{error}</span>}
    </div>}
  </div>
}
function PersonCell({prop,value,onChange}:{prop:DBProperty;value:unknown;onChange:(v:unknown)=>void}) {
  const [members,setMembers]=useState<{id:string;name:string}[]>([])
  useEffect(()=>{let live=true
    fetch('/api/members').then(r=>{if(!r.ok)throw new Error(String(r.status));return r.json()})
      .then((result:unknown)=>{if(live && Array.isArray(result))setMembers(result)})
      .catch(()=>{})
    return ()=>{live=false}
  },[])
  return <select aria-label={prop.name} value={String(value??'')} onChange={e=>onChange(e.target.value||null)}>
    <option value="">Unassigned</option>{members.map(m=><option key={m.id} value={m.id}>{m.name}</option>)}
  </select>
}
function BoardView({ records, props, updateRecord, groupBy, onOpenRecord }: {
  records: DBRecord[]; props: DBProperty[]; updateRecord: RecordUpdate; groupBy: string; onOpenRecord?: OpenRecord
}) {
  const [dragId, setDragId] = useState<string | null>(null)
  const groupProp = props.find(p => p.id === groupBy && p.type === 'select') || props.find(p => p.type === 'select')
  if (!groupProp) return <p>Add a Select property to use Board view.</p>
  const groups: Record<string, DBRecord[]> = { '(empty)': [] }
  let options: string[] = []
  try { options = JSON.parse(groupProp.config || '{}').options || [] } catch { /* legacy config */ }
  for (const option of options) groups[option] = []
  for (const rec of records) {
    const key = String(rec.values?.[groupProp.id] ?? '(empty)')
    if (!groups[key]) groups[key] = []
    groups[key].push(rec)
  }
  const move = async (key: string) => {
    const record = records.find(rec => rec.id === dragId)
    setDragId(null)
    if (record && String(record.values?.[groupProp.id] ?? '(empty)') !== key) {
      await updateRecord(record, { values: { [groupProp.id]: key === '(empty)' ? null : key } })
    }
  }
  return <div style={{ display: 'flex', gap: 12, overflowX: 'auto' }}>
    {Object.entries(groups).map(([group, items]) => <div key={group} data-testid="board-group" data-group={group}
      onDragOver={event => event.preventDefault()} onDrop={() => { void move(group) }}
      style={{ minWidth: 220, padding: 8, background: 'var(--card)', border: '1px solid var(--border)', borderRadius: 8 }}>
      <strong>{group} ({items.length})</strong>
      {items.map(item => <div data-testid="board-card" key={item.id} draggable
        onDragStart={() => setDragId(item.id)} onDragEnd={() => setDragId(null)}
        style={{ padding: 8, marginTop: 8, cursor: 'grab', border: '1px solid var(--border)', borderRadius: 6 }}>
        {item.title} <button onClick={() => item.pageId && onOpenRecord?.(item.pageId)}>Open</button>
      </div>)}
    </div>)}
  </div>
}
function calendarDay(year: number, month: number, day: number): string {
  return [year, String(month + 1).padStart(2, '0'), String(day).padStart(2, '0')].join('-')
}
function CalendarView({ records, props, updateRecord, dateProperty, onOpenRecord }: {
  records: DBRecord[]; props: DBProperty[]; updateRecord: RecordUpdate; dateProperty: string; onOpenRecord?: OpenRecord
}) {
  const [cursor, setCursor] = useState(() => new Date())
  const [draggedRecord, setDraggedRecord] = useState<string | null>(null)
  const dateProp = props.find(p => p.id === dateProperty && p.type === 'date') || props.find(p => p.type === 'date')
  const year = cursor.getFullYear()
  const month = cursor.getMonth()
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const offset = new Date(year, month, 1).getDay()
  const cells: (number | null)[] = [...Array<null>(offset).fill(null), ...Array.from({ length: daysInMonth }, (_, index) => index + 1)]
  if (!dateProp) return <p>Add a Date property to use Calendar view.</p>
  return <div>
    <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 8 }}>
      <button onClick={() => setCursor(new Date(year, month - 1, 1))}>Previous</button>
      <strong>{cursor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</strong>
      <button onClick={() => setCursor(new Date(year, month + 1, 1))}>Next</button>
    </div>
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, minmax(90px, 1fr))', overflowX: 'auto', gap: 4 }}>
      {['Sun','Mon','Tue','Wed','Thu','Fri','Sat'].map(name => <strong key={name}>{name}</strong>)}
      {cells.map((day,index) => <div data-testid="calendar-cell" data-date={day ? calendarDay(year, month, day) : ''}
        key={index} onDragOver={event=>{if(day)event.preventDefault()}} onDrop={()=>{
          if(!day || !draggedRecord)return
          const record=records.find(r=>r.id===draggedRecord)
          setDraggedRecord(null)
          if(record)void updateRecord(record,{values:{[dateProp.id]:calendarDay(year,month,day)}})
        }} style={{ border: '1px solid var(--border)', background: 'var(--card)', padding: 4, minHeight: 80 }}>
        <small>{day || ''}</small>
        {day && records.filter(rec => String(rec.values?.[dateProp.id] ?? '').slice(0,10) === calendarDay(year,month,day)).map(rec => <div key={rec.id} data-testid="calendar-record" draggable onDragStart={()=>setDraggedRecord(rec.id)} onDragEnd={()=>setDraggedRecord(null)}>
          <button onClick={() => rec.pageId && onOpenRecord?.(rec.pageId)}>{rec.title}</button>
          <input type="date" aria-label={'Date for ' + rec.title} value={String(rec.values?.[dateProp.id] ?? '').slice(0,10)}
            onChange={e => { void updateRecord(rec, { values: { [dateProp.id]: e.target.value || null } }) }} />
        </div>)}
      </div>)}
    </div>
    <section style={{marginTop:12}}>
      <strong>Unscheduled records</strong>
      <div style={{display:'flex',gap:12,flexWrap:'wrap',marginTop:6}}>
        {records.filter(rec=>!rec.values?.[dateProp.id]).map(rec=><label key={rec.id}>
          {rec.title} <input type="date" aria-label={'Schedule '+rec.title} onChange={e=>{
            if(e.target.value)void updateRecord(rec,{values:{[dateProp.id]:e.target.value}})
          }}/>
        </label>)}
      </div>
    </section>
  </div>
}
function ListView({ records, onOpenRecord }: { records: DBRecord[]; onOpenRecord?: OpenRecord }) {
  return <div style={{ display: 'grid', gap: 8 }}>{records.map(rec => <div data-testid="list-record" key={rec.id} style={{ borderBottom: '1px solid var(--border)', padding: 8 }}>
    <strong>{rec.title}</strong> <button onClick={() => rec.pageId && onOpenRecord?.(rec.pageId)}>Open page</button>
  </div>)}</div>
}
function GalleryView({ records, onOpenRecord }: { records: DBRecord[]; onOpenRecord?: OpenRecord }) {
  return <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 12 }}>
    {records.map(rec => <div data-testid="gallery-record" key={rec.id} style={{ border: '1px solid var(--border)', background: 'var(--card)', padding: 12, borderRadius: 8 }}>
      <strong>{rec.title}</strong><div>{Object.values(rec.values || {}).slice(0,3).map((value,index) => <span key={index}>{String(value ?? '')} </span>)}</div>
      <button onClick={() => rec.pageId && onOpenRecord?.(rec.pageId)}>Open page</button>
    </div>)}
  </div>
}
