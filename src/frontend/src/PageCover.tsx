import { useState } from 'react'
import Dialog from './Dialog'
import { COVER_PRESETS, parseCover } from './pageCoverPresets'

export default function PageCover({ cover, title, onChange }: {
  cover?: string
  title: string
  onChange: (cover: string) => Promise<void>
}) {
  const parsed = parseCover(cover)
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState('')
  const [urlDraft, setUrlDraft] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const start = () => {
    setDraft(parsed?.kind === 'gradient' ? parsed.value : '')
    setUrlDraft(parsed?.kind === 'image' ? parsed.value : '')
    setError('')
    setOpen(true)
  }

  const selected = parseCover(draft || urlDraft)
  const save = async () => {
    if (!selected) { setError('Choose a preset or enter an http(s) or same-origin image URL.'); return }
    setSaving(true)
    try {
      await onChange(selected.value)
      setOpen(false)
    } catch (e) {
      setError((e as Error).message)
    } finally { setSaving(false) }
  }

  return (
    <>
      {parsed ? (
        <div className="page-cover" data-testid="page-cover" style={parsed.kind === 'gradient' ? { backgroundImage: parsed.value } : undefined}>
          {parsed.kind === 'image' && <img src={parsed.value} alt="" />}
          <div className="page-cover-actions">
            <button className="btn" onClick={start}>Change cover</button>
            <button className="btn" disabled={saving} onClick={async () => {
              setSaving(true)
              try { await onChange('') } catch (e) { setError((e as Error).message) } finally { setSaving(false) }
            }}>Remove cover</button>
          </div>
          <span className="sr-only">{`Cover for ${title || 'Untitled'}`}</span>
        </div>
      ) : (
        <button className="btn quiet page-cover-add" aria-label={`Add cover to ${title || 'Untitled'}`} onClick={start}>Add cover</button>
      )}
      {error && !open && <p role="alert" className="page-cover-error">{error}</p>}
      {open && (
        <Dialog label="Page cover" onClose={() => setOpen(false)}>
          <div className="modal" style={{ width: 'min(520px, 100%)' }} onClick={(e) => e.stopPropagation()}>
            <h3>Page cover</h3>
            <div className="cover-preset-grid" role="group" aria-label="Cover presets">
              {COVER_PRESETS.map((preset) => (
                <button key={preset.id} className="cover-preset" aria-pressed={draft === preset.value}
                  aria-label={`Use ${preset.label} cover`} style={{ backgroundImage: preset.value }}
                  onClick={() => { setDraft(preset.value); setUrlDraft(''); setError('') }}>
                  <span>{preset.label}</span>
                </button>
              ))}
            </div>
            <label className="form-row" style={{ marginTop: 12 }}>
              <span>Or image URL</span>
              <input aria-label="Cover image URL" placeholder="https://… or /api/page-assets?id=…" value={urlDraft}
                onChange={(e) => { setUrlDraft(e.target.value); setDraft(''); setError('') }} />
            </label>
            {selected && (
              <div className="page-cover cover-preview" aria-hidden="true" style={selected.kind === 'gradient' ? { backgroundImage: selected.value } : undefined}>
                {selected.kind === 'image' && <img src={selected.value} alt="" />}
              </div>
            )}
            {error && <p role="alert">{error}</p>}
            <div className="row" style={{ justifyContent: 'space-between', marginTop: 12 }}>
              <button className="btn quiet" disabled={saving} onClick={() => setOpen(false)}>Cancel</button>
              <div className="row">
                {cover && <button className="btn quiet" disabled={saving} onClick={async () => {
                  setSaving(true)
                  try { await onChange(''); setOpen(false) } catch (e) { setError((e as Error).message) } finally { setSaving(false) }
                }}>Remove cover</button>}
                <button className="btn primary" disabled={saving} onClick={() => void save()}>{saving ? 'Saving…' : 'Save cover'}</button>
              </div>
            </div>
          </div>
        </Dialog>
      )}
    </>
  )
}
