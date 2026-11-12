// Чистые функции панели «Мой путь» (11 §1.5, §1.7): тексты, «Что дальше», оптимистичная отметка.
import type { PathStep, PathView, Period, StepStatus } from '@/shared/api/schemas'
import { ddmm, hhmm } from '@/shared/lib/dates'

export const STATUS_TEXT: Record<StepStatus, { label: string; icon: string; tone: 'neutral' | 'info' | 'warning' | 'success' }> = {
  not_started: { label: 'Не начат', icon: '○', tone: 'neutral' },
  in_progress: { label: 'В работе', icon: '◐', tone: 'info' },
  waiting: { label: 'Ждёт ответа', icon: '⏳', tone: 'warning' },
  done: { label: 'Готово', icon: '✓', tone: 'success' },
}

export const TYPE_ICON: Record<PathStep['type'], string> = { learn: '📘', setup: '🔧', access: '🔑', meet: '👥', tell: '🎤', org: '📋' }
export const TYPE_TEXT: Record<PathStep['type'], string> = { learn: 'Изучить', setup: 'Настроить', access: 'Доступ', meet: 'Встреча', tell: 'Рассказать', org: 'Организационное' }

export const MAIN_BUTTON: Record<PathStep['help']['kind'], string> = {
  explain: 'Разобраться',
  access_request: 'Запросить доступ',
  ask_mentor: 'Сформулировать вопрос',
  track_only: 'Начать',
}

export const REJECT_REASON: Record<string, string> = {
  not_needed: 'доступ сейчас не нужен',
  other_resource: 'нужен другой ресурс',
  discuss_1on1: 'обсудим на 1:1',
}

/** Можно ли отметить шаг вручную (шаг с заявкой закрывает только бадди, 11 §1.5.3). */
export const canMarkDone = (s: PathStep) => s.help.kind !== 'access_request' && (s.status === 'not_started' || s.status === 'in_progress')
export const canReopen = (s: PathStep) => s.help.kind !== 'access_request' && s.status === 'done'

export function buttonLabel(s: PathStep): string {
  return `${MAIN_BUTTON[s.help.kind]}: ${s.title}`
}

export function periodDayText(p: PathView): string {
  const period = p.periods.find((x) => x.id === p.current_period)
  if (!period) return `день ${p.day}`
  if (period.to_day == null) return `${period.title} · день ${p.day}`
  const len = period.to_day - period.from_day + 1
  return `${period.title} · день ${p.day - period.from_day + 1} из ${len}`
}

export function reasonText(reason: 'overdue' | 'current' | 'ahead', period?: Period): string | null {
  if (reason === 'overdue') return `Хвост этапа «${period?.title ?? ''}»`
  if (reason === 'ahead') return 'Можно начать заранее'
  return null
}

export function requestText(s: PathStep, timeZone?: string): string | null {
  const r = s.request
  if (!r) return null
  if (r.status === 'approved') return `Заявка ${r.request_id} одобрена`
  if (r.status === 'rejected') return `Заявку ${r.request_id} отклонили${r.reject_reason ? `: ${REJECT_REASON[r.reject_reason] ?? r.reject_reason}` : ''}`
  if (r.auto_approve_at) return `Одобрится около ${hhmm(r.auto_approve_at, timeZone)}`
  return 'Ждёт наставника'
}

export function startText(p: PathView): string {
  return `Стажировка начнётся ${ddmm(p.start_date)}. Можно заглянуть в первую неделю`
}

const OPEN: StepStatus[] = ['not_started', 'in_progress']

/** «Что дальше» (11 §1.5.4): хвосты прошлых этапов → текущий → ближайший будущий. */
export function computeNext(steps: PathStep[], periods: Period[]): PathView['next_step'] {
  const order = new Map(periods.map((p, i) => [p.id, i]))
  const state = new Map(periods.map((p) => [p.id, p.state]))
  const sorted = [...steps].sort((a, b) => (order.get(a.period) ?? 0) - (order.get(b.period) ?? 0) || a.order - b.order)
  const open = sorted.filter((s) => OPEN.includes(s.status))
  const past = open.find((s) => state.get(s.period) === 'past')
  if (past) return { step_id: past.step_id, reason: 'overdue' }
  const cur = open.find((s) => state.get(s.period) === 'current')
  if (cur) return { step_id: cur.step_id, reason: 'current' }
  const ahead = open.find((s) => state.get(s.period) === 'future')
  if (ahead) return { step_id: ahead.step_id, reason: 'ahead' }
  return null
}

/** Оптимистичное изменение статуса шага с пересчётом сводки на клиенте (< 100 мс, PF-03). */
export function applyStatus(p: PathView, stepId: string, status: StepStatus): PathView {
  const steps = p.steps.map((s) => (s.step_id === stepId ? { ...s, status } : s))
  const done = (list: PathStep[]) => list.filter((s) => s.status === 'done').length
  const periods = p.periods.map((per) => ({ ...per, done: done(steps.filter((s) => s.period === per.id)) }))
  const next_step = computeNext(steps, periods)
  const waiting = steps.filter((s) => s.status === 'waiting').map((s) => s.step_id)
  let state = p.state
  if (state !== 'pre_start') state = next_step ? 'ready' : waiting.length ? 'only_waiting' : 'finished'
  return {
    ...p,
    steps,
    periods,
    next_step,
    waiting,
    state,
    progress: {
      period_done: done(steps.filter((s) => s.period === p.current_period)),
      period_total: steps.filter((s) => s.period === p.current_period).length,
      overall_done: done(steps),
      overall_total: steps.length,
    },
  }
}

export type PanelState = 'loading' | 'error' | 'stale' | PathView['state']

export function panelState(q: { isPending: boolean; isError: boolean; data?: PathView }): PanelState {
  if (q.data) return q.isError ? 'stale' : q.data.state
  if (q.isError) return 'error'
  return 'loading'
}

/** Смена статусов заявок между опросами → тосты (11 §3.6, XF-03/XF-04). */
export function requestTransitions(prev: PathView | undefined, next: PathView): { step: PathStep; to: 'approved' | 'rejected' }[] {
  if (!prev) return []
  const before = new Map(prev.steps.map((s) => [s.step_id, s.request?.status]))
  const out: { step: PathStep; to: 'approved' | 'rejected' }[] = []
  for (const s of next.steps) {
    const was = before.get(s.step_id)
    const now = s.request?.status
    if (was === 'pending' && (now === 'approved' || now === 'rejected')) out.push({ step: s, to: now })
  }
  return out
}
