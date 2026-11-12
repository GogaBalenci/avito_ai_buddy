import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { errorsFromServer, initialFlow, reduce, type Flow } from './flow'
import { timeOffSchema, validateTimeOff, violationMessage, workingDaysText } from './rules'
import * as f from '@/test/fixtures'

type Case = { id: string; date_from: string; date_to: string; existing: unknown[]; expect: { ok: boolean; working_days?: number; violations?: { field: string; code: string }[] } }
// Общие тест-векторы Go + Vitest (10 §16.1). Читаются с диска, чтобы сборка образа web не зависела от docs/.
const vectors = JSON.parse(readFileSync(resolve(process.cwd(), '../docs/specs/s3/validation-cases.json'), 'utf8')) as {
  today: string
  max_working_days: number
  horizon_days: number
  cases: Case[]
}
const limits = { today: vectors.today, max_working_days: vectors.max_working_days, horizon_days: vectors.horizon_days }

describe('time-off rules: shared vectors (F-01)', () => {
  it('has vectors', () => expect(vectors.cases.length).toBeGreaterThan(15))
  it.each(vectors.cases.filter((c) => c.existing.length === 0).map((c) => [c.id, c] as const))('%s', (_id, c) => {
    const r = validateTimeOff(c.date_from, c.date_to, limits)
    if (c.expect.ok) {
      expect(r.violations).toEqual([])
      expect(r.workingDays).toBe(c.expect.working_days)
    } else {
      expect(r.violations.map((v) => ({ field: v.field, code: v.code }))).toEqual(c.expect.violations)
    }
  })
})

describe('time-off rules: messages and schema', () => {
  it('messages match the Go texts', () => {
    const m = (code: string, params?: Record<string, unknown>) => violationMessage({ field: 'range', code, params })
    expect(m('REQUIRED')).toBe('Укажи дату')
    expect(m('INVALID_DATE')).toBe('Не похоже на дату')
    expect(m('DATE_IN_PAST')).toMatch(/уже прошла/)
    expect(m('TOO_FAR', { max_date: '2027-02-08' })).toBe('Можно выбрать дату не позже 08.02.2027')
    expect(m('WEEKEND_DATE', { date: '2026-11-14', weekday: 6 })).toBe('14.11 — суббота. Отгул начинается и заканчивается в рабочий день')
    expect(m('WEEKEND_DATE', { date: '2026-11-15', weekday: 0 })).toMatch(/воскресенье/)
    expect(m('WEEKEND_DATE', { date: 'x' })).toMatch(/выходной/)
    expect(m('RANGE_REVERSED')).toBe('«До» не может быть раньше «от»')
    expect(m('TOO_LONG', { working_days: 6 })).toMatch(/^Выбрано 6 рабочих дней, а в тестовом сервисе можно до 5/)
    expect(m('OVERLAP', { request_id: 'ABS-000101', date_from: '2026-11-30', date_to: '2026-12-01' })).toBe('Пересекается с заявкой ABS-000101 на 30.11–01.12')
    expect(m('WHATEVER')).toMatch(/Сервис отклонил даты/)
    expect(workingDaysText(1)).toBe('1 рабочий день')
    expect(workingDaysText(2)).toBe('2 рабочих дня')
    expect(workingDaysText(5)).toBe('5 рабочих дней')
  })

  it('Zod schema reports codes on fields', () => {
    const s = timeOffSchema(limits)
    expect(s.safeParse({ date_from: '2026-11-13', date_to: '2026-11-13' }).success).toBe(true)
    const bad = s.safeParse({ date_from: '2026-11-09', date_to: '' })
    expect(bad.success).toBe(false)
    expect(bad.error!.issues.map((i) => i.path[0])).toEqual(['date_from', 'date_to'])
  })

  it('TZ-independent: uses today from config, not the browser clock (F-12)', () => {
    // «сегодня» на сервере 2026-11-10 (вторник): 10.11 разрешено, 09.11 — в прошлом
    expect(validateTimeOff('2026-11-10', '2026-11-10', limits).violations).toEqual([])
    expect(validateTimeOff('2026-11-09', '2026-11-10', limits).violations[0].code).toBe('DATE_IN_PAST')
  })
})

