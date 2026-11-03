import { useEffect, useState } from 'react'
import Dialog from './Dialog'

type PropertyInfo = { id: string; databaseId?: string; name: string; type: string; config: string; position?: number; visible?: boolean; version?: number }
type DatabaseInfo = { id: string; title: string }
const TYPES = ['text','number','checkbox','select','multi_select','date','url','email','person','file','relation','formula','rollup','created_time','last_edited_time'] as const

function optionList(config?: string): string {
  try {
    const parsed = JSON.parse(config || '{}') as { options?: unknown }
    return Array.isArray(parsed.options) ? parsed.options.map(String).join(', ') : ''
  } catch { return '' }
}

// Edit mode completes the create-only form: rename, select option editing and
// visibility are supported by PUT /api/db/properties. Type changes stay
// disabled because the backend requires a data migration for them.
export default function DatabasePropertyDialog({ databaseId, properties, property, onClose, onSaved }: {
  databaseId: string; properties: PropertyInfo[]; property?: PropertyInfo; onClose: () => void; onSaved: () => void
}) {
  const editing = !!property
  const [name, setName] = useState(property?.name || '')
  const [type, setType] = useState<string>(property?.type || 'text')
  const [options, setOptions] = useState(editing ? optionList(property?.config) : 'Todo,Done')
  const [visible, setVisible] = useState(property ? property.visible !== false : true)
  const [databases, setDatabases] = useState<DatabaseInfo[]>([])
  const [targetDB, setTargetDB] = useState('')
  const [formulaOp, setFormulaOp] = useState('sum')
  const [formulaA, setFormulaA] = useState('')
  const [formulaB, setFormulaB] = useState('')
  const [relationProperty, setRelationProperty] = useState('')
  const [targetProperty, setTargetProperty] = useState('')
  const [targetProperties, setTargetProperties] = useState<PropertyInfo[]>([])
  const [rollupOp, setRollupOp] = useState('sum')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  useEffect(() => {
    let live = true
    fetch('/api/databases').then(res => { if (!res.ok) throw new Error(String(res.status)); return res.json() })
      .then((data: DatabaseInfo[]) => { if (live) setDatabases(data) })
      .catch(e => { if (live) setError('Unable to load databases: ' + String(e)) })
    return () => { live = false }
  }, [])
  const numberFields = properties.filter(p => p.type === 'number')
  const references = properties.filter(p => p.type === 'relation')
  useEffect(() => {
    let live = true
    const relation = references.find(p => p.id === relationProperty)
    if (!relation) { return }
    let target = ''
    try { target = JSON.parse(relation.config).targetDatabaseId || '' } catch { return }
    if (!target) return
    fetch('/api/databases?id=' + encodeURIComponent(target)).then(r => {
      if (!r.ok) throw new Error(String(r.status))
      return r.json()
    }).then((db: {properties: PropertyInfo[]}) => { if (live) setTargetProperties(db.properties) })
      .catch(e => { if (live) setError('Cannot load target properties: ' + String(e)) })
    return () => { live = false }
  }, [relationProperty, properties])
  const acceptsOptions = type === 'select' || type === 'multi_select'
  const save = async () => {
    if (saving) return
    if (!name.trim()) { setError('Property name is required'); return }
    let config: Record<string, unknown> = {}
    if (acceptsOptions) config = { options: [...new Set(options.split(',').map(o => o.trim()).filter(Boolean))] }
    if (!editing) {
      if (type === 'relation') {
        if (!targetDB) { setError('Select a target database'); return }
        config = { targetDatabaseId: targetDB }
      }
      if (type === 'formula') {
        if (!formulaA) { setError('Choose a formula operand'); return }
        config = { operation: formulaOp, args: [{propertyId: formulaA}, ...(formulaB ? [{propertyId: formulaB}] : [])] }
      }
      if (type === 'rollup') {
        if (!relationProperty || !targetProperty) { setError('Choose a relation and target field'); return }
        config = {relationPropertyId: relationProperty, targetPropertyId: targetProperty, aggregate: rollupOp}
      }
    }
    setSaving(true)
    setError('')
    try {
      if (editing) {
        // Relation/formula/rollup configuration is preserved verbatim; only
        // select options are rewritten (and validated against stored values).
        const body = {
          id: property!.id,
          databaseId: property!.databaseId || databaseId,
          name: name.trim(),
          type: property!.type,
          config: acceptsOptions ? JSON.stringify(config) : (property!.config || '{}'),
          position: property!.position ?? 0,
          visible: property!.type === 'title' ? true : visible,
          version: property!.version,
        }
        const res = await fetch('/api/db/properties', {method:'PUT',headers:{'Content-Type':'application/json'},
          body:JSON.stringify(body)})
        if (!res.ok) {
          const message = await res.json().catch(() => ({error: res.statusText})) as {error?:string}
          throw new Error(message.error || String(res.status))
        }
      } else {
        const res = await fetch('/api/db/properties', {method:'POST',headers:{'Content-Type':'application/json'},
          body:JSON.stringify({ databaseId, name:name.trim(), type, config:JSON.stringify(config) })})
        if (!res.ok) {
          const message = await res.json().catch(() => ({error: res.statusText})) as {error?:string}
          throw new Error(message.error || String(res.status))
        }
      }
      onSaved()
    } catch (e) {setError((e as Error).message)}
    finally {setSaving(false)}
  }
  return <Dialog label={editing ? 'Edit database property' : 'New database property'} onClose={onClose}>
    <section style={{width:'min(480px,100%)',maxHeight:'85vh',overflowY:'auto',padding:20,borderRadius:12,background:'var(--card)',border:'1px solid var(--border)',display:'grid',gap:12}}>
      <h3 style={{margin:0}}>{editing ? 'Edit database property' : 'New database property'}</h3>
      <label>Name <input aria-label="Property name" value={name} onChange={e=>setName(e.target.value)} autoFocus /></label>
      <label>Type <select aria-label="Property type" value={type} disabled={editing} onChange={e=>setType(e.target.value)}>
        {editing ? <option value={property!.type}>{property!.type.replaceAll('_',' ')}</option> : TYPES.map(t=><option key={t} value={t}>{t.replaceAll('_',' ')}</option>)}
      </select></label>
      {editing && <small>Changing a property type needs a data migration; create a new property instead.</small>}
      {acceptsOptions && <label>Options (comma separated) <input aria-label="Property options" value={options} onChange={e=>setOptions(e.target.value)}/></label>}
      {editing && !acceptsOptions && <small>The existing {property!.type.replaceAll('_',' ')} configuration is kept unchanged.</small>}
      {!editing && type==='relation' && <label>Target database <select aria-label="Relation target database" value={targetDB} onChange={e=>setTargetDB(e.target.value)}>
        <option value="">Choose database</option>{databases.map(db=><option key={db.id} value={db.id}>{db.title}</option>)}
      </select></label>}
      {!editing && type==='formula' && <>
        <label>Operation <select aria-label="Formula operation" value={formulaOp} onChange={e=>setFormulaOp(e.target.value)}>
          {['sum','subtract','multiply','divide','concat'].map(x=><option key={x} value={x}>{x}</option>)}
        </select></label>
        <label>First property <select aria-label="Formula first property" value={formulaA} onChange={e=>setFormulaA(e.target.value)}>
          <option value="">Choose property</option>{properties.filter(p=>formulaOp==='concat'||p.type==='number').map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select></label>
        <label>Second property (optional) <select aria-label="Formula second property" value={formulaB} onChange={e=>setFormulaB(e.target.value)}>
          <option value="">None</option>{(formulaOp==='concat'?properties:numberFields).map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select></label>
      </>}
      {!editing && type==='rollup' && <>
        <label>Relation <select aria-label="Rollup relation" value={relationProperty} onChange={e=>{setRelationProperty(e.target.value);setTargetProperty('');setTargetProperties([])}}>
          <option value="">Choose relation</option>{references.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select></label>
        <label>Target property <select aria-label="Rollup target property" value={targetProperty} onChange={e=>setTargetProperty(e.target.value)}>
          <option value="">Choose property</option>{targetProperties.filter(p=>rollupOp==='count'||p.type==='number').map(p=><option key={p.id} value={p.id}>{p.name}</option>)}
        </select></label>
        <label>Aggregate <select aria-label="Rollup operation" value={rollupOp} onChange={e=>setRollupOp(e.target.value)}>
          {['sum','count','average','min','max'].map(x=><option key={x} value={x}>{x}</option>)}
        </select></label>
      </>}
      {(type==='created_time'||type==='last_edited_time') && <p>System-maintained, read-only timestamp.</p>}
      {editing && property!.type !== 'title' && <label style={{display:'flex',alignItems:'center',gap:8}}>
        <input type="checkbox" aria-label="Show property in database" checked={visible} onChange={e=>setVisible(e.target.checked)} /> Show property in database
      </label>}
      {error && <p role="alert" style={{color:'#dc2626'}}>{error}</p>}
      <div style={{display:'flex',justifyContent:'flex-end',gap:8}}>
        <button onClick={onClose}>Cancel</button><button className="btn primary" disabled={saving} onClick={()=>{void save()}}>{saving ? (editing ? 'Saving…' : 'Creating…') : editing ? 'Save property' : 'Create property'}</button>
      </div>
    </section>
  </Dialog>
}
