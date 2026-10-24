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
  onSave: (pageId: string, content: string) => Promise<void>
  onContentChange?: (content: string) => void
  onReload: () => Promise<void>
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

export default function PageEditor({ pageId, initialContent, theme, onSave, onContentChange, onReload, onStatus }: Props) {
  const blocks = useMemo(() => parseBlocks(initialContent), [initialContent])
  const editor = useCreateBlockNote({ initialContent: blocks })
  const latestReq = useRef(0)
  const pendingContent = useRef<string | null>(null)
  const timer = useRef<any>(null)
  const onSaveRef = useRef(onSave)
  const doSaveRef = useRef<(c: string) => Promise<void>>(async () => {})
  const [status, setStatus] = useState<SaveStatus>('saved')

  useEffect(() => { onSaveRef.current = onSave }, [onSave])

  const updateStatus = useCallback((s: SaveStatus) => {
    setStatus(s)
    onStatus(s)
  }, [onStatus])

  const doSave = useCallback(async (content: string) => {
    const reqId = ++latestReq.current
    updateStatus('saving')
    try {
      await onSaveRef.current(pageId, content)
      if (reqId === latestReq.current) {
        updateStatus('saved')
        pendingContent.current = null
      }
    } catch (e) {
      if (reqId === latestReq.current) {
        if ((e as Error).message.includes('version conflict')) {
          updateStatus('conflict')
        } else {
          updateStatus('error')
          pendingContent.current = content
          setTimeout(() => {
            if (pendingContent.current === content) doSaveRef.current(content)
          }, 3000)
        }
      }
    }
  }, [updateStatus])

  const scheduleSave = useCallback((content: string) => {
    pendingContent.current = content
    onContentChange?.(content)
    updateStatus('unsaved')
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      doSave(content)
    }, 1000)
  }, [doSave, updateStatus])

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
  }, [editor])

  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current)
      if (pendingContent.current) {
        doSaveRef.current(pendingContent.current)
      }
    }
  }, [])

  useEffect(() => {
    const handler = () => {
      if (pendingContent.current) {
        try { onSaveRef.current(pageId, pendingContent.current) } catch {}
      }
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [])

  return (
    <div>
      <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 8 }}>
        Status: {status === 'error' ? 'Save failed (retrying…)' : status === 'conflict' ? 'Version conflict' : status}
      </div>
      {status === 'conflict' && (
        <div className="row" style={{ marginBottom: 8, gap: 8 }}>
          <button className="btn" onClick={async () => { await onReload(); }}>Reload latest</button>
          <button className="btn primary" onClick={() => { pendingContent.current = JSON.stringify(editor.document); doSave(JSON.stringify(editor.document)) }}>Retry my changes</button>
        </div>
      )}
      <BlockNoteView editor={editor} theme={theme} />
    </div>
  )
}
