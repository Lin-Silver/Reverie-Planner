import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useCreateBlockNote } from '@blocknote/react'
import { BlockNoteView } from '@blocknote/mantine'
import '@blocknote/mantine/style.css'
import type { Block } from '@blocknote/core'

type SaveStatus = 'saved' | 'saving' | 'unsaved' | 'error' | 'conflict'
type Props = {
  pageId: string
  initialContent: string
  theme: 'light' | 'dark'
  reloadToken: number
  discardRef?: { current: boolean }
  onSave: (pageId: string, content: string) => Promise<void>
  onContentChange?: (content: string) => void
  onReload: () => Promise<void>
  onRetry: (content: string) => Promise<void>
  onStatus: (s: SaveStatus) => void
}

function parseBlocks(content: string): Block[] | undefined {
  if (!content) return undefined
  try {
    const parsed = JSON.parse(content)
    if (Array.isArray(parsed)) return parsed as Block[]
  } catch {}
  return [{ type: 'paragraph', content: [{ type: 'text', text: content, styles: {} }] } as Block]
}

export default function PageEditor({ pageId, initialContent, theme, reloadToken, discardRef, onSave, onContentChange, onReload, onRetry, onStatus }: Props) {
  const blocks = useMemo(() => parseBlocks(initialContent), [initialContent])
  const editor = useCreateBlockNote({ initialContent: blocks })
  const latestReq = useRef(0)
  const pendingContent = useRef<string | null>(null)
  const timer = useRef<any>(null)
  const retryTimer = useRef<any>(null)
  const onSaveRef = useRef(onSave)
  const doSaveRef = useRef<(c: string) => Promise<void>>(async () => {})
  const [status, setStatus] = useState<SaveStatus>('saved')
  const isDiscarded = useRef(false)

  const updateStatus = useCallback((s: SaveStatus) => {
    setStatus(s)
    onStatus(s)
  }, [onStatus])

  useEffect(() => {
    isDiscarded.current = true
    if (timer.current) clearTimeout(timer.current)
    if (retryTimer.current) clearTimeout(retryTimer.current)
    pendingContent.current = null
    const t = setTimeout(() => { isDiscarded.current = false }, 0)
    return () => clearTimeout(t)
  }, [reloadToken])

  useEffect(() => { onSaveRef.current = onSave }, [onSave])

  const doSave = useCallback(async (content: string) => {
    if (isDiscarded.current) return
    const reqId = ++latestReq.current
    updateStatus('saving')
    try {
      await onSaveRef.current(pageId, content)
      if (isDiscarded.current) return
      if (reqId === latestReq.current) {
        updateStatus('saved')
        pendingContent.current = null
      }
    } catch (e) {
      if (isDiscarded.current) return
      if (reqId === latestReq.current) {
        if ((e as Error).message.includes('version conflict')) {
          updateStatus('conflict')
        } else {
          updateStatus('error')
          pendingContent.current = content
          if (retryTimer.current) clearTimeout(retryTimer.current)
          retryTimer.current = setTimeout(() => {
            if (!isDiscarded.current && pendingContent.current === content) doSaveRef.current(content)
          }, 3000)
        }
      }
    }
  }, [updateStatus, pageId])

  const scheduleSave = useCallback((content: string) => {
    if (isDiscarded.current) return
    pendingContent.current = content
    onContentChange?.(content)
    updateStatus('unsaved')
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      doSave(content)
    }, 1000)
  }, [doSave, updateStatus, onContentChange])

  const scheduleSaveRef = useRef(scheduleSave)
  useEffect(() => {
    scheduleSaveRef.current = scheduleSave
    doSaveRef.current = doSave
  }, [scheduleSave, doSave])

  useEffect(() => {
    const unsub = editor.onChange(() => {
      const json = JSON.stringify(editor.document)
      scheduleSaveRef.current(json)
    })
    return () => { unsub() }
  }, [editor, pageId])

  useEffect(() => {
    return () => {
      isDiscarded.current = true
      if (timer.current) { clearTimeout(timer.current); timer.current = null }
      if (retryTimer.current) { clearTimeout(retryTimer.current); retryTimer.current = null }
      const content = pendingContent.current
      pendingContent.current = null
      const discarding = discardRef ? discardRef.current : false
      if (content && !discarding) {
        try { onSaveRef.current(pageId, content).catch(() => {}) } catch {}
      }
    }
  }, [])

  useEffect(() => {
    const handler = () => {
      if (pendingContent.current && !isDiscarded.current) {
        try { onSaveRef.current(pageId, pendingContent.current) } catch {}
      }
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [pageId])

  return (
    <div>
      <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 8 }}>
        Status: {status === 'error' ? 'Save failed (retrying…)' : status === 'conflict' ? 'Version conflict' : status}
      </div>
      {status === 'conflict' && (
        <div className="row" style={{ marginBottom: 8, gap: 8 }}>
          <button className="btn" onClick={async () => { await onReload(); }}>Reload latest</button>
          <button className="btn primary" onClick={() => { const c = JSON.stringify(editor.document); onRetry(c).then(() => { updateStatus('saved'); pendingContent.current = null }).catch(() => updateStatus('error')) }}>Retry my changes</button>
        </div>
      )}
      <BlockNoteView editor={editor} theme={theme} />
    </div>
  )
}
