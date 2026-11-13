import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import type { PathView } from '@/shared/api/schemas'
import { useMascot } from '@/shared/mascot/mascotStore'

const panel = () => screen.getByRole('complementary', { name: 'Мой путь' })
const summary = (done: number) => ({ state: 'ready', next_step: { step_id: 'cm-1.7', reason: 'current' }, waiting: ['cm-1.10'], progress: { period_done: done, period_total: 6, overall_done: done, overall_total: 7 } })

async function openPanel() {
  const r = renderApp('/')
  await within(await screen.findByRole('complementary', { name: 'Мой путь' })).findByText('Что дальше')
  return r
}

describe('«Мой путь» panel (wave 3)', () => {
  it('first screen: period and day, progress, next step, waiting, accordion (AC-P09)', async () => {
    await openPanel()
    const p = panel()
    expect(within(p).getByText('Неделя 1 — Вход в команду · день 4 из 7')).toBeInTheDocument()
    const bar = within(p).getByRole('progressbar', { name: 'Готово на этапе' })
    expect(bar).toHaveAttribute('aria-valuenow', '2')
    expect(bar).toHaveAttribute('aria-valuemax', '6')
    const next = within(p).getByRole('region', { name: 'Что дальше' })
    expect(within(next).getByText('Попроси добавить тебя во все ключевые каналы и чаты')).toBeInTheDocument()
    expect(within(next).getByRole('button', { name: 'Запросить доступ' })).toBeInTheDocument()
    expect(within(p).getByText(/Ждём ответа/).parentElement).toHaveTextContent('ждёт наставника')
    expect(within(p).getByRole('button', { name: /Неделя 1 — Вход в команду/ })).toHaveAttribute('aria-expanded', 'true')
    expect(within(p).getByRole('button', { name: /Неделя 2/ })).toHaveAttribute('aria-expanded', 'false')
    expect(within(p).getAllByText('Готово').length).toBeGreaterThan(0)
    expect(within(p).getByText('Ждёт ответа')).toBeInTheDocument()
  })

  it('shows a skeleton while loading (PF-02)', async () => {
    server.use(http.get('/api/path', async () => {
      await delay(200)
      return HttpResponse.json(f.path())
    }))
    renderApp('/')
    expect(await screen.findByRole('status', { name: 'Загружаю твой путь' })).toBeInTheDocument()
    expect(await screen.findByText('Что дальше')).toBeInTheDocument()
  })

  it('error state with «Повторить»; chat column stays (P-A1)', async () => {
    let fail = true
    server.use(http.get('/api/path', () => (fail ? errorBody(503, 'UNAVAILABLE', 'Сервис чек-листа не отвечает') : HttpResponse.json(f.path()))))
    renderApp('/')
    expect(await screen.findByText(/Не получилось загрузить твой путь/)).toBeInTheDocument()
    expect(screen.getByLabelText('Чат с Витей')).toBeInTheDocument()
    fail = false
    await userEvent.click(within(panel()).getByRole('button', { name: 'Повторить' }))
    expect(await screen.findByText('Что дальше')).toBeInTheDocument()
  })

  it('unexpected format → error, not a crash (PF-01)', async () => {
    server.use(http.get('/api/path', () => HttpResponse.json({ steps: 'nope' })))
    renderApp('/')
    expect(await screen.findByText(/неожиданном формате/)).toBeInTheDocument()
  })

  it.each<[Partial<PathView>, RegExp]>([
    [{ state: 'pre_start', start_date: '2026-11-16', day: -3, next_step: { step_id: 'cm-1.7', reason: 'ahead' } }, /Стажировка начнётся 16\.11/],
    [{ state: 'only_waiting', next_step: null }, /Осталось дождаться ответов/],
    [{ state: 'finished', next_step: null, waiting: [] }, /Все шаги пройдены/],
  ])('server state %#', async (over, text) => {
    server.use(http.get('/api/path', () => HttpResponse.json(f.path(over))))
    renderApp('/')
    expect(await within(await screen.findByRole('complementary', { name: 'Мой путь' })).findByText(text)).toBeInTheDocument()
  })

  it('stale: background refetch fails, data stays with «Не обновилось»', async () => {
    const { queryClient } = await openPanel()
    server.use(http.get('/api/path', () => errorBody(503, 'UNAVAILABLE', 'нет')))
    await act(() => queryClient.refetchQueries({ queryKey: ['path'] }).catch(() => undefined))
    expect(await within(panel()).findByText(/Не обновилось/)).toBeInTheDocument()
    expect(within(panel()).getByText('Что дальше')).toBeInTheDocument()
  })

  it('synthetic track plaque and «тестовый» chips (PF-05); stale_requests caption (XF-08)', async () => {
    const base = f.path()
    server.use(http.get('/api/path', () => HttpResponse.json(f.path({
      employee: { ...base.employee, track: { id: 'frontend', title: 'Frontend', synthetic: true, disclaimer: 'Тестовый трек: шаги придуманы для прототипа' } },
      steps: base.steps.map((s) => ({ ...s, synthetic: true })),
      stale_requests: true,
    }))))
    await openPanel()
    expect(within(panel()).getByText('Тестовый трек: шаги придуманы для прототипа')).toBeInTheDocument()
    expect(within(panel()).getAllByText('тестовый').length).toBeGreaterThan(1)
    expect(within(panel()).getByText(/Статусы заявок не обновились/)).toBeInTheDocument()
  })

  it('main step button sends step:<id> with a new UUID each time and is disabled in flight (PF-09, PF-04)', async () => {
    const bodies: { client_message_id: string; payload?: string }[] = []
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string; payload?: string }
      bodies.push(b)
      await delay(50)
      return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, reply_id: crypto.randomUUID(), intent: 'step_explain', text: 'Объяснение' }))
    }))
    await openPanel()
    const btn = within(within(panel()).getByRole('region', { name: 'Что дальше' })).getByRole('button', { name: 'Запросить доступ' })
    await userEvent.click(btn)
    expect(btn).toBeDisabled()
    await waitFor(() => expect(btn).toBeEnabled())
    await userEvent.click(btn)
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[0].payload).toBe('step:cm-1.7')
    expect(bodies[0].client_message_id).not.toBe(bodies[1].client_message_id)
  })

  it('404 on step: → toast and path refetch (PF-10)', async () => {
    let pathCalls = 0
    server.use(
      http.get('/api/path', () => {
        pathCalls++
        return HttpResponse.json(f.path())
      }),
      http.post('/api/chat/messages', () => errorBody(404, 'NOT_FOUND', 'Такого шага в твоём пути нет')),
    )
    await openPanel()
    const before = pathCalls
    await userEvent.click(within(panel()).getByRole('button', { name: 'Запросить доступ' }))
    expect(await screen.findByText('Такого шага в твоём пути нет', { selector: '.toast span' })).toBeInTheDocument()
    await waitFor(() => expect(pathCalls).toBeGreaterThan(before))
  })

  it('mark done: optimistic, server summary, toast «Отменить» reopens (PF-03)', async () => {
    const calls: Record<string, unknown>[] = []
    server.use(http.post('/api/path/steps/:id/status', async ({ request, params }) => {
      const b = (await request.json()) as { status: 'done' | 'in_progress' }
      calls.push({ id: params.id, ...b })
      await delay(30)
      const step = { ...f.path().steps[3], status: b.status }
      return HttpResponse.json({ step, changed: true, path_summary: summary(b.status === 'done' ? 3 : 2) })
    }))
    await openPanel()
    await userEvent.click(within(panel()).getByRole('button', { name: /Заведи конспект встреч/ }))
    await userEvent.click(within(panel()).getByRole('button', { name: 'Отметить пройденным' }))
    expect(within(panel()).getByRole('progressbar', { name: 'Готово на этапе' })).toHaveAttribute('aria-valuenow', '3')
    expect(await screen.findByText('Отмечено: «Заведи конспект встреч с наставником»')).toBeInTheDocument()
    expect(useMascot.getState().client).toBe('done')
    expect(calls[0]).toMatchObject({ id: 'cm-1.9', status: 'done', expected_status: 'not_started' })
    await userEvent.click(screen.getByRole('button', { name: 'Отменить' }))
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1]).toMatchObject({ status: 'in_progress', expected_status: 'done' })
  })

  it('409 STATUS_CONFLICT → rollback, refetch, toast «Шаг уже обновлён» (P-A8)', async () => {
    server.use(http.post('/api/path/steps/:id/status', () => errorBody(409, 'STATUS_CONFLICT', 'Статус изменился', { current_status: 'done' })))
    await openPanel()
    await userEvent.click(within(panel()).getByRole('button', { name: /Заведи конспект встреч/ }))
    await userEvent.click(within(panel()).getByRole('button', { name: 'Отметить пройденным' }))
    expect(await screen.findByText('Шаг уже обновлён')).toBeInTheDocument()
    expect(within(panel()).getByRole('progressbar', { name: 'Готово на этапе' })).toHaveAttribute('aria-valuenow', '2')
  })

  it('network failure → rollback, «Не сохранилось · Повторить» with the same key (P-A9)', async () => {
    const keys: string[] = []
    let fail = true
    server.use(http.post('/api/path/steps/:id/status', async ({ request }) => {
      const b = (await request.json()) as { idempotency_key: string }
      keys.push(b.idempotency_key)
      if (fail) return HttpResponse.error()
      return HttpResponse.json({ step: { ...f.path().steps[3], status: 'done' }, changed: true, path_summary: summary(3) })
    }))
    await openPanel()
    await userEvent.click(within(panel()).getByRole('button', { name: /Заведи конспект встреч/ }))
    await userEvent.click(within(panel()).getByRole('button', { name: 'Отметить пройденным' }))
    expect(await screen.findByText('Не сохранилось')).toBeInTheDocument()
    expect(within(panel()).getByRole('progressbar', { name: 'Готово на этапе' })).toHaveAttribute('aria-valuenow', '2')
    expect(useMascot.getState().client).toBe('oops')
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    await waitFor(() => expect(keys).toHaveLength(2))
    expect(keys[0]).toBe(keys[1])
  })

  it('other errors → toast with the server message', async () => {
    server.use(http.post('/api/path/steps/:id/status', () => errorBody(409, 'TRANSITION_NOT_ALLOWED', 'Этот шаг закроется сам, когда заявку одобрят')))
    await openPanel()
    await userEvent.click(within(panel()).getByRole('button', { name: /Познакомься с командой/ }))
    await userEvent.click(within(panel()).getByRole('button', { name: 'Вернуть в работу' }))
    expect(await screen.findByText(/закроется сам/)).toBeInTheDocument()
  })

  it('reopen success shows a toast', async () => {
    server.use(http.post('/api/path/steps/:id/status', () => HttpResponse.json({ step: { ...f.path().steps[0], status: 'in_progress' }, changed: true, path_summary: summary(1) })))
    await openPanel()
    await userEvent.click(within(panel()).getByRole('button', { name: /Познакомься с командой/ }))
    await userEvent.click(within(panel()).getByRole('button', { name: 'Вернуть в работу' }))
    expect(await screen.findByText('Шаг «Познакомься с командой» снова в работе')).toBeInTheDocument()
  })

  it('waiting step row offers «Статус заявки»; next click reports a client event', async () => {
    const events: unknown[] = []
    server.use(http.post('/api/client-events', async ({ request }) => {
      events.push(await request.json())
      return new HttpResponse(null, { status: 204 })
    }))
    await openPanel()
    await userEvent.click(within(panel()).getByRole('button', { name: /Запроси доступ в пространство/ }))
    expect(within(panel()).getByRole('button', { name: 'Статус заявки' })).toBeInTheDocument()
    await userEvent.click(within(within(panel()).getByRole('region', { name: 'Что дальше' })).getByRole('button'))
    await waitFor(() => expect(events).toEqual([{ name: 'next_step_clicked', step_id: 'cm-1.7' }]))
  })

  it('accordion toggles periods; «Обновить» refetches', async () => {
    await openPanel()
    const w2 = within(panel()).getByRole('button', { name: /Неделя 2/ })
    await userEvent.click(w2)
    expect(w2).toHaveAttribute('aria-expanded', 'true')
    expect(within(panel()).getByText('Сформулируй вопрос наставнику о плане')).toBeInTheDocument()
    await userEvent.click(within(panel()).getByRole('button', { name: 'Обновить' }))
  })

  it('polls only with pending requests and toasts rejection (XF-02, XF-04)', async () => {
    let n = 0
    server.use(
      http.get('/api/config', () => HttpResponse.json({ ...f.config, poll_interval_ms: 60 })),
      http.get('/api/path', () => {
        n++
        const base = f.path()
        if (n === 1) return HttpResponse.json(base)
        return HttpResponse.json(f.path({ has_pending_requests: false, steps: base.steps.map((s) => (s.step_id === 'cm-1.10' ? { ...s, status: 'in_progress' as const, request: { ...s.request!, status: 'rejected' as const, reject_reason: 'other_resource' } } : s)) }))
      }),
    )
    renderApp('/')
    expect(await screen.findByText(/отклонили: нужен другой ресурс/, { selector: '.toast span' }, { timeout: 3000 })).toBeInTheDocument()
    const settled = n
    await new Promise((r) => setTimeout(r, 300))
    expect(n).toBe(settled)
  })

  it('approval between polls → toast and Vitya done (XF-03)', async () => {
    let n = 0
    server.use(
      http.get('/api/config', () => HttpResponse.json({ ...f.config, poll_interval_ms: 60 })),
      http.get('/api/path', () => {
        n++
        const base = f.path()
        if (n === 1) return HttpResponse.json(base)
        return HttpResponse.json(f.path({ has_pending_requests: false, steps: base.steps.map((s) => (s.step_id === 'cm-1.10' ? { ...s, status: 'done' as const, request: { ...s.request!, status: 'approved' as const } } : s)) }))
      }),
    )
    renderApp('/')
    expect(await screen.findByText(/одобрен/, { selector: '.toast span' }, { timeout: 3000 })).toBeInTheDocument()
    expect(useMascot.getState().client).toBe('done')
  })
})

