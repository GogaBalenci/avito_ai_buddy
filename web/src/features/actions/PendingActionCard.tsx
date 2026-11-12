import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ApiError, errorText } from '@/shared/api/client'
import type { ActionView, PendingAction } from '@/shared/api/schemas'
import { hhmm, rangeText } from '@/shared/lib/dates'
import { useMascot } from '@/shared/mascot/mascotStore'
import { Button } from '@/shared/ui/Button'
import { Chip, Plaque } from '@/shared/ui/Chip'
import { useChat } from '@/features/chat/chatStore'
import { workingDaysText } from '@/features/time-off/rules'
import { cancelAction, confirmAction, getAction, invalidateAfterRequest } from './actionsApi'

export const LEVEL: Record<string, string> = { read: 'чтение', write: 'запись', admin: 'администрирование' }

export function policyHint(policy?: string, minutes?: number | null): string {
  if (policy === 'auto') return 'Одобряется сразу'
  if (policy === 'mentor') return 'Заявку одобряет наставник'
  if (policy === 'timer') return `Одобрится автоматически примерно через ${minutes ?? 2} мин`
  return ''
}

const STATE_TEXT: Record<string, string> = {
  cancelled: 'Черновик отменён — ничего не отправлено',
  replaced: 'Черновик заменён новым',
  expired: 'Черновик устарел — запроси ещё раз',
}

/**
 * Черновик действия из ответа чата (S4) или из истории: состояние берётся из
 * GET /api/actions/{id} (10 §5.1, XF-09). Подтверждение — только кнопкой (AG-05).
 */
export function PendingActionCard({ initial, fromHistory }: { initial: PendingAction; fromHistory?: boolean }) {
  const qc = useQueryClient()
  const key = ['action', initial.action_id]
  const q = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getAction(initial.action_id, signal),
    initialData: fromHistory ? undefined : ({ ...initial, result: null } as ActionView),
    staleTime: fromHistory ? 0 : Infinity,
  })
  const [busy, setBusy] = useState<'confirm' | 'cancel' | null>(null)
  const [error, setError] = useState<{ text: string; retry: boolean } | null>(null)
  const view = q.data
  const s = view?.summary ?? initial.summary

  const setState = (state: ActionView['state'], result?: ActionView['result']) =>
    qc.setQueryData<ActionView>(key, (old) => (old ? { ...old, state, result: result ?? old.result } : old))

  const confirm = async () => {
    setBusy('confirm')
    setError(null)
    useMascot.getState().setClient('thinking')
    try {
      const res = await confirmAction(initial.action_id)
      setState('confirmed', res.result)
      useChat.getState().appendBot(res.reply)
      invalidateAfterRequest()
    } catch (e) {
      const err = e instanceof ApiError ? e : null
      useMascot.getState().setClient('oops')
      if (err?.status === 410 || err?.status === 404) {
        setState('expired')
      } else {
        setError({ text: err?.retryable ? 'Сервис не ответил, заявка не создана. Повтор не создаст дубликат' : errorText(e), retry: !!err?.retryable })
      }
    } finally {
      setBusy(null)
    }
  }

  const cancel = async () => {
    setBusy('cancel')
    try {
      const v = await cancelAction(initial.action_id)
      qc.setQueryData(key, v)
      useMascot.getState().setClient('hello')
    } catch (e) {
      if (e instanceof ApiError && (e.status === 410 || e.status === 404)) setState('expired')
      else setError({ text: errorText(e), retry: false })
    } finally {
      setBusy(null)
    }
  }

  const title =
    initial.kind === 'time_off'
      ? `Отгул ${rangeText(s.date_from, s.date_to)}${s.working_days ? `, ${workingDaysText(s.working_days)}` : ''}`
      : `Заявка на «${s.title ?? s.resource_id}»`

  return (
    <div className="action card" aria-label="Черновик заявки">
      <p className="timeoff__title">Проверь: {title}</p>
      {initial.kind === 'access' && (
        <dl className="summary">
          {s.access_level && (
            <>
              <dt>Уровень</dt>
              <dd>{LEVEL[s.access_level]}</dd>
            </>
          )}
          {s.approver && (
            <>
              <dt>Согласующий</dt>
              <dd>{s.approver}</dd>
            </>
          )}
          {s.effective_policy && (
            <>
              <dt>Правило</dt>
              <dd>{policyHint(s.effective_policy, s.timer_minutes)}</dd>
            </>
          )}
          {s.justification && (
            <>
              <dt>Обоснование</dt>
              <dd>{s.justification}</dd>
            </>
          )}
        </dl>
      )}
      <Plaque>
        Тестовый порядок, не регламент Авито ·{' '}
        <Link to="/kb/$slug" params={{ slug: initial.kind === 'access' ? 'access-test-order' : 'time-off-test-order' }}>
          как это устроено
        </Link>
      </Plaque>

      {!view && q.isPending && <p className="subtle">Проверяю состояние черновика…</p>}
      {q.isError && !view && <p className="subtle">Состояние черновика не загрузилось: {errorText(q.error)}</p>}

      {view?.state === 'active' && (
        <>
          <p className="subtle">Ничего не отправлю, пока не нажмёшь «Подтвердить». Черновик действует до {hhmm(view.expires_at)}.</p>
          {error && (
            <p className="fieldset__caption--error" role="alert">
              {error.text}
            </p>
          )}
          <div className="row">
            <Button variant="primary" loading={busy === 'confirm'} disabled={!!busy} onClick={() => void confirm()}>
              {error?.retry ? 'Повторить' : 'Подтвердить'}
            </Button>
            <Button variant="ghost" loading={busy === 'cancel'} disabled={!!busy} onClick={() => void cancel()}>
              Отмена
            </Button>
          </div>
        </>
      )}
      {view?.state === 'confirming' && <p className="subtle">Заявка отправляется…</p>}
      {view?.state === 'confirmed' && (
        <p>
          <Chip tone="success">Отправлено</Chip> {view.result ? `Заявка №${view.result.request_id}` : ''}
        </p>
      )}
      {view && STATE_TEXT[view.state] && <p className="subtle">{STATE_TEXT[view.state]}</p>}
    </div>
  )
}
