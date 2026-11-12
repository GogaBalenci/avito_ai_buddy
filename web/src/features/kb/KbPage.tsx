import { useQuery } from '@tanstack/react-query'
import { Link, useParams, useRouter } from '@tanstack/react-router'
import Markdown, { type Components } from 'react-markdown'
import { api, ApiError, errorText } from '@/shared/api/client'
import { KbDocSchema } from '@/shared/api/schemas'
import { Button } from '@/shared/ui/Button'
import { Chip, Plaque } from '@/shared/ui/Chip'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'
import { Vitya } from '@/shared/mascot/Vitya'
import { sourceLabel } from '@/features/chat/replyLogic'

/**
 * Санитайзер ссылок (12 §10.2): только http, https и относительные пути.
 * javascript:, data:, vbscript: и прочие схемы превращаются в пустую ссылку.
 */
export function safeUrl(url: string): string {
  const u = url.trim()
  if (/^https?:\/\//i.test(u)) return u
  if (u.startsWith('/') && !u.startsWith('//')) return u
  if (u.startsWith('#')) return u
  if (!/^[a-z][a-z0-9+.-]*:/i.test(u) && !u.startsWith('//')) return u
  return ''
}

const components: Components = {
  a: ({ href, children }) => {
    const url = href ?? ''
    const kb = /^\/kb\/([a-z0-9-]+)$/.exec(url)
    if (kb) {
      return (
        <Link to="/kb/$slug" params={{ slug: kb[1] }}>
          {children}
        </Link>
      )
    }
    if (!url) return <span className="link-disabled">{children}</span>
    return (
      <a href={url} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
  // картинок в базе знаний нет; внешние картинки не грузим (CSP img-src 'self')
  img: ({ alt }) => <span className="subtle">[изображение: {alt}]</span>,
}

/** Markdown без сырого HTML: react-markdown без rehype-raw экранирует теги (AG-26, KF-06). */
export function SafeMarkdown({ text }: { text: string }) {
  return (
    <div className="md">
      <Markdown urlTransform={safeUrl} components={components} skipHtml>
        {text}
      </Markdown>
    </div>
  )
}

const KIND: Record<string, string> = { process: 'Процесс', term: 'Термин', tool: 'Инструмент', guide: 'Руководство', org: 'Оргструктура' }

/** Документ базы знаний /kb/$slug (11 §2.11): 200 / 403 (заголовок без тела) / 404. */
export function KbPage() {
  const { slug } = useParams({ strict: false }) as { slug: string }
  const router = useRouter()
  const q = useQuery({
    queryKey: ['kb', slug],
    queryFn: ({ signal }) => api(`/api/kb/${encodeURIComponent(slug)}`, { schema: KbDocSchema, signal }),
  })
  const err = q.error instanceof ApiError ? q.error : null

  return (
    <main className="page kb">
      <Button variant="link" onClick={() => router.history.back()}>
        ← Назад
      </Button>
      {q.isPending && <Skeleton lines={8} label="Загружаю документ" />}
      {err?.status === 403 && (
        <div className="path__empty card">
          <Vitya state="ask_mentor" size={64} />
          <h1>{err.body?.title ?? 'Документ с ограниченным доступом'}</h1>
          <p>Этот документ доступен не всем ролям. Если он нужен для работы — спроси наставника.</p>
        </div>
      )}
      {err?.status === 404 && (
        <div className="path__empty card">
          <Vitya state="oops" size={64} />
          <h1>Документа нет</h1>
          <p>Такого документа в базе знаний нет. Спроси Витю в чате — подскажу, где искать.</p>
          <Link to="/">В чат</Link>
        </div>
      )}
      {q.isError && err?.status !== 403 && err?.status !== 404 && <ErrorState text={`Не получилось загрузить документ. ${errorText(q.error)}`} onRetry={() => void q.refetch()} />}
      {q.data && (
        <article className="card kb__doc">
          <div className="row">
            <Chip>{KIND[q.data.kind] ?? q.data.kind}</Chip>
            <Chip tone={q.data.label === 'test_description' ? 'warning' : 'test'}>{sourceLabel(q.data)}</Chip>
            {q.data.dated && <span className="subtle">{q.data.dated}</span>}
          </div>
          <h1>{q.data.title}</h1>
          <Plaque tone={q.data.label === 'test_description' ? 'warning' : 'test'}>
            {q.data.label === 'test_description'
              ? 'Тестовое описание для прототипа: в открытых источниках этого нет — уточни у наставника.'
              : 'Тестовая инструкция для прототипа, а не регламент Авито.'}
          </Plaque>
          <p className="kb__summary">{q.data.summary}</p>
          <SafeMarkdown text={q.data.body_md} />
          {q.data.conflicts_with.length > 0 && (
            <Plaque tone="warning">
              Эта инструкция расходится с другой:{' '}
              {q.data.conflicts_with.map((s) => (
                <Link key={s} to="/kb/$slug" params={{ slug: s }}>
                  {s}
                </Link>
              ))}
              . Уточни у наставника, какая актуальна.
            </Plaque>
          )}
        </article>
      )}
    </main>
  )
}