describe('time-off flow reducer (F-02, F-03)', () => {
  const cfg = f.timeOffConfig
  const start = () => initialFlow('a', {}, cfg)

  it('empty → editing; «до» = «от» when empty; invalid_client on rule break', () => {
    let s: Flow = start()
    expect(s.state).toBe('empty')
    s = reduce(s, { type: 'set', field: 'from', value: '2026-11-13', config: cfg })
    expect(s.to).toBe('2026-11-13')
    expect(s.state).toBe('editing')
    s = reduce(s, { type: 'set', field: 'to', value: '2026-11-14', config: cfg })
    expect(s.state).toBe('invalid_client')
    expect(s.errors.date_to?.code).toBe('WEEKEND_DATE')
    s = reduce(s, { type: 'set', field: 'to', value: '2026-11-16', config: cfg })
    expect(s.state).toBe('editing')
  })

  it('prefilled shows errors right away', () => {
    const s = initialFlow('a', { prefill: { date_from: '2026-11-14', date_to: '2026-11-14' }, existing: [] }, cfg)
    expect(s.state).toBe('prefilled')
    expect(s.errors.date_from?.code).toBe('WEEKEND_DATE')
    expect(initialFlow('a', {}, undefined).errors).toEqual({})
  })

  it('full happy path and side states', () => {
    let s = reduce(start(), { type: 'set', field: 'from', value: '2026-11-13', config: cfg })
    s = reduce(s, { type: 'submit_start' })
    expect(s.state).toBe('submitting')
    s = reduce(s, { type: 'slow' })
    expect(s.slow).toBe(true)
    const draft = { action_id: 'x', kind: 'time_off' as const, state: 'active' as const, expires_at: '2026-11-12T10:00:00Z', summary: {} }
    s = reduce(s, { type: 'submit_ok', draft, overlapUnchecked: true })
    expect(s.state).toBe('draft')
    expect(reduce(s, { type: 'set', field: 'from', value: '2026-11-16' })).toBe(s)
    expect(reduce(s, { type: 'edit' }).state).toBe('editing')
    expect(reduce(start(), { type: 'edit' }).state).toBe('empty')
    s = reduce(s, { type: 'confirm_start' })
    expect(reduce(s, { type: 'service_error', message: 'm' }).state).toBe('service_error')
    expect(reduce(s, { type: 'expired', keepDates: true }).from).toBe('2026-11-13')
    expect(reduce(s, { type: 'expired', keepDates: false }).from).toBe('')
    s = reduce(s, { type: 'confirm_ok', result: f.requestAbs })
    expect(s.state).toBe('done')
    expect(reduce(s, { type: 'slow' }).slow).toBeFalsy()
    expect(reduce(s, { type: 'supersede' }).state).toBe('done')
    expect(reduce(start(), { type: 'supersede' }).state).toBe('superseded')
    expect(reduce(start(), { type: 'cancel' }).state).toBe('cancelled')
    expect(reduce(start(), { type: 'submit_failed', message: 'x' }).state).toBe('failed')
    expect(reduce(start(), { type: 'server_invalid', errors: {} }).state).toBe('invalid_server')
    expect(reduce(start(), { type: 'client_invalid', errors: {} }).touched).toBe(true)
  })

  it('maps server field errors, keeping OVERLAP request id', () => {
    const e = errorsFromServer([
      { field: 'range', code: 'OVERLAP', message: 'Пересекается', params: { request_id: 'ABS-000101' } },
      { field: 'date_from', code: 'DATE_IN_PAST', message: 'прошла' },
      { field: 'date_from', code: 'TOO_FAR', message: 'второе' },
      { field: 'payload', code: 'X', message: 'payload' },
    ])
    expect(e.range?.requestId).toBe('ABS-000101')
    expect(e.date_from?.code).toBe('DATE_IN_PAST')
    expect(errorsFromServer(undefined)).toEqual({})
  })
})
