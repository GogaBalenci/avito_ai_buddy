import { useRef } from 'react'
import { Link } from '@tanstack/react-router'
import type { ReplyAction } from '@/shared/api/schemas'
import { ddmm, ddmmyyyy, hhmm, rangeText } from '@/shared/lib/dates'
import { Button } from '@/shared/ui/Button'
import { Chip, Plaque } from '@/shared/ui/Chip'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Fieldset } from '@/shared/ui/Fieldset'
import { Skeleton } from '@/shared/ui/Skeleton'
import { CopyButton } from '@/features/chat/CopyBox'
import { sendPayload } from '@/features/chat/chatStore'
import { workingDaysText, validateTimeOff } from './rules'
import { useTimeOffForm } from './useTimeOffForm'

const EDITABLE = ['empty', 'prefilled', 'editing', 'invalid_client', 'invalid_server', 'failed', 'expired']

/** Форма «от — до» и карточки черновика и результата в чате (10 §9). */
export function TimeOffFlow({ flowId, action, inactive = false }: { flowId: string; action: ReplyAction; inactive?: boolean }) {
  const { config, configError, retryConfig, flow, set, submit, confirm, cancel, edit, focusRef } = useTimeOffForm(flowId, action, inactive)
  const fromRef = useRef<HTMLInputElement>(null)
  const toRef = useRef<HTMLInputElement>(null)
  focusRef.current = (field) => (field === 'date_from' ? fromRef.current : field === 'date_to' ? toRef.current : toRef.current)?.focus()

  if (inactive && !flow) {
    return (
      <div className="timeoff card timeoff--inactive">
        <p className="subtle">Форма отгула из истории</p>
        <Button size="s" onClick={() => void sendPayload('intent:time_off', 'Оформить отгул')}>
          Оформить отгул
        </Button>
      </div>
    )
  }
  if (configError) return <ErrorState text={`Не получилось загрузить правила отгула. ${configError}`} onRetry={retryConfig} />
  if (!flow || !config) return <Skeleton lines={2} label="Готовлю форму" />

  const { state, errors } = flow
  const editable = EDITABLE.includes(state)
  const busy = state === 'submitting' || state === 'confirming'
  const plaque = (
    <Plaque>
      Тестовый порядок, не регламент Авито · <Link to="/kb/$slug" params={{ slug: 'time-off-test-order' }}>[1] Порядок оформления отгула</Link>
    </Plaque>
  )

  if (state === 'cancelled') return <p className="subtle timeoff__note">Окей, ничего не отправляю</p>

  if (state === 'done' && flow.result) {
    const r = flow.result
    return (
      <div className="timeoff card timeoff--done" role="status">
        <p className="timeoff__title">
          Готово: заявка №{r.request_id} <Chip tone="success">Оформлена</Chip>
        </p>
        <p>
          Отгул {rangeText(r.date_from, r.date_to)}
          {r.working_days ? `, ${workingDaysText(r.working_days)}` : ''}. Уже в «Моём пути» и в истории.
        </p>
        {plaque}
        <div className="row">
          <CopyButton label="Скопировать сообщение наставнику" text={flow.copyText ?? ''} variant="primary" />
          <Link to="/requests" className="btn btn--ghost btn--s">
            Мои заявки
          </Link>
        </div>
      </div>
    )
  }

  if ((state === 'draft' || state === 'confirming' || state === 'service_error') && flow.draft) {
    const s = flow.draft.summary
    return (
      <div className="timeoff card" aria-label="Черновик заявки на отгул">
        <p className="timeoff__title">
          Проверь: отгул {rangeText(s.date_from, s.date_to)}
          {s.working_days ? `, ${workingDaysText(s.working_days)}` : ''}.
        </p>
        <p className="subtle">Ничего не отправлю, пока не нажмёшь «Подтвердить». Черновик действует до {hhmm(flow.draft.expires_at, 'Europe/Moscow')}.</p>
        {flow.overlapUnchecked && <Plaque tone="info">Пересечения с другими отгулами проверю при отправке</Plaque>}
        {plaque}
        {state === 'service_error' && (
          <p className="fieldset__caption--error" role="alert">
            {flow.message}
          </p>
        )}
        {flow.slow && <p className="subtle">Сервис отвечает дольше обычного…</p>}
        <div className="row">
          <Button variant="primary" loading={state === 'confirming'} onClick={() => void confirm()}>
            {state === 'service_error' ? 'Повторить' : 'Подтвердить'}
          </Button>
          {state === 'draft' && <Button onClick={edit}>Изменить</Button>}
          <Button variant="ghost" disabled={state === 'confirming'} onClick={() => void cancel()}>
            Отмена
          </Button>
        </div>
      </div>
    )
  }

  const valid = config ? validateTimeOff(flow.from, flow.to, config) : null
  const fieldError = (k: 'date_from' | 'date_to') => errors[k] ?? (errors.range && k === 'date_to' ? errors.range : undefined)
  const captionError = errors.date_from ?? errors.date_to ?? errors.range
  const caption = captionError
    ? captionError.message
    : valid?.workingDays
      ? `${workingDaysText(valid.workingDays)}, выходные не считаются`
      : `До ${config.max_working_days} рабочих дней, начало и конец — в рабочий день, не позже ${ddmmyyyy(config.max_date)}`
  const captionId = `to-cap-${flowId}`

  return (
    <form
      className={`timeoff card${state === 'superseded' ? ' timeoff--inactive' : ''}`}
      noValidate
      aria-label="Даты отгула"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      {plaque}
      {flow.prefilled && state === 'prefilled' && <p className="subtle">Подставил даты из сообщения — проверь</p>}
      {flow.existing.length > 0 && (
        <p className="subtle">
          У тебя уже есть отгул {flow.existing.map((x) => `${ddmm(x.date_from)}–${ddmm(x.date_to)}`).join(', ')}
        </p>
      )}
      {state === 'expired' && (
        <p className="fieldset__caption--error" role="alert">
          Черновик устарел — проверь даты ещё раз
        </p>
      )}
      <Fieldset legend="Даты отгула" caption={caption} captionId={captionId} error={!!captionError}>
        <label className="field">
          От
          <input
            ref={fromRef}
            type="date"
            value={flow.from}
            disabled={!editable}
            aria-invalid={!!fieldError('date_from')}
            aria-describedby={captionId}
            onChange={(e) => set('from', e.target.value)}
          />
        </label>
        <label className="field">
          До
          <input
            ref={toRef}
            type="date"
            value={flow.to}
            disabled={!editable}
            aria-invalid={!!fieldError('date_to')}
            aria-describedby={captionId}
            onChange={(e) => set('to', e.target.value)}
          />
        </label>
      </Fieldset>
      {errors.range?.code === 'OVERLAP' && (
        <Link to="/requests" className="subtle">
          Посмотреть заявку {errors.range.requestId ?? ''}
        </Link>
      )}
      {state === 'failed' && (
        <p className="fieldset__caption--error" role="alert">
          {flow.message}
        </p>
      )}
      {flow.slow && <p className="subtle">Сервис отвечает дольше обычного…</p>}
      {state === 'superseded' ? (
        <p className="subtle">Открыта новая форма ниже</p>
      ) : (
        <div className="row">
          <Button type="submit" variant="primary" loading={busy} disabled={!flow.from}>
            {state === 'failed' ? 'Повторить' : 'Проверить'}
          </Button>
          <Button variant="ghost" disabled={busy} onClick={() => void cancel()}>
            Отмена
          </Button>
        </div>
      )}
    </form>
  )
}
