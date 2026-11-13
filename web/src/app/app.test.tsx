import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import { api } from '@/shared/api/client'
import { describeMode } from './ModeIndicator'

describe('login and shell (wave 2)', () => {
  it('no session → login screen with test accounts; login in one click goes home', async () => {
    let me = false
    server.use(
      http.get('/api/me', () => (me ? HttpResponse.json(f.meIntern) : errorBody(401, 'UNAUTHORIZED', 'Нет сессии'))),
      http.post('/api/auth/login', async ({ request }) => {
        expect(await request.json()).toEqual({ user_id: 'u-intern-be' })
        me = true
        return HttpResponse.json(f.meIntern)
      }),
    )
    const { history } = renderApp('/')
    expect(await screen.findByRole('heading', { name: 'Войти тестовой учёткой' })).toBeInTheDocument()
    expect(screen.queryByText(/Сессия истекла/)).not.toBeInTheDocument()
    const item = (await screen.findByText('Аня Смирнова')).closest('li')!
    await userEvent.click(within(item).getByRole('button', { name: 'Войти' }))
    await waitFor(() => expect(history.location.pathname).toBe('/'))
    expect(await screen.findByLabelText('Чат с Витей')).toBeInTheDocument()
    expect(screen.getByText('Режим demo')).toBeInTheDocument()
  })

  it('mentor logs in and lands on «Мои стажёры»', async () => {
    server.use(http.post('/api/auth/login', () => HttpResponse.json(f.meMentor)), http.get('/api/me', () => HttpResponse.json(f.meMentor)))
    const { history } = renderApp('/login')
    const item = (await screen.findByText('Олег Наставников')).closest('li')!
    await userEvent.click(within(item).getByRole('button', { name: 'Войти' }))
    await waitFor(() => expect(history.location.pathname).toBe('/mentor/interns'))
    expect(screen.getByRole('link', { name: /Входящие/ })).toBeInTheDocument()
  })

  it('intern routes redirect a mentor and mentor routes redirect an intern', async () => {
    server.use(http.get('/api/me', () => HttpResponse.json(f.meMentor)))
    const a = renderApp('/requests')
    await waitFor(() => expect(a.history.location.pathname).toBe('/mentor/interns'))
    a.unmount()
    server.use(http.get('/api/me', () => HttpResponse.json(f.meIntern)))
    const b = renderApp('/mentor/inbox')
    await waitFor(() => expect(b.history.location.pathname).toBe('/'))
  })

  it('login error and accounts error are shown', async () => {
    server.use(
      http.get('/api/me', () => errorBody(401, 'UNAUTHORIZED', 'Нет')),
      http.post('/api/auth/login', () => errorBody(404, 'NOT_FOUND', 'Учётка не найдена')),
    )
    renderApp('/login')
    const item = (await screen.findByText('Аня Смирнова')).closest('li')!
    await userEvent.click(within(item).getByRole('button', { name: 'Войти' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Учётка не найдена')
  })

  it('accounts failure has «Повторить»', async () => {
    server.use(http.get('/api/me', () => errorBody(401, 'UNAUTHORIZED', 'Нет')), http.get('/api/auth/accounts', () => errorBody(503, 'UNAVAILABLE', 'Сервис недоступен')))
    renderApp('/login')
    expect(await screen.findByText(/Сервис недоступен/)).toBeInTheDocument()
    server.use(http.get('/api/auth/accounts', () => HttpResponse.json({ items: f.accounts })))
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    expect(await screen.findByText('Аня Смирнова')).toBeInTheDocument()
  })

  it('session expires mid-work → login with a toast (P-A23)', async () => {
    const { history } = renderApp('/')
    await screen.findByLabelText('Чат с Витей')
    server.use(http.get('/api/me', () => errorBody(401, 'UNAUTHORIZED', 'Нет')), http.get('/api/path', () => errorBody(401, 'UNAUTHORIZED', 'Нет')))
    await api('/api/path').catch(() => undefined)
    await waitFor(() => expect(history.location.pathname).toBe('/login'))
    expect(await screen.findByText('Сессия истекла — войди снова')).toBeInTheDocument()
  })

  it('guard shows an error state for non-401 failures', async () => {
    server.use(http.get('/api/me', () => errorBody(503, 'UNAVAILABLE', 'API недоступен')))
    renderApp('/')
    expect(await screen.findByText('API недоступен')).toBeInTheDocument()
  })

  it('logout clears the session and opens login', async () => {
    const { history } = renderApp('/')
    await screen.findByLabelText('Чат с Витей')
    server.use(http.get('/api/me', () => errorBody(401, 'UNAUTHORIZED', 'Нет')))
    await userEvent.click(screen.getByRole('button', { name: 'Выйти' }))
    await waitFor(() => expect(history.location.pathname).toBe('/login'))
  })

  it('demo panel: reset and flags', async () => {
    let reset = 0
    let flagBody: unknown
    let pathCalls = 0
    server.use(
      http.get('/api/path', () => {
        pathCalls++
        return HttpResponse.json(pathCalls > 1 ? f.path({ progress: { period_done: 0, period_total: 6, overall_done: 0, overall_total: 7 } }) : f.path())
      }),
      http.post('/api/demo/reset', () => {
        reset++
        return new HttpResponse(null, { status: 204 })
      }),
      http.post('/api/demo/flags', async ({ request }) => {
        flagBody = await request.json()
        return HttpResponse.json({ name: 'mock_absences_fault', value: 'unavailable', allowed_values: ['off', 'unavailable'] })
      }),
    )
    renderApp('/')
    await userEvent.click(await screen.findByRole('button', { name: 'Демо' }))
    const panel = await screen.findByRole('dialog', { name: 'Демо-инструменты' })
    await userEvent.click(within(panel).getByRole('button', { name: 'Сбросить демо-данные' }))
    expect(await screen.findByText(/Демо-данные сброшены/)).toBeInTheDocument()
    expect(reset).toBe(1)
    // открытая панель «Мой путь» перечитывает путь сразу, без «Обновить»
    await waitFor(() => expect(screen.getByRole('progressbar', { name: 'Готово на этапе' })).toHaveAttribute('aria-valuenow', '0'))
    const select = await within(panel).findByLabelText('Сбой сервиса отсутствий')
    await userEvent.selectOptions(select, 'unavailable')
    expect(await screen.findByText('Сбой сервиса отсутствий: недоступен')).toBeInTheDocument()
    expect(flagBody).toEqual({ name: 'mock_absences_fault', value: 'unavailable' })
    await userEvent.click(within(panel).getByRole('button', { name: 'Закрыть' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('demo panel errors become toasts', async () => {
    server.use(
      http.post('/api/demo/reset', () => errorBody(503, 'UNAVAILABLE', 'Имитации недоступны')),
      http.post('/api/demo/flags', () => errorBody(400, 'BAD_REQUEST', 'Нельзя')),
      http.get('/api/demo/flags', () => errorBody(503, 'UNAVAILABLE', 'Флаги недоступны')),
    )
    renderApp('/')
    await userEvent.click(await screen.findByRole('button', { name: 'Демо' }))
    await userEvent.click(screen.getByRole('button', { name: 'Сбросить демо-данные' }))
    expect(await screen.findByText(/Имитации недоступны/)).toBeInTheDocument()
    expect(await screen.findByText(/Флаги недоступны/)).toBeInTheDocument()
  })

  it('no demo panel when demo tools are disabled', async () => {
    server.use(http.get('/api/config', () => HttpResponse.json({ ...f.config, demo_tools_enabled: false })))
    renderApp('/')
    await screen.findByLabelText('Чат с Витей')
    expect(screen.queryByRole('button', { name: 'Демо' })).not.toBeInTheDocument()
  })

  it('LLM mode indicator uses /api/llm/health', async () => {
    server.use(
      http.get('/api/config', () => HttpResponse.json({ ...f.config, llm_provider: 'openai_compatible' })),
      http.get('/api/llm/health', () => HttpResponse.json({ ...f.llmHealth, provider: 'openai_compatible', status: 'ok', model: 'gpt-x' })),
    )
    renderApp('/')
    expect(await screen.findByText('LLM · gpt-x')).toBeInTheDocument()
  })
})

describe('describeMode', () => {
  const cfg = { ...f.config, llm_provider: 'openai_compatible' as const }
  const h = (status: typeof f.llmHealth.status) => ({ ...f.llmHealth, provider: 'openai_compatible' as const, status })
  it.each([
    ['auth_error', 'Упрощённый режим', /LLM_API_KEY/],
    ['misconfigured', 'Упрощённый режим', /LLM_MODEL/],
    ['disabled', 'Упрощённый режим', /флагом/],
    ['circuit_open', 'LLM · сбои', /ошибками/],
    ['degraded', 'LLM · сбои', /ошибками/],
  ] as const)('%s', (status, label, title) => {
    const m = describeMode(cfg, h(status))
    expect(m.label).toBe(label)
    expect(m.title).toMatch(title)
  })
  it('misconfigured by an unknown LLM_PROVIDER names the variable', () => {
    const m = describeMode(cfg, { ...h('misconfigured'), last_error_code: 'invalid_LLM_PROVIDER' })
    expect(m.label).toBe('Упрощённый режим')
    expect(m.title).toMatch(/LLM_PROVIDER: допустимы demo и openai_compatible/)
  })
  it('unknown health while loading', () => {
    expect(describeMode(cfg, undefined).title).toMatch(/Проверяю/)
    expect(describeMode(undefined, undefined).label).toBe('Режим demo')
  })
})

describe('LLM notice icon (wave 8)', () => {
  it('stays next to the mode indicator after an llm_* notice', async () => {
    const { lastLlmNotice } = await import('./ModeIndicator')
    expect(lastLlmNotice([[], ['pii_masked']])).toBeNull()
    expect(lastLlmNotice([['llm_auth_error'], ['simplified_mode', 'pii_masked']])).toBe('simplified_mode')
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string }
      return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, mode: 'fallback', notices: ['llm_auth_error'], text: 'Ответ по правилам' }))
    }))
    renderApp('/')
    await userEvent.type(await screen.findByLabelText('Сообщение Вите'), 'привет{Enter}')
    expect(await screen.findByRole('img', { name: /Ключ LLM не принят/ })).toBeInTheDocument()
    expect(screen.getByText(/Ключ LLM не принят — работаю в упрощённом режиме/, { selector: '.plaque' })).toBeInTheDocument()
  })
})
