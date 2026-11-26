import type { Block } from '@blocknote/core'

// Self-contained Markdown <-> BlockNote block converters. They intentionally
// cover the common block types and degrade gracefully so import/export works
// regardless of BlockNote version-specific helpers.

type InlineContent = any

function txt(text: string): InlineContent[] {
  return [{ type: 'text', text, styles: {} }]
}

function inlineToMd(content: InlineContent): string {
  if (!content) return ''
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .map((c: any) => {
      if (c.type === 'link') return `[${inlineToMd(c.content)}](${c.href})`
      let t: string = c.text ?? ''
      if (c.styles?.bold) t = `**${t}**`
      if (c.styles?.italic) t = `*${t}*`
      if (c.styles?.strike) t = `~~${t}~~`
      if (c.styles?.code) t = `\`${t}\``
      return t
    })
    .join('')
}

export function blocksToMarkdown(blocks: Block[]): string {
  const lines: string[] = []
  const walk = (list: Block[], depth: number) => {
    for (const b of list) {
      const indent = '  '.repeat(depth)
      const props = (b as any).props ?? {}
      switch (b.type) {
        case 'heading': {
          const level = Math.min(6, Math.max(1, props.level ?? 1))
          lines.push(`${'#'.repeat(level)} ${inlineToMd((b as any).content)}`)
          break
        }
        case 'bulletListItem':
          lines.push(`${indent}- ${inlineToMd((b as any).content)}`)
          break
        case 'numberedListItem':
          lines.push(`${indent}1. ${inlineToMd((b as any).content)}`)
          break
        case 'checkListItem':
          lines.push(`${indent}- [${props.checked ? 'x' : ' '}] ${inlineToMd((b as any).content)}`)
          break
        case 'codeBlock': {
          lines.push('```' + (props.language || ''))
          lines.push(typeof (b as any).content === 'string' ? (b as any).content : inlineToMd((b as any).content))
          lines.push('```')
          break
        }
        case 'quote':
          lines.push(`> ${inlineToMd((b as any).content)}`)
          break
        case 'divider':
          lines.push('---')
          break
        default:
          lines.push(`${indent}${inlineToMd((b as any).content)}`)
      }
      const children = (b as any).children as Block[] | undefined
      if (children && children.length) walk(children, depth + 1)
    }
  }
  walk(blocks || [], 0)
  return lines.join('\n')
}

export function markdownToBlocks(md: string): Block[] {
  const blocks: any[] = []
  const lines = md.replace(/\r\n/g, '\n').split('\n')
  let i = 0
  let para: string[] = []
  const flush = () => {
    if (para.length) {
      blocks.push({ type: 'paragraph', content: txt(para.join(' ')) })
      para = []
    }
  }
  while (i < lines.length) {
    const line = lines[i]
    if (/^```/.test(line)) {
      flush()
      const lang = line.slice(3).trim()
      const code: string[] = []
      i++
      while (i < lines.length && !/^```/.test(lines[i])) {
        code.push(lines[i])
        i++
      }
      blocks.push({ type: 'codeBlock', props: { language: lang }, content: code.join('\n') })
      i++
      continue
    }
    const h = /^(#{1,6})\s+(.*)$/.exec(line)
    if (h) {
      flush()
      blocks.push({ type: 'heading', props: { level: h[1].length }, content: txt(h[2]) })
      i++
      continue
    }
    if (/^\s*[-*]\s+\[[ xX]\]\s+/.test(line)) {
      flush()
      const checked = /\[[xX]\]/.test(line)
      blocks.push({ type: 'checkListItem', props: { checked }, content: txt(line.replace(/^\s*[-*]\s+\[[ xX]\]\s+/, '')) })
      i++
      continue
    }
    const bullet = /^\s*[-*]\s+(.*)$/.exec(line)
    if (bullet) {
      flush()
      blocks.push({ type: 'bulletListItem', content: txt(bullet[1]) })
      i++
      continue
    }
    const num = /^\s*\d+\.\s+(.*)$/.exec(line)
    if (num) {
      flush()
      blocks.push({ type: 'numberedListItem', content: txt(num[1]) })
      i++
      continue
    }
    if (/^>\s?/.test(line)) {
      flush()
      blocks.push({ type: 'quote', content: txt(line.replace(/^>\s?/, '')) })
      i++
      continue
    }
    if (/^-{3,}$/.test(line.trim())) {
      flush()
      blocks.push({ type: 'divider', content: undefined })
      i++
      continue
    }
    if (line.trim() === '') {
      flush()
      i++
      continue
    }
    para.push(line)
    i++
  }
  flush()
  if (!blocks.length) blocks.push({ type: 'paragraph', content: txt('') })
  return blocks as Block[]
}