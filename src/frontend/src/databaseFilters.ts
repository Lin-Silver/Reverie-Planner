// Pure typed predicates used by every database view. No eval, SQL fragments or regexes.
export type FilterOperator = 'equals' | 'not_equals' | 'contains' | 'not_contains' | 'gt' | 'gte' | 'lt' | 'lte' | 'before' | 'after' | 'is_empty' | 'is_not_empty'
export type FilterRule = { kind: 'rule'; propertyId: string; operator: FilterOperator; value: string }
export type FilterGroup = { kind: 'group'; join: 'and' | 'or'; children: FilterExpr[] }
export type FilterExpr = FilterRule | FilterGroup
export type FilterProperty = { id: string; name: string; type: string }
export type FilterRecord = { title: string; values: Record<string, unknown> }

const common: FilterOperator[] = ['equals', 'not_equals', 'is_empty', 'is_not_empty']
const textOps: FilterOperator[] = ['equals', 'not_equals', 'contains', 'not_contains', 'is_empty', 'is_not_empty']
const numberOps: FilterOperator[] = ['equals', 'not_equals', 'gt', 'gte', 'lt', 'lte', 'is_empty', 'is_not_empty']
const dateOps: FilterOperator[] = ['equals', 'not_equals', 'before', 'after', 'is_empty', 'is_not_empty']
export function filterOperators(type: string): FilterOperator[] {
  switch (type) {
    case 'title': case 'text': case 'url': case 'email': case 'person': return textOps
    case 'number': case 'formula': case 'rollup': return numberOps
    case 'date': case 'created_time': case 'last_edited_time': return dateOps
    case 'select': case 'multi_select': case 'relation': return textOps
    default: return common
  }
}
const empty = (value: unknown) => value == null || value === '' || (Array.isArray(value) && value.length === 0)
function canonicalDate(input: unknown): string {
  if (typeof input !== 'string') return ''
  const candidate = input.slice(0, 10)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(candidate)) return ''
  const d = new Date(candidate + 'T00:00:00Z')
  return Number.isNaN(d.valueOf()) || d.toISOString().slice(0, 10) !== candidate ? '' : candidate
}
export function matchesFilters(record: FilterRecord, props: FilterProperty[], expression: FilterExpr | null | undefined, depth = 0): boolean {
  if (!expression) return true
  if (depth > 8) return false
  if (expression.kind === 'group') {
    if (!Array.isArray(expression.children) || expression.children.length === 0) return true
    if (expression.children.length > 40) return false
    return expression.join === 'and'
      ? expression.children.every(e => matchesFilters(record, props, e, depth + 1))
      : expression.join === 'or' && expression.children.some(e => matchesFilters(record, props, e, depth + 1))
  }
  if (expression.kind !== 'rule') return false
  const prop = props.find(p => p.id === expression.propertyId)
  if (!prop || !filterOperators(prop.type).includes(expression.operator)) return false
  const value = prop.type === 'title' ? record.title : record.values[prop.id]
  if (expression.operator === 'is_empty') return empty(value)
  if (expression.operator === 'is_not_empty') return !empty(value)
  if (empty(value)) return expression.operator === 'not_equals' && expression.value !== ''
  let found = value
  if (Array.isArray(value)) found = value.join('\n')
  const scalar = String(found).toLocaleLowerCase()
  const expected = String(expression.value ?? '').toLocaleLowerCase()
  if (expression.operator === 'contains') return Array.isArray(value) ? value.some(x => String(x).toLocaleLowerCase().includes(expected)) : scalar.includes(expected)
  if (expression.operator === 'not_contains') return !scalar.includes(expected)
  if (expression.operator === 'equals') {
    if (prop.type === 'number') return Number(value) === Number(expression.value) && expression.value.trim() !== ''
    if (typeof value === 'boolean') return String(value) === expression.value
    return Array.isArray(value) ? value.some(x => String(x).toLocaleLowerCase() === expected) : scalar === expected
  }
  if (expression.operator === 'not_equals') {
    if (prop.type === 'number') return Number(value) !== Number(expression.value) && expression.value.trim() !== ''
    if (typeof value === 'boolean') return String(value) !== expression.value
    return Array.isArray(value) ? !value.some(x => String(x).toLocaleLowerCase() === expected) : scalar !== expected
  }
  if (['gt', 'gte', 'lt', 'lte'].includes(expression.operator)) {
    if (expression.value.trim() === '') return false
    const lhs = Number(value), rhs = Number(expression.value)
    if (!Number.isFinite(lhs) || !Number.isFinite(rhs)) return false
    if (expression.operator === 'gt') return lhs > rhs
    if (expression.operator === 'gte') return lhs >= rhs
    if (expression.operator === 'lt') return lhs < rhs
    return lhs <= rhs
  }
  const lhs = canonicalDate(value), rhs = canonicalDate(expression.value)
  if (!lhs || !rhs) return false
  return expression.operator === 'before' ? lhs < rhs : expression.operator === 'after' ? lhs > rhs : false
}
export function newFilterRule(properties: FilterProperty[]): FilterRule {
  const prop = properties[0]
  return { kind: 'rule', propertyId: prop?.id || '', operator: 'equals', value: '' }
}
export function newFilterGroup(): FilterGroup {
  return { kind: 'group', join: 'and', children: [] }
}
