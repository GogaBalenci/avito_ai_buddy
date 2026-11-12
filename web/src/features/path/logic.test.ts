import * as f from '@/test/fixtures'
import { applyStatus, buttonLabel, canMarkDone, canReopen, computeNext, panelState, periodDayText, reasonText, requestText, requestTransitions, startText } from './logic'

describe('path logic', () => {
  it('period/day text uses server day (PF-07)', () => {
    expect(periodDayText(f.path())).toBe('Неделя 1 — Вход в команду · день 4 из 7')
    const after = f.path({ current_period: 'after3', day: 120, periods: [{ id: 'after3', title: 'После 3 месяцев', from_day: 91, to_day: null, state: 'current', done: 0, total: 0 }] })
    expect(periodDayText(after)).toBe('После 3 месяцев · день 120')
    expect(periodDayText(f.path({ periods: [] }))).toBe('день 4')
  })

  it('reason, request and start texts', () => {
    expect(reasonText('overdue', f.path().periods[0])).toBe('Хвост этапа «Неделя 1 — Вход в команду»')
    expect(reasonText('overdue')).toBe('Хвост этапа «»')
    expect(reasonText('ahead')).toBe('Можно начать заранее')
    expect(reasonText('current')).toBeNull()
    const s = f.path().steps[2]
    expect(requestText(s)).toBe('Ждёт наставника')
    expect(requestText({ ...s, request: { ...s.request!, auto_approve_at: '2026-11-12T09:02:00Z' } })).toBe('Одобрится около 12:02')
    expect(requestText({ ...s, request: { ...s.request!, status: 'approved' } })).toBe('Заявка ACC-000101 одобрена')
    expect(requestText({ ...s, request: { ...s.request!, status: 'rejected', reject_reason: 'discuss_1on1' } })).toBe('Заявку ACC-000101 отклонили: обсудим на 1:1')
    expect(requestText({ ...s, request: { ...s.request!, status: 'rejected', reject_reason: 'x' } })).toBe('Заявку ACC-000101 отклонили: x')
    expect(requestText({ ...s, request: { ...s.request!, status: 'rejected' } })).toBe('Заявку ACC-000101 отклонили')
    expect(requestText(f.path().steps[0])).toBeNull()
    expect(startText(f.path({ start_date: '2026-11-20' }))).toMatch(/20\.11/)
    expect(buttonLabel(f.path().steps[4])).toBe('Разобраться: Изучи принципы разработки')
  })

  it('who can mark and reopen', () => {
    const [done, access, waiting, todo] = f.path().steps
    expect(canMarkDone(todo)).toBe(true)
    expect(canMarkDone(access)).toBe(false)
    expect(canMarkDone(waiting)).toBe(false)
    expect(canReopen(done)).toBe(true)
    expect(canReopen({ ...access, status: 'done' })).toBe(false)
  })

  it('computeNext: overdue → current → ahead → null (11 §1.5.4)', () => {
    const p = f.path()
    expect(computeNext(p.steps, p.periods)).toEqual({ step_id: 'cm-1.7', reason: 'current' })
    const past = p.periods.map((x) => (x.id === 'week1' ? { ...x, state: 'past' as const } : { ...x, state: 'current' as const }))
    expect(computeNext(p.steps, past)).toEqual({ step_id: 'cm-1.7', reason: 'overdue' })
    const allDoneW1 = p.steps.map((s) => (s.period === 'week1' && s.status !== 'waiting' ? { ...s, status: 'done' as const } : s))
    expect(computeNext(allDoneW1, p.periods)).toEqual({ step_id: 'cm-2.1', reason: 'ahead' })
    expect(computeNext(p.steps.map((s) => ({ ...s, status: 'done' as const })), p.periods)).toBeNull()
  })

  it('applyStatus recomputes progress, next, waiting and state', () => {
    const p = applyStatus(f.path(), 'cm-1.9', 'done')
    expect(p.progress.period_done).toBe(3)
    expect(p.progress.overall_done).toBe(3)
    expect(p.periods[0].done).toBe(3)
    expect(p.next_step?.step_id).toBe('cm-1.7')
    let all = f.path()
    for (const s of all.steps) if (s.status !== 'waiting') all = applyStatus(all, s.step_id, 'done')
    expect(all.state).toBe('only_waiting')
    all = applyStatus(all, 'cm-1.10', 'done')
    expect(all.state).toBe('finished')
    expect(applyStatus(f.path({ state: 'pre_start' }), 'cm-1.9', 'done').state).toBe('pre_start')
  })

  it('panelState covers loading, error, stale and server states (PF-02)', () => {
    expect(panelState({ isPending: true, isError: false })).toBe('loading')
    expect(panelState({ isPending: false, isError: true })).toBe('error')
    expect(panelState({ isPending: false, isError: true, data: f.path() })).toBe('stale')
    expect(panelState({ isPending: false, isError: false, data: f.path({ state: 'finished' }) })).toBe('finished')
  })

  it('requestTransitions finds pending → approved/rejected', () => {
    const prev = f.path()
    const next = f.path({ steps: prev.steps.map((s) => (s.step_id === 'cm-1.10' ? { ...s, status: 'done' as const, request: { ...s.request!, status: 'approved' as const } } : s)) })
    expect(requestTransitions(undefined, next)).toEqual([])
    expect(requestTransitions(prev, next).map((t) => t.to)).toEqual(['approved'])
    const rej = f.path({ steps: prev.steps.map((s) => (s.step_id === 'cm-1.10' ? { ...s, request: { ...s.request!, status: 'rejected' as const } } : s)) })
    expect(requestTransitions(prev, rej).map((t) => t.to)).toEqual(['rejected'])
  })
})
