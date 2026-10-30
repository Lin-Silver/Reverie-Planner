import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { matchesFilters, newFilterGroup, type FilterGroup } from './databaseFilters'
import DatabaseFilterBuilder from './DatabaseFilterBuilder'
import DatabasePropertyDialog from './DatabasePropertyDialog'
import DatabaseTrash from './DatabaseTrash'
import DatabaseFileCell from './DatabaseFileCell'
import DatabaseBoard from './DatabaseBoard'
import DatabaseBulkPaste, { type BulkRecordEdit } from './DatabaseBulkPaste'
import { selectedCellsTSV } from './databaseClipboard'
import { sortDatabaseRecords, type DatabaseSortRule } from './databaseSorting'



type ViewType = 'table' | 'board' | 'calendar' | 'list' | 'gallery'
const VIEW_TYPES: ViewType[] = ['table', 'board', 'calendar', 'list', 'gallery']
type ViewConfig = { filter?: string; sort?: string; sortRules?: DatabaseSortRule[]; groupBy?: string; dateProperty?: string; filterTree?: FilterGroup; columnOrder?: string[]; hiddenColumns?: string[]; columnWidths?: Record<string, number>; galleryProperties?: string[]; galleryCoverProperty?: string }

export type DBProperty = { id: string; databaseId: string; name: string; type: string; config: string; position: number; visible: boolean; version: number }
type DBView = { id: string; databaseId: string; name: string; type: string; config: string; position: number; version: number }
type Database = { id: string; pageId: string; title: string; icon: string; properties: DBProperty[]; views: DBView[]; version: number }
export type DBRecord = { id: string; databaseId: string; title: string; pageId: string; values: Record<string, any>; version: number; createdAt: string; updatedAt: string }

