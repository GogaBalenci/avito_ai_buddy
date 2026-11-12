// Правила дат отгула (10 §6): те же коды и порядок, что в Go (internal/timeoff/rules),
// общие тест-векторы docs/specs/s3/validation-cases.json. Это подсказка, а не источник истины:
// окончательно проверяет сервер. «Сегодня» — только из GET /api/time-off/config.
import { z } from 'zod'
import type { TimeOffConfig } from '@/shared/api/schemas'
import { ddmm, ddmmyyyy, isWeekend, parseIsoDate, plural, toIso, weekday, workingDays, DAY_MS } from '@/shared/lib/dates'

export type TimeOffField = 'date_from' | 'date_to' | 'range'
export type Violation = { field: TimeOffField; code: string; params?: Record<string, unknown> }
export type Limits = Pick<TimeOffConfig, 'today' | 'max_working_days' | 'horizon_days'>

const WEEKDAY: Record<number, string> = { 6: 'суббота', 0: 'воскресенье' }

function checkField(field: 'date_from' | 'date_to', value: string, today: number, maxDate: number): { t: number | null; v?: Violation } {
  const s = value.trim()
  if (!s) return { t: null, v: { field, code: 'REQUIRED' } }
  const t = parseIsoDate(s)
  if (t === null) return { t: null, v: { field, code: 'INVALID_DATE' } }
  if (t < today) return { t, v: { field, code: 'DATE_IN_PAST', params: { today: toIso(today) } } }
  if (t > maxDate) return { t, v: { field, code: 'TOO_FAR', params: { max_date: toIso(maxDate) } } }
  if (isWeekend(t)) return { t, v: { field, code: 'WEEKEND_DATE', params: { date: s, weekday: weekday(t) } } }
  return { t }
}

/** Порядок 10 §6.2: поля → интервал. OVERLAP проверяет только сервер. */
export function validateTimeOff(dateFrom: string, dateTo: string, limits: Limits): { violations: Violation[]; workingDays?: number } {
  const today = parseIsoDate(limits.today) ?? 0
  const maxDate = today + limits.horizon_days * DAY_MS
  const f = checkField('date_from', dateFrom, today, maxDate)
  const t = checkField('date_to', dateTo, today, maxDate)
  const vs = [f.v, t.v].filter((x): x is Violation => !!x)
  if (vs.length) return { violations: vs }
  const from = f.t!
  const to = t.t!
  if (to < from) return { violations: [{ field: 'range', code: 'RANGE_REVERSED' }] }
  const wd = workingDays(from, to)
  if (wd > limits.max_working_days) return { violations: [{ field: 'range', code: 'TOO_LONG', params: { working_days: wd, max: limits.max_working_days } }] }
  return { violations: [], workingDays: wd }
}

/** Тексты ошибок — как у Go (rules.Message), чтобы клиент и сервер говорили одинаково. */
export function violationMessage(v: Violation, limits?: Pick<TimeOffConfig, 'max_working_days'>): string {
  const p = v.params ?? {}
  const str = (k: string) => (typeof p[k] === 'string' ? (p[k] as string) : '')
  switch (v.code) {
    case 'REQUIRED':
      return 'Укажи дату'
    case 'INVALID_DATE':
      return 'Не похоже на дату'
    case 'DATE_IN_PAST':
      return 'Дата уже прошла — выбери сегодня или позже'
    case 'TOO_FAR':
      return `Можно выбрать дату не позже ${ddmmyyyy(str('max_date'))}`
    case 'WEEKEND_DATE':
      return `${ddmm(str('date'))} — ${WEEKDAY[Number(p.weekday)] ?? 'выходной'}. Отгул начинается и заканчивается в рабочий день`
    case 'RANGE_REVERSED':
      return '«До» не может быть раньше «от»'
    case 'TOO_LONG':
      return `Выбрано ${Number(p.working_days)} рабочих дней, а в тестовом сервисе можно до ${limits?.max_working_days ?? 5}. Для долгого отсутствия поговори с руководителем`
    case 'OVERLAP':
      return `Пересекается с заявкой ${str('request_id')} на ${ddmm(str('date_from'))}–${ddmm(str('date_to'))}`
    default:
      return 'Сервис отклонил даты: проверь их или спроси наставника'
  }
}

export function workingDaysText(n: number): string {
  return `${n} ${plural(n, ['рабочий день', 'рабочих дня', 'рабочих дней'])}`
}

/** Zod-схема формы с superRefine (10 §6.3): те же коды в issue.params.code. */
export function timeOffSchema(limits: Limits) {
  return z
    .object({ date_from: z.string(), date_to: z.string() })
    .superRefine((val, ctx) => {
      for (const v of validateTimeOff(val.date_from, val.date_to, limits).violations) {
        ctx.addIssue({ code: 'custom', path: [v.field], message: violationMessage(v, limits), params: { code: v.code, ...v.params } })
      }
    })
}
