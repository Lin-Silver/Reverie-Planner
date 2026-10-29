import type { DBProperty, DBRecord } from './DatabaseView'

const COPYABLE_TYPES = new Set(['title', 'text', 'number', 'checkbox', 'date', 'select', 'multi_select', 'email', 'url'])

// Only produce a rectangular, paste-compatible TSV. Refuse ambiguous cell
// delimiters rather than copying text that would silently shift other cells.
export function selectedCellsTSV(
  records: DBRecord[], selectedIds: string[], properties: DBProperty[],
  firstId: string, lastId: string
): { text: string; rows: number; columns: number } {
  const selected = records.filter(record => selectedIds.includes(record.id))
  if (!selected.length || selected.length > 100 || selected.length !== selectedIds.length) {
    throw new Error('Select 1–100 visible rows to copy.')
  }
  const first = properties.findIndex(property => property.id === firstId)
  const last = properties.findIndex(property => property.id === lastId)
  if (first < 0 || last < first) throw new Error('Choose a valid start and end column.')
  const columns = properties.slice(first, last + 1)
  const unsupported = columns.find(property => !COPYABLE_TYPES.has(property.type))
  if (unsupported) throw new Error('Copy is not supported for ' + unsupported.name + ' (' + unsupported.type + ').')

  const text = selected.map(record => columns.map(property => {
    const value = property.type === 'title' ? record.title : record.values?.[property.id]
    let cell: string
    if (property.type === 'multi_select') {
      if (value != null && !Array.isArray(value)) throw new Error('Invalid multi-select value in ' + property.name)
      const options: unknown[] = value ?? []
      if (options.some(option => typeof option !== 'string' || option.includes(','))) {
        throw new Error('Cannot safely copy multi-select options containing commas or invalid values.')
      }
      cell = options.join(',')
    } else if (property.type === 'checkbox') {
      cell = value ? 'true' : 'false'
    } else {
      cell = value == null ? '' : String(value)
    }
    if (/[\t\r\n]/.test(cell)) throw new Error('Cannot copy cells containing tabs or line breaks as TSV.')
    return cell
  }).join('\t')).join('\n')

  if (text.length > 100000) throw new Error('Copied data exceeds the 100 KB paste limit.')
  return { text, rows: selected.length, columns: columns.length }
}
