import { Link } from '@tanstack/react-router'
import type { Source } from '@/shared/api/schemas'

/**
 * Текст ответа бадди — только текст (AG-26): никакого HTML, разрешены лишь маркеры [n],
 * которые становятся ссылками на источник с тем же номером.
 */
export function splitMarkers(text: string, sources: Source[]): ({ kind: 'text'; value: string } | { kind: 'ref'; source: Source })[] {
  const out: ({ kind: 'text'; value: string } | { kind: 'ref'; source: Source })[] = []
  const re = /\[(\d+)\]/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) {
    const src = sources.find((s) => s.n === Number(m![1]))
    if (!src) continue
    if (m.index > last) out.push({ kind: 'text', value: text.slice(last, m.index) })
    out.push({ kind: 'ref', source: src })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ kind: 'text', value: text.slice(last) })
  return out
}

/** Внутренняя ссылка базы знаний: только /kb/<slug>. */
export function kbSlug(url: string): string | null {
  const m = /^\/kb\/([a-z0-9-]+)$/.exec(url)
  return m ? m[1] : null
}

export function ReplyText({ text, sources }: { text: string; sources: Source[] }) {
  return (
    <p className="reply__text">
      {splitMarkers(text, sources).map((part, i) => {
        if (part.kind === 'text') return <span key={i}>{part.value}</span>
        const slug = kbSlug(part.source.url)
        return slug ? (
          <Link key={i} to="/kb/$slug" params={{ slug }} className="marker" title={part.source.title}>
            [{part.source.n}]
          </Link>
        ) : (
          <span key={i}>[{part.source.n}]</span>
        )
      })}
    </p>
  )
}
