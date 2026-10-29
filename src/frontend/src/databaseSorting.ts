// Sort records consistently across the five database layouts. Null and empty
// values always appear last, independently of ascending/descending direction.
export type DatabaseSortRule = { propertyId: string; direction: 'asc' | 'desc' }
export type DatabaseSortProperty = { id: string; type: string }
export type DatabaseSortableRecord = { id: string; title: string; values: Record<string, unknown> }

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
function empty(value: unknown): boolean {
  return value === null || value === undefined || value === '' || (Array.isArray(value) && value.length === 0)
}
function compareValue(left: unknown, right: unknown, type: string): number {
  if (empty(left)) return empty(right) ? 0 : 1
  if (empty(right)) return -1
  if (type === 'number' || type === 'formula' || type === 'rollup') {
    const a = typeof left === 'number' ? left : Number(left)
    const b = typeof right === 'number' ? right : Number(right)
    if (Number.isFinite(a) && Number.isFinite(b)) return a - b
  }
  if (type === 'checkbox') return Number(Boolean(left)) - Number(Boolean(right))
  if (type === 'date' || type === 'created_time' || type === 'last_edited_time') {
    return String(left).localeCompare(String(right))
  }
  const stringValue = (value: unknown): string =>
    Array.isArray(value) ? value.map(item => String(item)).join(', ') : String(value)
  return collator.compare(stringValue(left), stringValue(right))
}
export function sortDatabaseRecords<R extends DatabaseSortableRecord>(
  records: R[],
  properties: DatabaseSortProperty[],
  rules: DatabaseSortRule[]
): R[] {
  const valid = rules.filter(rule => rule.propertyId === 'title' || properties.some(p => p.id === rule.propertyId))
  if (!valid.length) return records
  const byId = new Map(properties.map(p => [p.id, p]))
  // JS Array.sort is stable in modern browsers; original order wins ties.
  return [...records].sort((a, b) => {
    for (const rule of valid) {
      const type = rule.propertyId === 'title' ? 'title' : byId.get(rule.propertyId)?.type || 'text'
      const left = rule.propertyId === 'title' ? a.title : a.values?.[rule.propertyId]
      const right = rule.propertyId === 'title' ? b.title : b.values?.[rule.propertyId]
      // Nulls remain last even when descending.
      const missingLeft = empty(left), missingRight = empty(right)
      const cmp = compareValue(left, right, type)
      if (cmp !== 0) {
        if (missingLeft || missingRight) return cmp
        return rule.direction === 'desc' ? -cmp : cmp
      }
    }
    return 0
  })
}
