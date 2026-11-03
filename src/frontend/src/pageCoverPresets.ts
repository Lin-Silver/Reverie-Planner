// Stored verbatim in pages.cover. Only these literals render as CSS gradients,
// so an arbitrary database value can never inject styles into the page header.
export const COVER_PRESETS = [
  { id: 'dawn', label: 'Dawn', value: 'linear-gradient(135deg,#f6d365 0%,#fda085 100%)' },
  { id: 'mist', label: 'Mist', value: 'linear-gradient(135deg,#d7e7f4 0%,#f5e7ec 55%,#c9bfde 100%)' },
  { id: 'forest', label: 'Forest', value: 'linear-gradient(135deg,#134e5e 0%,#71b280 100%)' },
  { id: 'dusk', label: 'Dusk', value: 'linear-gradient(135deg,#355c7d 0%,#6c5b7b 50%,#c06c84 100%)' },
  { id: 'paper', label: 'Paper', value: 'linear-gradient(135deg,#e9e4f0 0%,#dfe9f3 100%)' },
  { id: 'ember', label: 'Ember', value: 'linear-gradient(135deg,#fc5c7d 0%,#6a82fb 100%)' },
]

export type ParsedCover = { kind: 'gradient' | 'image'; value: string }

// Same-origin paths (imported attachments, wallpapers) and absolute HTTP URLs
// are accepted; protocol-relative and script/data schemes are rejected.
export function parseCover(raw?: string | null): ParsedCover | null {
  const value = (raw || '').trim()
  if (!value) return null
  if (COVER_PRESETS.some((preset) => preset.value === value)) return { kind: 'gradient', value }
  if (/^https?:\/\/[^\s"'<>]+$/i.test(value)) return { kind: 'image', value }
  if (/^\/(?!\/)[^\s"'<>]*$/.test(value)) return { kind: 'image', value }
  return null
}
