import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, errorText } from '@/shared/api/client'
import { configQuery } from '@/shared/api/queries'
import { RequestListSchema, RequestViewSchema, type RequestList, type RequestView } from '@/shared/api/schemas'
import { dayTime, hhmm } from '@/shared/lib/dates'
import { newId } from '@/shared/lib/uuid'
import { Button } from '@/shared/ui/Button'
import { Chip } from '@/shared/ui/Chip'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'
import { toast } from '@/shared/ui/toastStore'
import { Vitya } from '@/shared/mascot/Vitya'
import { LEVEL } from '@/features/actions/PendingActionCard'

export const INBOX_KEY = ['mentor', 'inbox'] as const

export const REJECT_OPTIONS: { value: 'not_needed' | 'other_resource' | 'discuss_1on1'; label: string }[] = [
  { value: 'not_needed', label: 'Доступ сейчас не нужен' },
  { value: 'other_resource', label: 'Нужен другой ресурс' },
  { value: 'discuss_1on1', label: 'Обсудим на 1:1' },
]

/** Входящие заявки своих стажёров; опрос — пока вкладка активна (11 §3.6). */
export function useMentorInbox() {
  const config = useQuery(configQuery)
  return useQuery({
    queryKey: INBOX_KEY,
    queryFn: ({ signal }) => api('/api/mentor/access-requests?status=pending', { schema: RequestListSchema, signal }),
    refetchInterval: config.data?.poll_interval_ms ?? 10_000,
    refetchIntervalInBackground: false,
  })
}

type Decision = { request: RequestView; decision: 'approve' | 'reject'; reject_reason?: string; key: string }

/** Решение наставника: карточка уходит из списка сразу и возвращается при ошибке (XF-05). */
export function useDecide() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (d: Decision) =>
      api(`/api/mentor/access-requests/${encodeURIComponent(d.request.request_id)}/decision`, {
        body: { decision: d.decision, idempotency_key: d.key, ...(d.decision === 'reject' ? { reject_reason: d.reject_reason } : {}) },
        schema: RequestViewSchema,
      }),
    onMutate: async (d) => {
      await qc.cancelQueries({ queryKey: INBOX_KEY })
      const snapshot = qc.getQueryData<RequestList>(INBOX_KEY)
      if (snapshot) qc.setQueryData<RequestList>(INBOX_KEY, { ...snapshot, items: snapshot.items.filter((r) => r.request_id !== d.request.request_id), total: Math.max(0, snapshot.total - 1) })
      return { snapshot }
    },
    onSuccess: (_r, d) => {
      toast(d.decision === 'approve' ? `Одобрено: «${d.request.resource_title ?? d.request.resource_id}»` : 'Заявка отклонена', { tone: 'success' })
      void qc.invalidateQueries({ queryKey: ['mentor', 'interns'] })
    },
    onError: (e, _d, ctx) => {
      if (e instanceof ApiError && e.code === 'ALREADY_DECIDED') {
        toast('Уже решено', { tone: 'info' })
        void qc.invalidateQueries({ queryKey: INBOX_KEY })
        return
      }
      if (ctx?.snapshot) qc.setQueryData(INBOX_KEY, ctx.snapshot)
      toast(errorText(e), { tone: 'error' })
    },
  })
}

function InboxCard({ r, timeZone, onDecide, busy }: { r: RequestView; timeZone?: string; onDecide: (d: Omit<Decision, 'key'>) => void; busy: boolean }) {
  const [rejecting, setRejecting] = useState(false)
  const [reason, setReason] = useState<string | null>(null)
  return (
    <li className="card inbox__item" aria-label={`Заявка ${r.request_id}`}>
      <div className="row">
        <strong>{r.employee?.display_name ?? 'Стажёр'}</strong>
        <Chip tone="warning">Ждёт решения</Chip>
      </div>
      <p className="inbox__res">
        {r.resource_title ?? r.resource_id}
        {r.access_level ? ` · ${LEVEL[r.access_level]}` : ''}
      </p>
      <p className="subtle">
        {r.step_title ? `Шаг «${r.step_title}» · ` : ''}создана {dayTime(r.created_at, timeZone)} · №{r.request_id}
      </p>
      {r.justification && <p className="subtle">Обоснование: {r.justification}</p>}
      {r.auto_approve_at && <p className="subtle">Одобрится автоматически около {hhmm(r.auto_approve_at, timeZone)}</p>}
      {!rejecting ? (
        <div className="row">
          <Button variant="primary" disabled={busy} onClick={() => onDecide({ request: r, decision: 'approve' })}>
            Одобрить
          </Button>
          <Button disabled={busy} onClick={() => setRejecting(true)}>
            Отклонить
          </Button>
        </div>
      ) : (
        <div className="stack">
          <div className="row" role="radiogroup" aria-label="Причина отказа">
            {REJECT_OPTIONS.map((o) => (
              <Button key={o.value} size="s" variant={reason === o.value ? 'primary' : 'ghost'} role="radio" aria-checked={reason === o.value} onClick={() => setReason(o.value)}>
                {o.label}
              </Button>
            ))}
          </div>
          <div className="row">
            <Button variant="primary" disabled={!reason || busy} onClick={() => reason && onDecide({ request: r, decision: 'reject', reject_reason: reason })}>
              Отклонить заявку
            </Button>
            <Button variant="ghost" onClick={() => setRejecting(false)}>
              Назад
            </Button>
          </div>
        </div>
      )}
    </li>
  )
}

/** «Входящие» наставника (11 §3.7, шаг 7). */
export function InboxPage() {
  const q = useMentorInbox()
  const config = useQuery(configQuery)
  const decide = useDecide()
  return (
    <main className="page">
      <h1>Входящие</h1>
      <p className="muted">Заявки твоих стажёров на доступы. Тестовый сервис доступов: решение сразу видно в пути стажёра.</p>
      {q.isPending && <Skeleton lines={4} label="Загружаю входящие" />}
      {q.isError && !q.data && <ErrorState text={`Не получилось загрузить заявки. ${errorText(q.error)}`} onRetry={() => void q.refetch()} />}
      {q.data?.stale && <p className="subtle">Сервис доступов не ответил — показаны сохранённые данные</p>}
      {q.data && q.data.items.length === 0 && (
        <div className="path__empty">
          <Vitya state="hello" size={64} />
          <p>Входящих заявок нет</p>
        </div>
      )}
      {q.data && q.data.items.length > 0 && (
        <ul className="inbox">
          {q.data.items.map((r) => (
            <InboxCard key={r.request_id} r={r} timeZone={config.data?.timezone} busy={decide.isPending} onDecide={(d) => decide.mutate({ ...d, key: newId() })} />
          ))}
        </ul>
      )}
    </main>
  )
}
