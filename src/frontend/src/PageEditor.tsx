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

export default function PageEditor({ pageId, initialContent, theme, reloadToken, onSave, onContentChange, onReload, onRetry, onStatus }: Props) {
  const blocks = useMemo(() => parseBlocks(initialContent), [initialContent])
  const editor = useCreateBlockNote({ initialContent: blocks })
  const latestReq = useRef(0)
  const pendingContent = useRef<string | null>(null)
  const timer = useRef<any>(null)
  const onSaveRef = useRef(onSave)
  const doSaveRef = useRef<(c: string) => Promise<void>>(async () => {})
  const [status, setStatus] = useState<SaveStatus>('saved')
  // isDiscarded 标记当前编辑器实例是否已废弃。
  // reloadToken 变化（Reload latest / Retry 导致重挂载）或组件卸载时置为 true，
  // 旧实例的所有异步写入（定时器、重试、排队 flush）均不再执行，彻底避免旧草稿回写。
  const isDiscarded = useRef(false)
  const instanceRef = useRef(reloadToken)

  const updateStatus = useCallback((s: SaveStatus) => {
    setStatus(s)
    onStatus(s)
  }, [onStatus])

  // reloadToken 变化：旧实例废弃，清空定时器和未提交草稿，不再写入。
  useEffect(() => {
    isDiscarded.current = true
    instanceRef.current = reloadToken
    if (timer.current) clearTimeout(timer.current)
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
          setTimeout(() => {
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

  // 卸载时永不 flush 未提交草稿：直接丢弃定时器与 pendingContent，
  // 确保旧实例卸载不会重新覆盖新实例刚加载的服务器正文。
  useEffect(() => {
    return () => {
      isDiscarded.current = true
      if (timer.current) clearTimeout(timer.current)
      pendingContent.current = null
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
