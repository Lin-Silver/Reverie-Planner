import { useLayoutEffect, useRef } from 'react'
import type { ReactNode } from 'react'

// Native dialog supplies inert background, focus containment and Escape handling.
export default function Dialog({ onClose, label, children, instant = false, navigation = false, returnFocus }: { onClose: () => void; label: string; children: ReactNode; instant?: boolean; navigation?: boolean; returnFocus?: HTMLElement | null }) {
 const ref = useRef<HTMLDialogElement>(null)
 const closeRef = useRef(onClose)
 useLayoutEffect(() => { closeRef.current = onClose }, [onClose])
 useLayoutEffect(() => {
  const previous = returnFocus || document.activeElement as HTMLElement | null
  const dialog = ref.current!
  dialog.showModal()
  return () => { dialog.close(); previous?.focus() }
 }, [returnFocus])
 return <dialog className={`dialog-shell ${navigation ? 'navigation-dialog' : ''}`} data-instant={instant || undefined} ref={ref} aria-label={label} onCancel={e => { e.preventDefault(); closeRef.current() }} onClick={e => { if (e.target === e.currentTarget) closeRef.current() }}>{children}</dialog>
}
