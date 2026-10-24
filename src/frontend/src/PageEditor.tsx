import { useEffect, useMemo, useRef, useState } from 'react'
import { useCreateBlockNote } from '@blocknote/react'
import { BlockNoteView } from '@blocknote/mantine'
import '@blocknote/mantine/style.css'
import type { Block } from '@blocknote/core'

type Props = {
  initialContent: string
  theme: 'light' | 'dark'
  onChange: (json: string) => void
  onSaveStatus: (s: 'saved' | 'saving' | 'unsaved') => void
}

function parseBlocks(content: string): Block[] | undefined {
  if (!content) return undefined
  try {
    const parsed = JSON.parse(content)
    if (Array.isArray(parsed)) return parsed as Block[]
  } catch {}
  return [{ type: 'paragraph', content: [{ type: 'text', text: content, styles: {} }] } as Block]
}

export default function PageEditor({ initialContent, theme, onChange, onSaveStatus }: Props) {
  const blocks = useMemo(() => parseBlocks(initialContent), [initialContent])
  const editor = useCreateBlockNote({ initialContent: blocks })
  const timer = useRef<any>(null)
  const [status, setStatus] = useState<'saved' | 'saving' | 'unsaved'>('saved')

  useEffect(() => {
    const unsub = editor.onChange(() => {
      setStatus('unsaved')
      onSaveStatus('unsaved')
      if (timer.current) clearTimeout(timer.current)
      timer.current = setTimeout(() => {
        setStatus('saving')
        onSaveStatus('saving')
        try {
          const json = JSON.stringify(editor.document)
          onChange(json)
          setStatus('saved')
          onSaveStatus('saved')
        } catch (e) {
          console.error('save err', e)
          setStatus('unsaved')
          onSaveStatus('unsaved')
        }
      }, 1000)
    })
    return () => { unsub(); if (timer.current) clearTimeout(timer.current) }
  }, [editor, onChange, onSaveStatus])

  return (
    <div>
      <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 8 }}>Status: {status}</div>
      <BlockNoteView editor={editor} theme={theme} />
    </div>
  )
}
