import { useEffect, useMemo, useRef, useState } from 'react'
import { useCreateBlockNote } from '@blocknote/react'
import { BlockNoteView } from '@blocknote/mantine'
import '@blocknote/mantine/style.css'
import type { Block } from '@blocknote/core'

type SaveStatus = 'saved' | 'saving' | 'unsaved' | 'error'
type Props = {
  initialContent: string
  theme: 'light' | 'dark'
  onSave: (content: string) => Promise<void>
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

export default function PageEditor({ initialContent, theme, onSave, onStatus }: Props) {
  const blocks = useMemo(() => parseBlocks(initialContent), [initialContent])
  const editor = useCreateBlockNote({ initialContent: blocks })
  const timer = useRef<any>(null)
  const pendingContent = useRef<string | null>(null)
  const latestReq = useRef(0)
  const [status, setStatus] = useState<SaveStatus>('saved')

  const updateStatus = (s: SaveStatus) => {
    setStatus(s)
    onStatus(s)
  }

  const doSave = async (content: string) => {
    const reqId = ++latestReq.current
    updateStatus('saving')
    try {
      await onSave(content)
      if (reqId === latestReq.current) {
        updateStatus('saved')
        pendingContent.current = null
      }
    } catch (e) {
      if (reqId === latestReq.current) {
        updateStatus('error')
        pendingContent.current = content
        // retry after 3s
        setTimeout(() => {
          if (pendingContent.current === content) doSave(content)
        }, 3000)
      }
    }
  }

  const scheduleSave = (content: string) => {
    pendingContent.current = content
    updateStatus('unsaved')
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      doSave(content)
    }, 1000)
  }

  useEffect(() => {
    const unsub = editor.onChange(() => {
      const json = JSON.stringify(editor.document)
      scheduleSave(json)
    })
    return () => { unsub() }
  }, [editor])

  // flush pending on unmount
  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current)
      if (pendingContent.current) {
        doSave(pendingContent.current)
      }
    }
  }, [])

  // flush on beforeunload
  useEffect(() => {
    const handler = () => {
      if (pendingContent.current) {
        // best-effort sync
        try { onSave(pendingContent.current) } catch {}
      }
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [onSave])

  return (
    <div>
      <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 8 }}>
        Status: {status === 'error' ? 'Save failed (retrying…)' : status}
      </div>
      <BlockNoteView editor={editor} theme={theme} />
    </div>
  )
}