async function api(path: string, opts?: RequestInit) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' }, ...opts })
  if (!res.ok) {
    const detail = await res.json().catch(() => null) as { error?: string } | null
    throw new Error(`${res.status} ${detail?.error || res.statusText}`)
  }
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
  const [sortDirection, setSortDirection] = useState<'asc'|'desc'>('asc')
  const [secondarySortProp, setSecondarySortProp] = useState('')
  const [secondarySortDirection, setSecondarySortDirection] = useState<'asc'|'desc'>('asc')
  const [groupBy, setGroupBy] = useState('')
  const [columnOrder,setColumnOrder] = useState<string[]>([])
  const [hiddenColumns,setHiddenColumns] = useState<string[]>([])
  const [columnWidths,setColumnWidths] = useState<Record<string,number>>({})
  const [selectedRecords,setSelectedRecords] = useState<string[]>([])
  const [bulkDraft,setBulkDraft] = useState('')
  const [bulkStartProperty,setBulkStartProperty] = useState('')
  const [dateProperty, setDateProperty] = useState('')
  const [galleryProperties, setGalleryProperties] = useState<string[] | null>(null)
  const [galleryCoverProperty, setGalleryCoverProperty] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [failedEdit, setFailedEdit] = useState<{recordId: string; patch: {title?: string; values?: Record<string, any>}} | null>(null)
  const [failedViewConfig, setFailedViewConfig] = useState<{ id: string; config: string } | null>(null)
  const [savingView, setSavingView] = useState(false)
  const [creatingRecord, setCreatingRecord] = useState(false)
  const pendingView = savingView || !!failedViewConfig
  const hasPendingEdit = !!failedEdit || !!bulkDraft || pendingView
  useEffect(() => { onPendingEditChange?.(hasPendingEdit) }, [hasPendingEdit, onPendingEditChange])

  const load = useCallback(async (keepSurface = false) => {
    // Recovery refreshes must keep their modal/category/focus mounted.
    if (!keepSurface) setLoading(true)
    try {
      const d = await api(`/api/databases?id=${databaseId}`) as Database
      setDb(d)
      const vs = d.views || []
      setViewId(old => vs.some(v => v.id === old) ? old : (vs.find(v => v.id === initialViewId)?.id || vs[0]?.id || ''))
      const recs = await api(`/api/db/records?databaseId=${databaseId}`) as DBRecord[]
      setRecords(recs || [])
      // A reload or database switch must not retain selection from old rows.
      setSelectedRecords([])
      setFailedEdit(null)
      setFailedViewConfig(null)
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
    const savedRules=Array.isArray(cfg.sortRules)?cfg.sortRules:[]
    setSortProp(savedRules[0]?.propertyId || cfg.sort || '')
    setSortDirection(savedRules[0]?.direction === 'desc' ? 'desc' : 'asc')
    setSecondarySortProp(savedRules[1]?.propertyId || '')
    setSecondarySortDirection(savedRules[1]?.direction === 'desc' ? 'desc' : 'asc')
    setGroupBy(cfg.groupBy || '')
    setColumnOrder(Array.isArray(cfg.columnOrder)?cfg.columnOrder:[])
    setHiddenColumns(Array.isArray(cfg.hiddenColumns)?cfg.hiddenColumns:[])
    setColumnWidths(cfg.columnWidths && typeof cfg.columnWidths==='object'?cfg.columnWidths:{})
    setDateProperty(cfg.dateProperty || '')
    setGalleryProperties(Array.isArray(cfg.galleryProperties) ? cfg.galleryProperties : null)
    setGalleryCoverProperty(cfg.galleryCoverProperty || '')
  }, [view?.id, view?.config])

  const sortRules: DatabaseSortRule[] = [
    ...(sortProp ? [{ propertyId: sortProp, direction: sortDirection }] : []),
    ...(secondarySortProp && secondarySortProp !== sortProp ? [{ propertyId: secondarySortProp, direction: secondarySortDirection }] : [])
  ]
  const saveViewSettings = async () => {
    if (!view || hasPendingEdit) return
    // Preserve extension settings written by other layouts rather than dropping
    // unknown keys when saving filters or column presentation.
    let previous: ViewConfig = {}
    try { previous = JSON.parse(view.config || '{}') as ViewConfig } catch { /* legacy config */ }
    const config = JSON.stringify({ ...previous, filter: filterText, filterTree, sort: sortProp, sortRules, groupBy, dateProperty, columnOrder, hiddenColumns, columnWidths, ...(galleryProperties === null ? {} : { galleryProperties }), galleryCoverProperty })
    setSavingView(true)
    try {
      const updated = await api('/api/db/views', { method: 'PUT', body: JSON.stringify({ ...view, config }) }) as DBView
      setDb(current => current && ({ ...current, views: current.views.map(v => v.id === updated.id ? updated : v) }))
      setError('')
    } catch (e) {
      setFailedViewConfig({ id: view.id, config })
      setError('View settings save failed: ' + (e as Error).message + '. Your settings are retained. Retry explicitly or reload latest to discard them.')
    } finally { setSavingView(false) }
  }
  const retryViewSettings = async () => {
    if (!failedViewConfig || savingView) return
    setSavingView(true)
    try {
      const latest = await api(`/api/databases?id=${encodeURIComponent(databaseId)}`) as Database
      const remote = latest.views.find(v => v.id === failedViewConfig.id)
      if (!remote) throw new Error('This view no longer exists. Reload latest to discard the draft.')
      const updated = await api('/api/db/views', { method: 'PUT', body: JSON.stringify({ ...remote, config: failedViewConfig.config }) }) as DBView
      setDb(current => current && ({ ...current, views: current.views.map(v => v.id === updated.id ? updated : v) }))
      setFailedViewConfig(null)
      setError('')
    } catch (e) { setError('View settings retry failed; draft retained: ' + (e as Error).message) }
    finally { setSavingView(false) }
  }
  const matchingRecords = records.filter(rec => matchesFilters(rec, props, filterTree) && (!filterText || (rec.title + ' ' + Object.values(rec.values || {}).join(' ')).toLowerCase().includes(filterText.toLowerCase())))
  const visibleRecords = sortDatabaseRecords(matchingRecords, props, sortRules)

  const updateRecord = async (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => {
    const body: any = { id: rec.id, title: patch.title ?? rec.title, values: patch.values ?? {}, version: rec.version }
    const updated = await api('/api/db/records', { method: 'PUT', body: JSON.stringify(body) }) as DBRecord
    setError('')
    setRecords(rs => rs.map(r => r.id === rec.id ? { ...r, ...updated, values: { ...r.values, ...(updated.values || {}) } } : r))
  }

  const safelyUpdateRecord = async (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => {
    if (hasPendingEdit) { setError('Resolve the outstanding edit or discard the bulk paste draft first.'); return }
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
    if (hasPendingEdit) { setError('Unsaved database edit: resolve it with Retry or Reload latest before leaving.'); return }
    onBack()
  }
  const guardedOpen = (pageId: string) => {
    if (hasPendingEdit) { setError('Unsaved database edit: resolve it before opening another page.'); return }
    onOpenRecord?.(pageId)
  }
  const addRecord = async () => {
    if (hasPendingEdit || creatingRecord) { setError('Resolve pending edits before adding records.'); return }
    setCreatingRecord(true)
    try {
      const rec = await api('/api/db/records', { method: 'POST', body: JSON.stringify({ databaseId, title: 'Untitled' }) }) as DBRecord
      setRecords(rs => [...rs, rec])
      setError('')
    } catch (e) { setError('Could not create record: ' + (e as Error).message) }
    finally { setCreatingRecord(false) }
  }

  const deleteRecord = async (id:string, skipConfirmation = false): Promise<boolean> => {
    if(hasPendingEdit){setError('Resolve the unsaved edit or bulk draft before deleting a record');return false}
    const rec = records.find(row => row.id === id && row.databaseId === databaseId)
    if (!rec) { setError('Record is no longer in this database; reload latest before deleting.'); return false }
    if(!skipConfirmation && !confirm('Move this record to Trash? Its data can be restored.'))return false
    try {
      // Prevent a stale browser tab from deleting edits made elsewhere.
      await api('/api/db/records?id='+encodeURIComponent(id)+'&version='+encodeURIComponent(String(rec.version)),{method:'DELETE'})
      setRecords(rows=>rows.filter(r=>r.id!==id))
      setSelectedRecords(selectedRecords.filter(selectedId=>selectedId!==id))
      setError('')
      return true
    }catch(e){setError('Could not delete record: '+(e as Error).message);return false}
  }
  const deleteProperty = async (pid: string) => {
    if (hasPendingEdit) { setError('Resolve the unsaved edit or bulk draft before deleting a property.'); return }
    const property = props.find(p => p.id === pid)
    if (!property) { setError('Property is no longer available; reload latest before deleting.'); return }
    try {
      // Reject stale deletions after a remote property rename or option edit.
      await api(`/api/db/properties?id=${encodeURIComponent(pid)}&version=${encodeURIComponent(String(property.version))}`, { method: 'DELETE' })
      await load()
    } catch (e) {
      setError('Could not delete property: ' + (e as Error).message + '. Reload latest before retrying.')
    }
  }

  const applyBulkEdit = async (edits: BulkRecordEdit[]): Promise<boolean> => {
    if (failedEdit) { setError('Resolve the outstanding cell edit before applying a bulk paste.'); return false }
    try {
      const updated = await api('/api/db/records/bulk', { method: 'POST', body: JSON.stringify({ databaseId, edits }) }) as DBRecord[]
      const byId = new Map(updated.map(record => [record.id, record]))
      setRecords(current => current.map(record => byId.get(record.id) || record))
      setError('')
      return true
    } catch (e) {
      setError('Bulk paste failed: ' + (e as Error).message + '. No batch changes were saved; draft and selection retained. Refresh versions before retrying a conflict.')
      return false
    }
  }
  const refreshBulkVersions = async () => {
    if (failedEdit) { setError('Resolve the outstanding cell edit before refreshing.'); return }
    try {
      const latest = await api('/api/db/records?databaseId=' + encodeURIComponent(databaseId)) as DBRecord[]
      setRecords(latest)
      setError('')
    } catch (e) { setError('Could not refresh record versions; draft retained: ' + (e as Error).message) }
  }

  const renameDb = async () => {
    if (!db) return
    if (hasPendingEdit) { setError('Resolve pending edits before renaming.'); return }
    const title = prompt('Database name?', db.title)
    if (!title?.trim() || title.trim() === db.title) return
    try {
      const updated = await api('/api/databases', { method: 'PUT', body: JSON.stringify({ id: db.id, title: title.trim(), icon: db.icon, version: db.version }) }) as Database
      setDb(updated)
      setError('')
    } catch (e) { setError('Could not rename database: ' + (e as Error).message) }
  }

  if (loading) return <div style={{ padding: 24 }}>Loading database…</div>
  // Save errors remain visible without hiding the editor or unsaved input.
  if (!db) return <div style={{ padding: 24 }}><p role="alert">{error || 'Database not found'}</p><button onClick={() => void load()}>Retry loading database</button><button onClick={onBack}>Back</button></div>

  return (
    <div className="database-workspace">
      {trashOpen && <DatabaseTrash databaseId={databaseId} onClose={()=>setTrashOpen(false)} onRestored={()=>{void load(true)}} />}
      {propertyDialogOpen && <DatabasePropertyDialog databaseId={databaseId} properties={props} onClose={() => setPropertyDialogOpen(false)} onCreated={() => {setPropertyDialogOpen(false);void load()}} />}
      {error && <div role="alert" style={{ marginBottom: 8, color: '#dc2626' }}>{error} {failedViewConfig && <button disabled={savingView} onClick={() => void retryViewSettings()}>Retry my view settings</button>} {failedEdit && <button onClick={() => void retryFailedEdit()}>Retry my changes</button>} <button disabled={!!bulkDraft || savingView} onClick={() => { void load() }}>Reload latest</button></div>}
      <div className="database-toolbar" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, marginBottom: 12 }}>
        <button className="btn" onClick={guardedBack}>← Back</button>
        <h2 style={{ margin: 0 }}>{db.icon} {db.title}</h2>
        <button className="btn" onClick={renameDb}>Rename</button>
        <button className="btn" onClick={() => {if(hasPendingEdit) {setError("Resolve unsaved edit or bulk draft before changing schema");return};setPropertyDialogOpen(true)}}>+ Property</button>
        <button className="btn primary" disabled={hasPendingEdit || creatingRecord} onClick={() => void addRecord()}>+ New</button>
        <button className="btn" aria-label="Open database trash" disabled={hasPendingEdit} onClick={()=>setTrashOpen(true)}>Trash</button>
        <div style={{ flex: 1 }} />
        <select aria-label="Active database view" disabled={hasPendingEdit} value={viewId} onChange={e => { if (hasPendingEdit) { setError('Discard or resolve pending edits before switching views.'); return }; setViewId(e.target.value) }}>
          {db.views.map(v => <option key={v.id} value={v.id}>{v.name} ({v.type})</option>)}
        </select>
        <select aria-label="New view type" value={newViewType} onChange={e => setNewViewType(e.target.value as ViewType)}>{VIEW_TYPES.map(t => <option key={t} value={t}>{t}</option>)}</select>
        <button className="btn" disabled={hasPendingEdit} onClick={async () => {
          try {
            const v = await api('/api/db/views', { method: 'POST', body: JSON.stringify({ databaseId, name: newViewType[0].toUpperCase()+newViewType.slice(1), type: newViewType }) }) as DBView
            setDb(current => current && ({ ...current, views: [...current.views, v] }))
            setViewId(v.id)
            setError('')
          } catch (e) { setError((e as Error).message) }
        }}>+ View</button>
        <button className="btn" aria-label="Rename database view" disabled={!view || hasPendingEdit} onClick={async () => {
          if (!view) return
          const name=prompt('New view name',view.name)
          if (!name?.trim() || name.trim()===view.name) return
          try {
            const updated=await api('/api/db/views',{method:'PUT',body:JSON.stringify({...view,name:name.trim()})}) as DBView
            setDb(current=>current&&({...current,views:current.views.map(v=>v.id===updated.id?updated:v)}))
          } catch(e){setError((e as Error).message)}
        }}>Rename view</button>
        <button className="btn" aria-label="Duplicate database view" disabled={!view || hasPendingEdit} onClick={async () => {
          if (!view) return
          try {
            const copy=await api('/api/db/views',{method:'POST',body:JSON.stringify({databaseId,name:view.name+' copy',type:view.type,config:view.config})}) as DBView
            setDb(current=>current&&({...current,views:[...current.views,copy]}))
            setViewId(copy.id)
          } catch(e){setError((e as Error).message)}
        }}>Duplicate</button>
        <button className="btn" aria-label="Delete database view" disabled={!view || db.views.length<=1 || hasPendingEdit} onClick={async () => {
          if (!view || !confirm('Delete this view? Records will be kept.')) return
          try {
            await api('/api/db/views?id='+encodeURIComponent(view.id)+'&version='+encodeURIComponent(String(view.version)),{method:'DELETE'})
            const remaining=db.views.filter(v=>v.id!==view.id)
            setDb(current=>current&&({...current,views:remaining}))
            setViewId(remaining[0]?.id||'')
          } catch(e){setError((e as Error).message)}
        }}>Delete view</button>
        <button className="btn" disabled={hasPendingEdit} onClick={async () => {
          try {
            const pg = await api('/api/pages', { method: 'POST', body: JSON.stringify({ title: `${db.title} (linked)`, workspace: true, createdBy: 'You' }) }) as any
            await api('/api/db/linked-view', { method: 'POST', body: JSON.stringify({ databaseId, pageId: pg.id, name: 'Linked view' }) })
            onLinkedPageCreated?.()
            alert('Linked view page created: open it from the sidebar.')
          } catch (e) { setError((e as Error).message) }
        }}>+ Linked view page</button>
      </div>
      <fieldset disabled={pendingView} style={{border:0,padding:0,margin:0,minWidth:0}}><div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
        <input aria-label="Database filter" disabled={!!bulkDraft} placeholder="Filter records" value={filterText} onChange={e => setFilterText(e.target.value)} />
        <select aria-label="Database sort" disabled={!!bulkDraft} value={sortProp} onChange={e => {
          const next=e.target.value
          setSortProp(next)
          if (!next || next===secondarySortProp) setSecondarySortProp('')
        }}><option value="">No sort</option><option value="title">Title</option>{props.filter(p => p.type !== 'title').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
        <select aria-label="Sort direction" value={sortDirection} disabled={!sortProp || !!bulkDraft} onChange={e => setSortDirection(e.target.value as 'asc'|'desc')}><option value="asc">Ascending</option><option value="desc">Descending</option></select>
        <select aria-label="Secondary database sort" value={secondarySortProp} disabled={!sortProp || !!bulkDraft} onChange={e => setSecondarySortProp(e.target.value)}><option value="">No secondary sort</option><option value="title" disabled={sortProp==='title'}>Title</option>{props.filter(p => p.type !== 'title').map(p => <option key={p.id} value={p.id} disabled={sortProp===p.id}>{p.name}</option>)}</select>
        <select aria-label="Secondary sort direction" value={secondarySortDirection} disabled={!secondarySortProp || !!bulkDraft} onChange={e => setSecondarySortDirection(e.target.value as 'asc'|'desc')}><option value="asc">Ascending</option><option value="desc">Descending</option></select>
        {view?.type === 'board' && <select aria-label="Board group by" value={groupBy} onChange={e => setGroupBy(e.target.value)}><option value="">First select property</option>{props.filter(p => p.type === 'select').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>}
        {view?.type === 'calendar' && <select aria-label="Calendar date property" value={dateProperty} onChange={e => setDateProperty(e.target.value)}><option value="">First date property</option>{props.filter(p => p.type === 'date').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>}
        <button disabled={!!bulkDraft} onClick={() => void saveViewSettings()}>Save view settings</button>
      </div>
      <details style={{ marginBottom: 10 }}><summary>Advanced filters (AND / OR)</summary><fieldset disabled={!!bulkDraft} style={{border:0,padding:0}}><DatabaseFilterBuilder group={filterTree} properties={props} onChange={setFilterTree} /></fieldset></details>
      {view?.type === 'gallery' && <details style={{marginBottom:12}}><summary>Gallery presentation</summary>
        <label>Cover attachment <select aria-label="Gallery cover property" value={galleryCoverProperty} onChange={e=>setGalleryCoverProperty(e.target.value)}>
          <option value="">No cover</option>{props.filter(p=>p.type==='file').map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select></label>
        <div role="group" aria-label="Gallery visible properties">{props.filter(p=>p.type!=='title').map(p=><label key={p.id} style={{display:'inline-flex',gap:4,margin:6}}>
          <input type="checkbox" aria-label={'Show gallery '+p.name} checked={(galleryProperties ?? props.filter(field=>field.type!=='title').slice(0,3).map(field=>field.id)).includes(p.id)} onChange={e=>{
            const selected=galleryProperties ?? props.filter(field=>field.type!=='title').slice(0,3).map(field=>field.id)
            setGalleryProperties(e.target.checked ? [...selected,p.id] : selected.filter(id=>id!==p.id))
          }}/>{p.name}</label>)}</div>
        <small>Choose an image attachment property for card covers. Save view settings to keep this layout.</small>
      </details>}
      {view?.type === 'table' && <TableView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} deleteProperty={deleteProperty} deleteRecord={deleteRecord} onOpenRecord={guardedOpen} columnOrder={columnOrder} setColumnOrder={setColumnOrder} hiddenColumns={hiddenColumns} setHiddenColumns={setHiddenColumns} columnWidths={columnWidths} setColumnWidths={setColumnWidths} selectedRecords={selectedRecords} setSelectedRecords={setSelectedRecords} bulkDraft={bulkDraft} setBulkDraft={setBulkDraft} bulkStartProperty={bulkStartProperty} setBulkStartProperty={setBulkStartProperty} applyBulkEdit={applyBulkEdit} refreshBulkVersions={refreshBulkVersions} bulkDisabled={!!failedEdit} />}
      {view?.type === 'board' && <DatabaseBoard records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} groupBy={groupBy} onOpenRecord={guardedOpen} />}
      {view?.type === 'calendar' && <CalendarView records={visibleRecords} props={props} updateRecord={safelyUpdateRecord} dateProperty={dateProperty} onOpenRecord={guardedOpen} />}
      {view?.type === 'list' && <ListView records={visibleRecords} onOpenRecord={guardedOpen} deleteRecord={deleteRecord} />}
      {view?.type === 'gallery' && <GalleryView records={visibleRecords} props={props} propertyIds={galleryProperties} coverProperty={galleryCoverProperty} onOpenRecord={guardedOpen} deleteRecord={deleteRecord} />}
      {!['table', 'board', 'calendar', 'list', 'gallery'].includes(view?.type || '') && <div>Unknown view</div>}</fieldset>
    </div>
  )
}


type RecordUpdate = (rec: DBRecord, patch: { title?: string; values?: Record<string, any> }) => Promise<void>
type OpenRecord = (pageId: string) => void
// Native arrow keys remain available for caret movement. Alt+Arrow moves
// focus between data cells without committing an unchanged value.
function navigateTableCell(event: KeyboardEvent<HTMLTableCellElement>) {
  if (!event.altKey || !['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(event.key)) return
  const origin=event.currentTarget
  const row=origin.parentElement as HTMLTableRowElement | null
  if (!row || !row.parentElement) return
  const offset=event.key==='ArrowLeft'?-1:event.key==='ArrowRight'?1:0
  let target: HTMLTableCellElement | undefined
  if(offset)target=row.cells[origin.cellIndex+offset] as HTMLTableCellElement | undefined
  else {
    const adjacent=event.key==='ArrowUp'?row.previousElementSibling:row.nextElementSibling
    target=(adjacent as HTMLTableRowElement | null)?.cells[origin.cellIndex] as HTMLTableCellElement | undefined
  }
  if (!target?.hasAttribute('data-column-id')) return
  const control=target.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('input,select,textarea')
  if (!control || control.disabled) return
  event.preventDefault()
  control.focus()
}
function TableView({ records, props, updateRecord, deleteProperty, deleteRecord, onOpenRecord, columnOrder, setColumnOrder, hiddenColumns, setHiddenColumns, columnWidths, setColumnWidths, selectedRecords, setSelectedRecords, bulkDraft, setBulkDraft, bulkStartProperty, setBulkStartProperty, applyBulkEdit, refreshBulkVersions, bulkDisabled }: {
  records: DBRecord[]; props: DBProperty[]; updateRecord: RecordUpdate; deleteProperty: (id: string) => Promise<void>;
  deleteRecord: (id: string, skipConfirmation?: boolean) => Promise<boolean>; onOpenRecord?: OpenRecord;
  columnOrder: string[]; setColumnOrder: (next: string[]) => void;
  hiddenColumns: string[]; setHiddenColumns: (next: string[]) => void;
  columnWidths: Record<string,number>; setColumnWidths: (next: Record<string,number>) => void;
  selectedRecords: string[]; setSelectedRecords: (next: string[]) => void;
  bulkDraft: string; setBulkDraft: (next: string) => void;
  bulkStartProperty: string; setBulkStartProperty: (next: string) => void;
  applyBulkEdit: (edits: BulkRecordEdit[]) => Promise<boolean>;
  refreshBulkVersions: () => Promise<void>;
  bulkDisabled: boolean;
}) {
  const [draftRowOrder, setDraftRowOrder] = useState<string[]>([])
  const [copyStartId, setCopyStartId] = useState('')
  const [copyEndId, setCopyEndId] = useState('')
  const [copyStatus, setCopyStatus] = useState('')
  const [copyError, setCopyError] = useState('')
  const [selectionAnchor, setSelectionAnchor] = useState('')
  const [cellAnchor, setCellAnchor] = useState<{ recordId: string; propertyId: string } | null>(null)
  // Track the previous keyboard-owned range so reversing Shift+Arrow can
  // deselect cells outside the new range without clearing unrelated row picks.
  const keyboardRange = useRef<{ anchorId: string; ids: string[] } | null>(null)
  // Shift-click selects a contiguous range in the current filtered/sorted table.
  // The anchor is an ID rather than a row index, so reordering cannot silently
  // select a different range; an invisible anchor falls back to a single row.
  const selectVisibleRow = (id: string, checked: boolean, shift: boolean) => {
    keyboardRange.current = null
    setCellAnchor(null)
    const anchorIndex = records.findIndex(record => record.id === selectionAnchor)
    const targetIndex = records.findIndex(record => record.id === id)
    if (shift && anchorIndex >= 0 && targetIndex >= 0) {
      const range = records.slice(Math.min(anchorIndex, targetIndex), Math.max(anchorIndex, targetIndex) + 1).map(record => record.id)
      setSelectedRecords(checked
        ? [...new Set([...selectedRecords, ...range])]
        : selectedRecords.filter(selectedId => !range.includes(selectedId)))
    } else {
      setSelectedRecords(checked ? [...new Set([...selectedRecords, id])] : selectedRecords.filter(selectedId => selectedId !== id))
      setSelectionAnchor(id)
    }
  }
  // Shift+Arrow on a focused row checkbox extends selection without editing a cell.
  // Keep the anchor tied to a record ID so sorting/filtering cannot retarget it.
  const extendVisibleRowWithKeyboard = (event: KeyboardEvent<HTMLInputElement>, id: string) => {
    if (!event.shiftKey || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) return
    setCellAnchor(null)
    event.preventDefault()
    const index = records.findIndex(record => record.id === id)
    const nextIndex = index + (event.key === 'ArrowDown' ? 1 : -1)
    if (index < 0 || nextIndex < 0 || nextIndex >= records.length) return
    const previous = keyboardRange.current
    const anchorIndex = records.findIndex(record => record.id === selectionAnchor)
    const continuing = !!previous && previous.anchorId === selectionAnchor && previous.ids.includes(id) && anchorIndex >= 0
    const startIndex = continuing ? anchorIndex : (previous || anchorIndex < 0 ? index : anchorIndex)
    if (!continuing) setSelectionAnchor(records[startIndex].id)
    const range = records.slice(Math.min(startIndex, nextIndex), Math.max(startIndex, nextIndex) + 1).map(record => record.id)
    const previousIds = continuing ? previous!.ids : []
    setSelectedRecords([...new Set([...selectedRecords.filter(selectedId => !previousIds.includes(selectedId)), ...range])])
    keyboardRange.current = { anchorId: records[startIndex].id, ids: range }
    const row = event.currentTarget.closest('tr')
    const nextRow = event.key === 'ArrowDown' ? row?.nextElementSibling : row?.previousElementSibling
    nextRow?.querySelector<HTMLInputElement>('td:first-child input[type="checkbox"]')?.focus()
  }
  const visibleProps=props.filter(p=>p.visible && !hiddenColumns.includes(p.id)).sort((a,b)=>{
    const ai=columnOrder.indexOf(a.id),bi=columnOrder.indexOf(b.id)
    return (ai<0?props.indexOf(a)+columnOrder.length:ai)-(bi<0?props.indexOf(b)+columnOrder.length:bi)
  })
  // Alt+click starts a cell selection; Alt+Shift+click or Alt+drag extends it.
  // Anchor by stable IDs, not indices, and refuse selection while a paste draft
  // is pending. This changes only local selection, never stored cell values.
  const selectCellRectangle = (
    recordId: string, propertyId: string, extend: boolean,
    fallback?: { recordId: string; propertyId: string }
  ) => {
    if (bulkDraft) return
    keyboardRange.current = null
    const endRow = records.findIndex(record => record.id === recordId)
    const endCol = visibleProps.findIndex(property => property.id === propertyId)
    if (endRow < 0 || endCol < 0) return
    const candidate = extend ? (cellAnchor || fallback) : undefined
    const startRow = candidate ? records.findIndex(record => record.id === candidate.recordId) : -1
    const startCol = candidate ? visibleProps.findIndex(property => property.id === candidate.propertyId) : -1
    const anchor = startRow >= 0 && startCol >= 0 ? candidate! : { recordId, propertyId }
    if (!extend || startRow < 0 || startCol < 0) setCellAnchor(anchor)
    const rowIndex = records.findIndex(record => record.id === anchor.recordId)
    const colIndex = visibleProps.findIndex(property => property.id === anchor.propertyId)
    const firstRow = Math.min(rowIndex, endRow), lastRow = Math.max(rowIndex, endRow)
    const firstCol = Math.min(colIndex, endCol), lastCol = Math.max(colIndex, endCol)
    setSelectedRecords(records.slice(firstRow, lastRow + 1).map(record => record.id))
    setCopyStartId(visibleProps[firstCol].id)
    setCopyEndId(visibleProps[lastCol].id)
    setBulkStartProperty(visibleProps[firstCol].id)
    setSelectionAnchor('')
    setCopyError('')
    setCopyStatus('')
  }
  const extendCellWithKeyboard = (event: KeyboardEvent<HTMLTableCellElement>, recordId: string, propertyId: string) => {
    if (!event.altKey || !event.shiftKey || !['ArrowUp','ArrowDown','ArrowLeft','ArrowRight'].includes(event.key) || bulkDraft) return false
    const rowIndex = records.findIndex(record => record.id === recordId)
    const colIndex = visibleProps.findIndex(property => property.id === propertyId)
    const nextRow = rowIndex + (event.key === 'ArrowUp' ? -1 : event.key === 'ArrowDown' ? 1 : 0)
    const nextCol = colIndex + (event.key === 'ArrowLeft' ? -1 : event.key === 'ArrowRight' ? 1 : 0)
    if (nextRow < 0 || nextRow >= records.length || nextCol < 0 || nextCol >= visibleProps.length) return false
    event.preventDefault()
    event.stopPropagation()
    selectCellRectangle(records[nextRow].id, visibleProps[nextCol].id, true, { recordId, propertyId })
    const row = event.currentTarget.parentElement as HTMLTableRowElement | null
    const target = row?.parentElement?.children[nextRow] as HTMLTableRowElement | undefined
    target?.querySelector<HTMLTableCellElement>('td[data-column-id="' + visibleProps[nextCol].id + '"]')
      ?.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('input,select,textarea')?.focus()
    return true
  }
  const changeBulkDraft = (next: string) => {
    if (!bulkDraft && next) setDraftRowOrder(records.filter(record => selectedRecords.includes(record.id)).map(record => record.id))
    if (!next) setDraftRowOrder([])
    setBulkDraft(next)
  }
  // A refresh may re-sort visible rows. Keep each TSV line bound to its original
  // selected record until the draft is discarded or successfully committed.
  const pasteRecords = bulkDraft && draftRowOrder.length
    ? draftRowOrder.map(id => records.find(record => record.id === id)).filter((record): record is DBRecord => !!record)
    : records
  const pasteIds = bulkDraft && draftRowOrder.length ? draftRowOrder : selectedRecords
  const copyStart = visibleProps.some(property => property.id === copyStartId) ? copyStartId : (visibleProps[0]?.id || '')
  const copyEnd = visibleProps.some(property => property.id === copyEndId) ? copyEndId : (visibleProps[visibleProps.length - 1]?.id || '')
  const copyCells = async () => {
    setCopyStatus('')
    setCopyError('')
    try {
      const result = selectedCellsTSV(records, selectedRecords, visibleProps, copyStart, copyEnd)
      await navigator.clipboard.writeText(result.text)
      setCopyStatus('Copied ' + result.rows + ' rows × ' + result.columns + ' columns to clipboard.')
    } catch (error) {
      setCopyError('Copy failed: ' + (error as Error).message)
    }
  }
  const reorder=(id:string,offset:number)=>{
    const ids=visibleProps.map(p=>p.id),index=ids.indexOf(id),target=index+offset
    if(target<0 || target>=ids.length)return
    const swapped=[...ids];[swapped[index],swapped[target]]=[swapped[target],swapped[index]]
    setColumnOrder([...swapped,...columnOrder.filter(id=>!swapped.includes(id))])
  }
  const toggle=(id:string)=>setHiddenColumns(hiddenColumns.includes(id)?hiddenColumns.filter(x=>x!==id):[...hiddenColumns,id])
  // Fixed widths prevent the browser from stretching the Page column across spare space.
  const tableWidth=42+180+visibleProps.reduce((sum,p)=>sum+(columnWidths[p.id]||160),0)
  return <div style={{overflowX:'auto'}}>
    <details style={{marginBottom:10}}>
      <summary>Table columns and layout</summary>
      <div role="group" aria-label="Column visibility" style={{display:'flex',gap:10,flexWrap:'wrap',padding:8}}>
        {props.map(p=><label key={p.id}><input type="checkbox" checked={!hiddenColumns.includes(p.id)} disabled={p.type==='title' || !!bulkDraft} onChange={()=>toggle(p.id)} />{p.name}</label>)}
      </div>
      <small>Column order and widths are stored with this view when you click Save view settings. Use Alt+Arrow keys to move between editable cells; Alt+click and Alt+Shift+click/drag select a rectangular cell range; Alt+Shift+Arrow extends a range from a focused cell; Shift+Up/Down on row checkboxes selects rows; Enter saves, Escape cancels.</small>
    </details>
    {selectedRecords.length>0&&<div style={{marginBottom:8}}>
      <strong>{selectedRecords.length} selected</strong>
      <button aria-label="Clear selected rows" disabled={!!bulkDraft} onClick={()=>{setSelectedRecords([]);setSelectionAnchor('');setCellAnchor(null);keyboardRange.current=null}}>Clear selection</button>
      <label>Copy from <select aria-label="Copy start column" value={copyStart} onChange={event=>setCopyStartId(event.target.value)}>
        {visibleProps.map(property=><option key={property.id} value={property.id}>{property.name}</option>)}
      </select></label>
      <label>through <select aria-label="Copy end column" value={copyEnd} onChange={event=>setCopyEndId(event.target.value)}>
        {visibleProps.map(property=><option key={property.id} value={property.id}>{property.name}</option>)}
      </select></label>
      <button aria-label="Copy selected cells" onClick={()=>void copyCells()}>Copy selected cells</button>
      {copyStatus && <small role="status">{copyStatus}</small>}
      {copyError && <small role="alert">{copyError}</small>}
      {bulkDraft && <small>Selection is locked until the paste draft is applied or discarded.</small>}
      <button aria-label="Delete selected rows" disabled={!!bulkDraft} onClick={async()=>{
        if(!confirm('Move '+selectedRecords.length+' records to Trash?'))return
        for(let i=0;i<selectedRecords.length;i++){
          // A failed deletion must not silently clear the remaining selection.
          // Bulk deletion is not atomic: stop and preserve failed/unprocessed IDs.
          if(!await deleteRecord(selectedRecords[i],true)){
            setSelectedRecords(selectedRecords.slice(i))
            return
          }
        }
        setSelectedRecords([])
        keyboardRange.current = null
      }}>Move selected to Trash</button>
    </div>}
    {(selectedRecords.length > 0 || bulkDraft) && <DatabaseBulkPaste
      records={pasteRecords} properties={visibleProps} selectedIds={pasteIds}
      draft={bulkDraft} onDraftChange={changeBulkDraft}
      startPropertyId={bulkStartProperty} onStartPropertyChange={setBulkStartProperty}
      onApply={applyBulkEdit} onRefreshVersions={refreshBulkVersions} disabled={bulkDisabled}
    />}
    <table style={{borderCollapse:'collapse',tableLayout:'fixed',width:tableWidth}}>
      <colgroup><col style={{width:42}}/><col style={{width:180}}/>{visibleProps.map(p=><col key={p.id} style={{width:columnWidths[p.id]||160}} />)}</colgroup>
      <thead><tr>
        <th><input aria-label="Select all visible records" type="checkbox" disabled={!!bulkDraft} checked={records.length>0 && records.every(r=>selectedRecords.includes(r.id))} onChange={e=>{keyboardRange.current=null;setCellAnchor(null);setSelectedRecords(e.target.checked?[...new Set([...selectedRecords,...records.map(r=>r.id)])]:selectedRecords.filter(id=>!records.some(r=>r.id===id)))}} /></th>
        <th>Page</th>{visibleProps.map((p,index)=><th key={p.id} style={{border:'1px solid var(--border)',padding:5,width:columnWidths[p.id]||160,minWidth:100}}>
          <div style={{display:'flex',alignItems:'center',gap:4}}><span>{p.name}</span>
            <button aria-label={'Move '+p.name+' left'} disabled={index===0 || !!bulkDraft} onClick={()=>reorder(p.id,-1)}>‹</button>
            <button aria-label={'Move '+p.name+' right'} disabled={index===visibleProps.length-1 || !!bulkDraft} onClick={()=>reorder(p.id,1)}>›</button>
            {p.type!=='title'&&<button aria-label={'Delete '+p.name} disabled={!!bulkDraft} onClick={()=>{if(confirm('Move property '+p.name+' to Trash?'))void deleteProperty(p.id)}}>×</button>}
          </div>
          <input type="number" aria-label={'Width '+p.name} title={'Column width: '+p.name} min={100} max={700} step={10} disabled={!!bulkDraft} value={columnWidths[p.id]||160} onChange={e=>{
            const width=Math.min(700,Math.max(100,Number(e.target.value)||160))
            setColumnWidths({...columnWidths,[p.id]:width})
          }} style={{width:64}} />
        </th>)}</tr></thead>
      <tbody>{records.map(rec=><tr key={rec.id} data-testid="database-record-row" data-record-id={rec.id}>
        <td><input aria-label={'Select record '+rec.title} type="checkbox" disabled={!!bulkDraft} checked={selectedRecords.includes(rec.id)} onChange={e=>selectVisibleRow(rec.id,e.target.checked,(e.nativeEvent as MouseEvent).shiftKey)} onKeyDown={e=>extendVisibleRowWithKeyboard(e,rec.id)}/></td>
        <td><button onClick={()=>rec.pageId&&onOpenRecord?.(rec.pageId)}>Open page</button><button aria-label={'Delete record '+rec.title} onClick={()=>{void deleteRecord(rec.id)}}>Delete</button></td>
        {visibleProps.map(p=><td key={p.id} data-column-id={p.id} onKeyDown={event=>{if(!extendCellWithKeyboard(event,rec.id,p.id))navigateTableCell(event)}} onPointerDownCapture={event=>{if(event.pointerType==='mouse' && event.button===0 && event.altKey && !bulkDraft){event.preventDefault();selectCellRectangle(rec.id,p.id,event.shiftKey)}}} onPointerEnter={event=>{if(event.pointerType==='mouse' && event.altKey && event.buttons===1 && !bulkDraft)selectCellRectangle(rec.id,p.id,true)}} onClickCapture={event=>{if(event.altKey){event.preventDefault();event.stopPropagation()}}} style={{border:'1px solid var(--border)',padding:4,width:columnWidths[p.id]||160,outline:selectedRecords.includes(rec.id) && visibleProps.findIndex(prop=>prop.id===p.id)>=visibleProps.findIndex(prop=>prop.id===copyStart) && visibleProps.findIndex(prop=>prop.id===p.id)<=visibleProps.findIndex(prop=>prop.id===copyEnd)?'1px solid var(--accent)':'none'}}>
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
  if (prop.type === 'number') return <input aria-label={prop.name} type="number" key={String(value)} defaultValue={value ?? ''}
    onBlur={e => {
      const edited=e.currentTarget.value === '' ? null : Number(e.currentTarget.value)
      const original=value==null || value==='' ? null : Number(value)
      if (edited!==original) onChange(edited)
    }}
    onKeyDown={e => {
      if(e.key==='Enter')e.currentTarget.blur()
      if(e.key==='Escape'){e.currentTarget.value=String(value??'');e.currentTarget.blur()}
    }} />
  if (prop.type === 'date') return <input aria-label={prop.name} type="date" value={String(value ?? '').slice(0, 10)} onChange={e => onChange(e.target.value || null)} />
  if (prop.type === 'select') {
    let options: string[] = []
    try { options = JSON.parse(prop.config || '{}').options || [] } catch { /* invalid legacy config */ }
    return <select aria-label={prop.name} value={value ?? ''} onChange={e => onChange(e.target.value || null)}>
      <option value="">—</option>{options.map(o => <option key={o} value={o}>{o}</option>)}
    </select>
  }
  if (prop.type === 'multi_select') return <input aria-label={prop.name} key={JSON.stringify(value)} defaultValue={Array.isArray(value) ? value.join(',') : ''}
    onBlur={e => {
      const edited=e.currentTarget.value.split(',').map(x=>x.trim()).filter(Boolean)
      const original=Array.isArray(value)?value:[]
      if(JSON.stringify(edited)!==JSON.stringify(original))onChange(edited)
    }}
    onKeyDown={e => {
      if(e.key==='Enter')e.currentTarget.blur()
      if(e.key==='Escape'){e.currentTarget.value=Array.isArray(value)?value.join(','):'';e.currentTarget.blur()}
    }} />
  return <input aria-label={prop.name} key={String(value)} defaultValue={value ?? ''}
    onBlur={e => { if (String(value ?? '') !== e.currentTarget.value) onChange(e.currentTarget.value) }}
    onKeyDown={e => {
      if(e.key==='Enter')e.currentTarget.blur()
      if(e.key==='Escape'){e.currentTarget.value=String(value??'');e.currentTarget.blur()}
    }}
    style={{ width: '100%', border: 'none', background: 'transparent', color: 'var(--fg)' }} />
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
        key={index} onDragOver={event=>{if(day){event.preventDefault();event.dataTransfer.dropEffect='move'}}} onDrop={event=>{
          if(!day)return
          event.preventDefault()
          const recordID=event.dataTransfer.getData('application/x-reverie-record') || draggedRecord
          if(!recordID)return
          const record=records.find(r=>r.id===recordID)
          setDraggedRecord(null)
          if(record)void updateRecord(record,{values:{[dateProp.id]:calendarDay(year,month,day)}})
        }} style={{ border: '1px solid var(--border)', background: 'var(--card)', padding: 4, minHeight: 80 }}>
        <small>{day || ''}</small>
        {day && records.filter(rec => String(rec.values?.[dateProp.id] ?? '').slice(0,10) === calendarDay(year,month,day)).map(rec => <div key={rec.id} data-testid="calendar-record">
          <span data-testid="calendar-drag-handle" draggable aria-label={'Drag '+rec.title+' to another day'} title="Drag to move date"
            onDragStart={event=>{event.dataTransfer.setData('application/x-reverie-record',rec.id);event.dataTransfer.effectAllowed='move';setDraggedRecord(rec.id)}}
            onDragEnd={()=>setDraggedRecord(null)} style={{cursor:'grab',userSelect:'none',display:'inline-block',padding:'4px 8px'}}>⋮⋮</span>
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
function ListView({ records, onOpenRecord, deleteRecord }: { records: DBRecord[]; onOpenRecord?: OpenRecord; deleteRecord: (id:string) => Promise<boolean> }) {
  return <div style={{ display: 'grid', gap: 8 }}>{records.map(rec => <div data-testid="list-record" key={rec.id} style={{ borderBottom: '1px solid var(--border)', padding: 8 }}>
    <strong>{rec.title}</strong> <button onClick={() => rec.pageId && onOpenRecord?.(rec.pageId)}>Open page</button>
    <button aria-label={'Delete record '+rec.title} onClick={()=>void deleteRecord(rec.id)}>Move to Trash</button>
  </div>)}</div>
}
function GalleryView({ records, props, propertyIds, coverProperty, onOpenRecord, deleteRecord }: {
  records: DBRecord[]; props: DBProperty[]; propertyIds: string[] | null; coverProperty: string; onOpenRecord?: OpenRecord; deleteRecord: (id:string) => Promise<boolean>
}) {
  const fields=props.filter(p=>p.type!=='title')
  const visible=propertyIds===null ? fields.slice(0,3) : fields.filter(p=>propertyIds.includes(p.id))
  const cover=props.find(p=>p.id===coverProperty && p.type==='file')
  return <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 12 }}>
    {records.map(rec => {
      const files=cover ? rec.values?.[cover.id] : null
      const coverId=Array.isArray(files) && typeof files[0]==='string' ? files[0] : ''
      return <article data-testid="gallery-record" key={rec.id} style={{ border: '1px solid var(--border)', background: 'var(--card)', padding: 12, borderRadius: 8 }}>
        {coverId && <img src={'/api/db/files?id='+encodeURIComponent(coverId)} alt={'Cover for '+rec.title} loading="lazy" style={{width:'100%',height:130,objectFit:'cover',borderRadius:6}} />}
        <strong>{rec.title}</strong><dl>{visible.map(property=>{
          const value=rec.values?.[property.id]
          const text=value==null ? '—' : Array.isArray(value) ? value.join(', ') : String(value)
          return <div key={property.id}><dt>{property.name}</dt><dd style={{marginLeft:0,overflowWrap:'anywhere'}}>{text}</dd></div>
        })}</dl>
        <button onClick={() => rec.pageId && onOpenRecord?.(rec.pageId)}>Open page</button>
        <button aria-label={'Delete record '+rec.title} onClick={()=>void deleteRecord(rec.id)}>Move to Trash</button>
      </article>
    })}
  </div>
}
