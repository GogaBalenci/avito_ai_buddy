import { useState, type ReactNode } from 'react'
import type { PathStep, PathView, Period } from '@/shared/api/schemas'
import { Button } from '@/shared/ui/Button'
import { Chip, Plaque } from '@/shared/ui/Chip'
import { Vitya } from '@/shared/mascot/Vitya'
import { buttonLabel, canMarkDone, canReopen, MAIN_BUTTON, periodDayText, reasonText, requestText, startText, STATUS_TEXT, TYPE_ICON, TYPE_TEXT } from './logic'

export type PathHandlers = {
  onStep: (s: PathStep) => void
  onMark: (s: PathStep) => void
  onReopen: (s: PathStep) => void
  onNextClick?: (s: PathStep) => void
}

type Props = {
  path: PathView
  handlers?: PathHandlers
  /** Запрос чата в полёте — основные кнопки шагов неактивны (PF-09). */
  busy?: boolean
  timeZone?: string
  stale?: boolean
  onRetry?: () => void
  children?: ReactNode
}

function Progress({ label, done, total, thin = false }: { label: string; done: number; total: number; thin?: boolean }) {
  const pct = total ? Math.round((done / total) * 100) : 0
  return (
    <div className={thin ? 'progress progress--thin' : 'progress'}>
      <div className="progress__label">
        <span>{label}</span>
        <span>
          {done} из {total}
        </span>
      </div>
      <div className="progress__track" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={total} aria-valuenow={done} aria-valuetext={`${done} из ${total}`}>
        <div className="progress__bar" style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

export function StatusChip({ step }: { step: PathStep }) {
  const s = STATUS_TEXT[step.status]
  return (
    <Chip tone={s.tone} icon={s.icon}>
      {s.label}
    </Chip>
  )
}

function StepRow({ step, handlers, busy, timeZone }: { step: PathStep; handlers?: PathHandlers; busy?: boolean; timeZone?: string }) {
  const [open, setOpen] = useState(false)
  const req = requestText(step, timeZone)
  const id = `step-${step.step_id}`
  return (
    <li className={`step step--${step.status}`}>
      <button type="button" className="step__head" aria-expanded={open} aria-controls={id} onClick={() => setOpen((v) => !v)}>
        <span className="step__icon" title={TYPE_TEXT[step.type]} aria-hidden="true">
          {TYPE_ICON[step.type]}
        </span>
        {step.checklist_ref && <span className="step__ref">{step.checklist_ref}</span>}
        <span className="step__title">{step.title}</span>
        {step.synthetic && <Chip tone="test">тестовый</Chip>}
        <StatusChip step={step} />
      </button>
      {open && (
        <div id={id} className="step__body">
          <p className="subtle">
            {TYPE_TEXT[step.type]}
            {req ? ` · ${req}` : ''}
          </p>
          {handlers && (
            <div className="row">
              {step.status !== 'done' && step.status !== 'waiting' && (
                <Button size="s" variant="primary" disabled={busy} onClick={() => handlers.onStep(step)}>
                  {MAIN_BUTTON[step.help.kind]}
                </Button>
              )}
              {step.status === 'waiting' && (
                <Button size="s" disabled={busy} onClick={() => handlers.onStep(step)}>
                  Статус заявки
                </Button>
              )}
              {canMarkDone(step) && (
                <Button size="s" onClick={() => handlers.onMark(step)}>
                  Отметить пройденным
                </Button>
              )}
              {canReopen(step) && (
                <Button size="s" variant="ghost" onClick={() => handlers.onReopen(step)}>
                  Вернуть в работу
                </Button>
              )}
            </div>
          )}
        </div>
      )}
    </li>
  )
}

function PeriodSection({ period, steps, ...rest }: { period: Period; steps: PathStep[]; handlers?: PathHandlers; busy?: boolean; timeZone?: string }) {
  const [open, setOpen] = useState(period.state === 'current')
  const hint = period.state === 'past' ? `готово ${period.done} из ${period.total}` : period.state === 'future' ? 'можно посмотреть заранее' : `${period.done} из ${period.total}`
  return (
    <section className={`period period--${period.state}`}>
      <button type="button" className="period__head" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span className="period__chevron" aria-hidden="true">
          {open ? '▾' : '▸'}
        </span>
        <span className="period__title">{period.title}</span>
        <span className="subtle">{hint}</span>
      </button>
      {open && (
        <ul className="steps">
          {steps.map((s) => (
            <StepRow key={s.step_id} step={s} {...rest} />
          ))}
          {steps.length === 0 && <li className="subtle">Шагов нет</li>}
        </ul>
      )}
    </section>
  )
}

/** Панель «Мой путь» (11 §1.7). Тонкая: данные и обработчики приходят снаружи. */
export function PathPanel({ path, handlers, busy, timeZone, stale, onRetry, children }: Props) {
  const byId = new Map(path.steps.map((s) => [s.step_id, s]))
  const next = path.next_step ? byId.get(path.next_step.step_id) : undefined
  const nextPeriod = next ? path.periods.find((p) => p.id === next.period) : undefined
  const waiting = path.waiting.map((id) => byId.get(id)).filter((s): s is PathStep => !!s)
  const track = path.employee.track

  return (
    <div className="path">
      {track.synthetic && <Plaque>{track.disclaimer ?? `Тестовый трек: ${track.title}`}</Plaque>}
      {stale && (
        <p className="subtle path__stale" role="status">
          Не обновилось ·{' '}
          <Button variant="link" onClick={onRetry}>
            Повторить
          </Button>
        </p>
      )}
      {path.stale_requests && (
        <p className="subtle path__stale" role="status">
          Статусы заявок не обновились ·{' '}
          <Button variant="link" onClick={onRetry}>
            Повторить
          </Button>
        </p>
      )}

      <div>
        <h2 className="path__period">{path.state === 'pre_start' ? 'Скоро старт' : periodDayText(path)}</h2>
        <Progress label="Готово на этапе" done={path.progress.period_done} total={path.progress.period_total} />
        <Progress label="Весь путь" done={path.progress.overall_done} total={path.progress.overall_total} thin />
      </div>

      {path.state === 'pre_start' && <p>{startText(path)}</p>}

      {path.state === 'finished' && (
        <div className="path__empty">
          <Vitya state="done" size={64} />
          <p>Все шаги пройдены. Обсуди итоги с наставником на 1:1</p>
        </div>
      )}

      {path.state === 'only_waiting' && (
        <div className="path__empty">
          <p>Осталось дождаться ответов</p>
          {onRetry && (
            <Button size="s" onClick={onRetry}>
              Обновить
            </Button>
          )}
        </div>
      )}

      {next && path.next_step && (
        <section className="next card" aria-labelledby="next-title">
          <p id="next-title" className="next__label">
            Что дальше
          </p>
          <p className="next__title">{next.title}</p>
          {reasonText(path.next_step.reason, nextPeriod) && <p className="subtle">{reasonText(path.next_step.reason, nextPeriod)}</p>}
          {next.synthetic && <Chip tone="test">тестовый</Chip>}
          {handlers && (
            <Button
              variant="primary"
              disabled={busy}
              onClick={() => {
                handlers.onNextClick?.(next)
                handlers.onStep(next)
              }}
            >
              {MAIN_BUTTON[next.help.kind]}
            </Button>
          )}
        </section>
      )}

      {waiting.length > 0 && (
        <Plaque tone="warning">
          <strong>Ждём ответа: </strong>
          {waiting
            .slice(0, 3)
            .map((s) => `${s.title}${requestText(s, timeZone) ? ` (${requestText(s, timeZone)?.toLowerCase()})` : ''}`)
            .join('; ')}
          {waiting.length > 3 && ` и ещё ${waiting.length - 3}`}
        </Plaque>
      )}

      {children}

      <div className="periods">
        {path.periods.map((p) => (
          <PeriodSection key={p.id} period={p} steps={path.steps.filter((s) => s.period === p.id)} handlers={handlers} busy={busy} timeZone={timeZone} />
        ))}
      </div>
    </div>
  )
}

export { buttonLabel }
