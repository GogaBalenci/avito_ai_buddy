import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { api, errorText } from '@/shared/api/client'
import { configQuery } from '@/shared/api/queries'
import { RequestListSchema, type RequestView } from '@/shared/api/schemas'
import { dayTime, hhmm, rangeText } from '@/shared/lib/dates'
import { Button } from '@/shared/ui/Button'
import { Chip, type ChipTone } from '@/shared/ui/Chip'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'
import { Vitya } from '@/shared/mascot/Vitya'
import { sendPayload, useChat } from '@/features/chat/chatStore'
import { LEVEL } from '@/features/actions/PendingActionCard'
import { REJECT_REASON } from '@/features/path/logic'
import { workingDaysText } from '@/features/time-off/rules'

export type RequestFilters = { kind?: 'time_off' | 'access'; scope?: 'upcoming' | 'all' }

export function useRequests(filters: RequestFilters) {
  const qs = new URLSearchParams()
  if (filters.kind) qs.set('kind', filters.kind)
  if (filters.scope) qs.set('scope', filters.scope)
  return useQuery({
    queryKey: ['requests', filters],
    queryFn: ({ signal }) => api(`/api/requests${qs.size ? `?${qs}` : ''}`, { schema: RequestListSchema, signal }),
  })
}

export function requestStatus(r: RequestView, timeZone?: string): { text: string; tone: ChipTone } {
  switch (r.status) {
    case 'registered':
      return { text: 'Оформлена', tone: 'success' }
    case 'cancelled':
      return { text: 'Отменена', tone: 'neutral' }
    case 'approved':
      return { text: 'Одобрена', tone: 'success' }
    case 'rejected':
      return { text: `Отклонена${r.reject_reason ? `: ${REJECT_REASON[r.reject_reason]}` : ''}`, tone: 'danger' }
    default:
      return { text: r.auto_approve_at ? `Одобрится около ${hhmm(r.auto_approve_at, timeZone)}` : 'Ждёт наставника', tone: 'warning' }
  }
}

export function requestTitle(r: RequestView): string {
  if (r.kind === 'time_off') return `Отгул ${rangeText(r.date_from, r.date_to)}${r.working_days ? ` · ${workingDaysText(r.working_days)}` : ''}`
  return `Доступ: ${r.resource_title ?? r.resource_id}${r.access_level ? ` · ${LEVEL[r.access_level]}` : ''}`
}

export function RequestItem({ r, timeZone }: { r: RequestView; timeZone?: string }) {
  const st = requestStatus(r, timeZone)
  return (
    <div className="request">
      <div className="request__main">
        <span className="request__title">{requestTitle(r)}</span>
        <span className="subtle">
          №{r.request_id}
          {r.step_title ? ` · шаг «${r.step_title}»` : ''}
        </span>
      </div>
      <Chip tone={st.tone}>{st.text}</Chip>
    </div>
  )
}

/** Блок «Мои заявки» в панели «Мой путь» (10 §12.2): ближайшие заявки + «Оформить отгул». */
export function MyRequestsBlock() {
  const q = useRequests({ scope: 'upcoming' })
  const config = useQuery(configQuery)
  const busy = useChat((s) => s.inFlight !== null)
  return (
    <section className="myreq" aria-labelledby="myreq-title">
      <div className="panel-head">
        <h3 id="myreq-title">Мои заявки</h3>
        <Link to="/requests" className="subtle">
          Все заявки
        </Link>
      </div>
      {q.isPending && <Skeleton lines={2} label="Загружаю заявки" />}
      {q.isError && <ErrorState text={errorText(q.error)} onRetry={() => void q.refetch()} />}
      {q.data?.stale && <p className="subtle">Сервис недоступен — показаны сохранённые данные</p>}
      {q.data && q.data.items.length === 0 && <p className="subtle">Активных заявок нет</p>}
      {q.data && q.data.items.length > 0 && (
        <ul className="requests">
          {q.data.items.slice(0, 4).map((r) => (
            <li key={r.request_id}>
              <RequestItem r={r} timeZone={config.data?.timezone} />
            </li>
          ))}
        </ul>
      )}
      <Button size="s" disabled={busy} onClick={() => void sendPayload('intent:time_off', 'Оформить отгул')}>
        Оформить отгул
      </Button>
    </section>
  )
}

const KINDS: { value: RequestFilters['kind']; label: string }[] = [
  { value: undefined, label: 'Все' },
  { value: 'time_off', label: 'Отгулы' },
  { value: 'access', label: 'Доступы' },
]

/** Экран /requests — история заявок из нашей БД (10 §12.3). */
export function RequestsPage() {
  const [kind, setKind] = useState<RequestFilters['kind']>(undefined)
  const q = useRequests({ kind, scope: 'all' })
  const config = useQuery(configQuery)
  return (
    <main className="page">
      <Link to="/">← Мой путь и чат</Link>
      <h1>Мои заявки</h1>
      <div className="row" role="group" aria-label="Тип заявок">
        {KINDS.map((k) => (
          <Button key={k.label} size="s" variant={kind === k.value ? 'primary' : 'ghost'} aria-pressed={kind === k.value} onClick={() => setKind(k.value)}>
            {k.label}
          </Button>
        ))}
      </div>
      {q.isPending && <Skeleton lines={4} label="Загружаю заявки" />}
      {q.isError && <ErrorState text={`Не получилось загрузить заявки. ${errorText(q.error)}`} onRetry={() => void q.refetch()} />}
      {q.data?.stale && <p className="subtle">Сервис недоступен — показаны сохранённые данные</p>}
      {q.data && q.data.items.length === 0 && (
        <div className="path__empty">
          <Vitya state="hello" size={64} />
          <p>Заявок пока нет. Оформить отгул или запросить доступ можно в чате.</p>
          <Link to="/" className="btn btn--primary btn--m">
            В чат
          </Link>
        </div>
      )}
      {q.data && q.data.items.length > 0 && (
        <ul className="requests card">
          {q.data.items.map((r) => (
            <li key={r.request_id} className="request--full">
              <RequestItem r={r} timeZone={config.data?.timezone} />
              <span className="subtle">создана {dayTime(r.created_at, config.data?.timezone)}</span>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}