describe('stage celebration (09 §17.8, AG-34)', () => {
  // неделя 1: открыт только cm-1.9, остальные «Готово»
  const almost = (): PathView => {
    const base = f.path()
    return f.path({
      has_pending_requests: false,
      steps: base.steps.map((s) => (s.period === 'week1' && s.step_id !== 'cm-1.9' ? { ...s, status: 'done' as const, request: s.request ? { ...s.request, status: 'approved' as const } : null } : s)),
      waiting: [],
      next_step: { step_id: 'cm-1.9', reason: 'current' },
      progress: { period_done: 5, period_total: 6, overall_done: 5, overall_total: 7 },
    })
  }
  const celebration = 'Неделя 1 позади! 🎉 Обсуди её с наставником на 1:1'

  it('the last step of the week → toast once, Vitya done; «Отменить» and «Готово» again do not repeat it', async () => {
    const calls: string[] = []
    server.use(
      http.get('/api/path', () => HttpResponse.json(almost())),
      http.post('/api/path/steps/:id/status', async ({ request }) => {
        const b = (await request.json()) as { status: 'done' | 'in_progress' }
        calls.push(b.status)
        const step = { ...almost().steps[3], status: b.status }
        const done = b.status === 'done'
        return HttpResponse.json({ step, changed: true, path_summary: { state: 'ready', next_step: done ? { step_id: 'cm-2.1', reason: 'ahead' } : { step_id: 'cm-1.9', reason: 'current' }, waiting: [], progress: { period_done: done ? 6 : 5, period_total: 6, overall_done: done ? 6 : 5, overall_total: 7 } } })
      }),
    )
    renderApp('/')
    const p = await screen.findByRole('complementary', { name: 'Мой путь' })
    await within(p).findByText('Что дальше')
    await userEvent.click(await within(p).findByRole('button', { name: /Заведи конспект встреч/ }))
    await userEvent.click(within(p).getByRole('button', { name: 'Отметить пройденным' }))
    expect(await screen.findByText(celebration, { selector: '.toast span' })).toBeInTheDocument()
    expect(useMascot.getState().client).toBe('done')
    await userEvent.click(screen.getByRole('button', { name: 'Отменить' }))
    await waitFor(() => expect(calls).toEqual(['done', 'in_progress']))
    await userEvent.click(await within(panel()).findByRole('button', { name: 'Отметить пройденным' })) // строка шага ещё раскрыта
    await waitFor(() => expect(calls).toEqual(['done', 'in_progress', 'done']))
    await screen.findAllByText('Отмечено: «Заведи конспект встреч с наставником»', { selector: '.toast span' })
    expect(screen.getAllByText(celebration, { selector: '.toast span' })).toHaveLength(1)
  })

  it('a failed mark does not celebrate (optimistic update only)', async () => {
    server.use(
      http.get('/api/path', () => HttpResponse.json(almost())),
      http.post('/api/path/steps/:id/status', () => errorBody(400, 'BAD_REQUEST', 'Нельзя')),
    )
    renderApp('/')
    const p = await screen.findByRole('complementary', { name: 'Мой путь' })
    await userEvent.click(await within(p).findByRole('button', { name: /Заведи конспект встреч/ }))
    await userEvent.click(within(p).getByRole('button', { name: 'Отметить пройденным' }))
    expect(await screen.findByText(/Нельзя/, { selector: '.toast span' })).toBeInTheDocument()
    expect(screen.queryByText(celebration, { selector: '.toast span' })).toBeNull()
  })

  it('approval of the last request of the week celebrates too', async () => {
    let n = 0
    const base = f.path()
    const waitingLast = f.path({
      steps: base.steps.map((s) => (s.period === 'week1' && s.step_id !== 'cm-1.10' ? { ...s, status: 'done' as const } : s)),
      next_step: { step_id: 'cm-2.1', reason: 'ahead' },
    })
    server.use(
      http.get('/api/config', () => HttpResponse.json({ ...f.config, poll_interval_ms: 60 })),
      http.get('/api/path', () => {
        n++
        if (n === 1) return HttpResponse.json(waitingLast)
        return HttpResponse.json(f.path({ ...waitingLast, has_pending_requests: false, steps: waitingLast.steps.map((s) => (s.step_id === 'cm-1.10' ? { ...s, status: 'done' as const, request: { ...s.request!, status: 'approved' as const } } : s)) }))
      }),
    )
    renderApp('/')
    expect(await screen.findByText(celebration, { selector: '.toast span' }, { timeout: 3000 })).toBeInTheDocument()
    expect(useMascot.getState().client).toBe('done')
  })
})

