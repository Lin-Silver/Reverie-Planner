import { filterOperators, newFilterGroup, newFilterRule, type FilterExpr, type FilterGroup, type FilterOperator, type FilterProperty, type FilterRule } from './databaseFilters'

export default function DatabaseFilterBuilder({ group, properties, onChange }: { group: FilterGroup; properties: FilterProperty[]; onChange: (value: FilterGroup) => void }) {
  const field = (rule: FilterRule) => properties.find(p => p.id === rule.propertyId)
  const modify = (index: number, child: FilterExpr) => onChange({ ...group, children: group.children.map((item, i) => i === index ? child : item) })
  return <fieldset style={{ border: '1px solid var(--border)', padding: 8, borderRadius: 6, minWidth: 0 }}>
    <legend>Filter group</legend>
    <label>Match
      <select aria-label="Filter join" value={group.join} onChange={e => onChange({ ...group, join: e.target.value as 'and' | 'or' })}>
        <option value="and">All (AND)</option><option value="or">Any (OR)</option>
      </select>
    </label>
    {group.children.map((child, index) => <div key={index} style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap', marginTop: 8 }}>
      {child.kind === 'group' ? <DatabaseFilterBuilder group={child} properties={properties} onChange={g => modify(index, g)} /> : <>
        <select aria-label="Filter property" value={child.propertyId} onChange={e => {
          const next = properties.find(p => p.id === e.target.value)
          modify(index, { ...child, propertyId: e.target.value, operator: filterOperators(next?.type || 'text')[0], value: '' })
        }}>{properties.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
        <select aria-label="Filter operator" value={child.operator} onChange={e => modify(index, { ...child, operator: e.target.value as FilterOperator })}>
          {filterOperators(field(child)?.type || 'text').map(operator => <option key={operator} value={operator}>{operator.replaceAll('_',' ')}</option>)}
        </select>
        {child.operator !== 'is_empty' && child.operator !== 'is_not_empty' && (field(child)?.type === 'checkbox' ? <select aria-label="Filter value" value={child.value} onChange={e => modify(index, { ...child, value: e.target.value })}>
          <option value="">Choose</option><option value="true">Checked</option><option value="false">Unchecked</option>
        </select> : <input aria-label="Filter value" type={field(child)?.type === 'number' ? 'number' : field(child)?.type === 'date' ? 'date' : 'text'} value={child.value} onChange={e => modify(index, { ...child, value: e.target.value })} />)}
      </>}
      <button aria-label="Remove filter" type="button" onClick={() => onChange({ ...group, children: group.children.filter((_, i) => i !== index) })}>Remove</button>
    </div>)}
    <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
      <button type="button" aria-label="Add filter condition" disabled={properties.length === 0} onClick={() => onChange({ ...group, children: [...group.children, newFilterRule(properties)] })}>+ Condition</button>
      <button type="button" aria-label="Add nested filter group" onClick={() => onChange({ ...group, children: [...group.children, newFilterGroup()] })}>+ Group</button>
    </div>
  </fieldset>
}