describe('mentor read-only screens (MP-08)', () => {
  beforeEach(() => server.use(http.get('/api/me', () => HttpResponse.json(f.meMentor))))

  it('«Мои стажёры» lists progress, pending requests and upcoming time off (AC-P20, AC-P27)', async () => {
    renderApp('/mentor/interns')
    expect(await screen.findByText('Аня Смирнова')).toBeInTheDocument()
    expect(screen.getByText('Заявок на доступ: 1')).toBeInTheDocument()
    expect(screen.getByText('Ближайший отгул: 30.11–01.12')).toBeInTheDocument()
    expect(screen.getByText('тестовый трек')).toBeInTheDocument()
    expect(screen.getByText('Отгулов не запланировано')).toBeInTheDocument()
  })

  it('empty and error states', async () => {
    server.use(http.get('/api/mentor/interns', () => HttpResponse.json({ items: [] })))
    const a = renderApp('/mentor/interns')
    expect(await screen.findByText('Стажёров пока нет')).toBeInTheDocument()
    a.unmount()
    server.use(http.get('/api/mentor/interns', () => errorBody(503, 'UNAVAILABLE', 'Недоступно')))
    renderApp('/mentor/interns')
    expect(await screen.findByText(/Недоступно/)).toBeInTheDocument()
  })

  it('intern path is read-only: no action buttons (PF-06)', async () => {
    renderApp('/mentor/interns/u-intern-be')
    expect(await screen.findByText('Что дальше')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Запросить доступ' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Заведи конспект встреч/ }))
    expect(screen.queryByRole('button', { name: 'Отметить пройденным' })).not.toBeInTheDocument()
  })

  it('foreign intern → 404 text; other errors → retry', async () => {
    server.use(http.get('/api/mentor/interns/:id/path', () => errorBody(404, 'NOT_FOUND', 'Не найдено')))
    const a = renderApp('/mentor/interns/u-intern-other')
    expect(await screen.findByText('Такого стажёра нет среди твоих')).toBeInTheDocument()
    a.unmount()
    server.use(http.get('/api/mentor/interns/:id/path', () => errorBody(503, 'UNAVAILABLE', 'Сбой')))
    renderApp('/mentor/interns/u-intern-be')
    expect(await screen.findByText(/Не получилось загрузить путь/)).toBeInTheDocument()
  })
})
