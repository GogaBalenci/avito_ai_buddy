import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import type { AgentReply, PendingAction } from '@/shared/api/schemas'
import { policyHint } from '@/features/actions/PendingActionCard'
import { requestStatus, requestTitle } from './requests'

const chat = () => screen.getByRole('region', { name: 'Чат с Витей' })
const ACC_ID = '77777777-7777-4777-8777-777777777777'
const accessDraft: PendingAction = {
  action_id: ACC_ID, kind: 'access', state: 'active', expires_at: '2026-11-12T09:15:00Z',
  summary: { resource_id: 'team-channels', title: 'Ключевые каналы и чаты команды', access_level: 'write', approver: 'Наставник (тестовый)', effective_policy: 'mentor', justification: 'Шаг 1.7 чек-листа адаптации', step_id: 'cm-1.7' },
}

function accessReply(over: Partial<AgentReply> = {}) {
  server.use(http.post('/api/chat/messages', async ({ request }) => {
    const b = (await request.json()) as { client_message_id: string }
    return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, reply_id: crypto.randomUUID(), intent: 'access_request', text: 'Проверь: заявка на «Ключевые каналы и чаты команды».', pending_action: accessDraft, actions: [{ type: 'confirm', label: 'Подтвердить', action_id: ACC_ID }, { type: 'cancel', label: 'Отмена', action_id: ACC_ID }], ...over }))
  }))
}

async function askAccess() {
  renderApp('/')
  await within(await screen.findByRole('complementary', { name: 'Мой путь' })).findByText('Что дальше')
  await userEvent.click(within(screen.getByRole('region', { name: 'Что дальше' })).getByRole('button', { name: 'Запросить доступ' }))
  return within(chat()).findByRole('generic', { name: 'Черновик заявки' })
}

describe('«Мои заявки» and /requests (wave 5)', () => {
  it('panel block lists upcoming requests and opens the form via intent:time_off', async () => {
    const sent: { payload?: string }[] = []
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string; payload?: string }
      sent.push(b)
      return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, reply_id: crypto.randomUUID() }))
    }))
    renderApp('/')
    const block = await screen.findByRole('region', { name: 'Мои заявки' })
    expect(await within(block).findByText('Отгул 30.11–01.12 · 2 рабочих дня')).toBeInTheDocument()
    expect(within(block).getByText('Оформлена')).toBeInTheDocument()
    expect(within(block).getByText('Ждёт наставника')).toBeInTheDocument()
    await userEvent.click(within(block).getByRole('button', { name: 'Оформить отгул' }))
    await waitFor(() => expect(sent[0]?.payload).toBe('intent:time_off'))
  })

  it('panel block: empty, stale and error', async () => {
    server.use(http.get('/api/requests', () => HttpResponse.json({ items: [], total: 0, stale: true })))
    const a = renderApp('/')
    const block = await screen.findByRole('region', { name: 'Мои заявки' })
    expect(await within(block).findByText('Активных заявок нет')).toBeInTheDocument()
    expect(within(block).getByText(/показаны сохранённые данные/)).toBeInTheDocument()
    a.unmount()
    server.use(http.get('/api/requests', () => errorBody(503, 'UNAVAILABLE', 'Заявки недоступны')))
    renderApp('/')
    expect(await screen.findByText(/Заявки недоступны/)).toBeInTheDocument()
  })

  it('/requests: list with filters, empty and error states', async () => {
    const kinds: (string | null)[] = []
    server.use(http.get('/api/requests', ({ request }) => {
      const k = new URL(request.url).searchParams.get('kind')
      kinds.push(k)
      return HttpResponse.json(k === 'access' ? { items: [], total: 0 } : { items: [f.requestAbs, f.requestAcc], total: 2 })
    }))
    renderApp('/requests')
    expect(await screen.findByText('Отгул 30.11–01.12 · 2 рабочих дня')).toBeInTheDocument()
    expect(screen.getByText('Доступ: Пространство кластера · чтение')).toBeInTheDocument()
    expect(screen.getByText(/создана 10\.11 в 12:00/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Доступы' }))
    expect(await screen.findByText(/Заявок пока нет/)).toBeInTheDocument()
    expect(kinds).toContain('access')
  })

  it('/requests error has retry', async () => {
    server.use(http.get('/api/requests', () => errorBody(503, 'UNAVAILABLE', 'нет')))
    renderApp('/requests')
    expect(await screen.findByText(/Не получилось загрузить заявки/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Повторить' })).toBeInTheDocument()
  })

  it('status and title helpers', () => {
    expect(requestStatus({ ...f.requestAcc, status: 'approved' }).text).toBe('Одобрена')
    expect(requestStatus({ ...f.requestAcc, status: 'rejected', reject_reason: 'not_needed' }).text).toBe('Отклонена: доступ сейчас не нужен')
    expect(requestStatus({ ...f.requestAcc, status: 'rejected' }).text).toBe('Отклонена')
    expect(requestStatus({ ...f.requestAbs, status: 'cancelled' }).text).toBe('Отменена')
    expect(requestStatus({ ...f.requestAcc, auto_approve_at: '2026-11-12T09:02:00Z' }).text).toBe('Одобрится около 12:02')
    expect(requestTitle({ ...f.requestAcc, resource_title: undefined, access_level: undefined })).toBe('Доступ: space-cluster')
    expect(requestTitle({ ...f.requestAbs, working_days: undefined })).toBe('Отгул 30.11–01.12')
    expect(policyHint('auto')).toBe('Одобряется сразу')
    expect(policyHint('timer', 2)).toBe('Одобрится автоматически примерно через 2 мин')
    expect(policyHint('timer')).toMatch(/2 мин/)
    expect(policyHint(undefined)).toBe('')
  })
})

describe('access draft card (XF-01, XF-09)', () => {
  it('shows resource, level, approver, policy hint, justification and test plaque; confirm appends reply', async () => {
    accessReply()
    server.use(http.post(`/api/actions/${ACC_ID}/confirm`, () => HttpResponse.json({
      result: { ...f.requestAcc, request_id: 'ACC-000102', resource_title: 'Ключевые каналы и чаты команды' },
      reply: f.reply({ mascot_state: 'done', text: 'Заявка №ACC-000102 у наставника. Как только он одобрит, шаг закроется сам.' }),
    })))
    const card = await askAccess()
    expect(within(card).getByText('Проверь: Заявка на «Ключевые каналы и чаты команды»')).toBeInTheDocument()
    expect(within(card).getByText('запись')).toBeInTheDocument()
    expect(within(card).getByText('Наставник (тестовый)')).toBeInTheDocument()
    expect(within(card).getByText('Заявку одобряет наставник')).toBeInTheDocument()
    expect(within(card).getByText('Шаг 1.7 чек-листа адаптации')).toBeInTheDocument()
    expect(within(card).getByText(/Тестовый порядок, не регламент Авито/)).toBeInTheDocument()
    await userEvent.click(within(card).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(chat()).findByText(/ACC-000102 у наставника/)).toBeInTheDocument()
    expect(within(card).getByText('Заявка №ACC-000102')).toBeInTheDocument()
  })

  it('confirm 503 → retry text; 410 → expired; cancel works', async () => {
    accessReply()
    let n = 0
    server.use(
      http.post(`/api/actions/${ACC_ID}/confirm`, () => (++n === 1 ? errorBody(503, 'UNAVAILABLE', 'нет') : errorBody(410, 'ACTION_EXPIRED', 'истёк'))),
    )
    const card = await askAccess()
    await userEvent.click(within(card).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(card).findByText(/Повтор не создаст дубликат/)).toBeInTheDocument()
    await userEvent.click(within(card).getByRole('button', { name: 'Повторить' }))
    expect(await within(card).findByText('Черновик устарел — запроси ещё раз')).toBeInTheDocument()
  })

  it('confirm 403 shows the message; cancel → cancelled', async () => {
    accessReply()
    server.use(
      http.post(`/api/actions/${ACC_ID}/confirm`, () => errorBody(403, 'PERMISSION_DENIED', 'Этот доступ стажёрам не выдаётся')),
      http.post(`/api/actions/${ACC_ID}/cancel`, () => HttpResponse.json({ ...accessDraft, state: 'cancelled', result: null })),
    )
    const card = await askAccess()
    await userEvent.click(within(card).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(card).findByText(/Этот доступ стажёрам не выдаётся/)).toBeInTheDocument()
    await userEvent.click(within(card).getByRole('button', { name: 'Отмена' }))
    expect(await within(card).findByText('Черновик отменён — ничего не отправлено')).toBeInTheDocument()
  })

  it('cancel errors: 410 → expired, other → message', async () => {
    accessReply()
    server.use(http.post(`/api/actions/${ACC_ID}/cancel`, () => errorBody(410, 'ACTION_REPLACED', 'заменён')))
    const card = await askAccess()
    await userEvent.click(within(card).getByRole('button', { name: 'Отмена' }))
    expect(await within(card).findByText('Черновик устарел — запроси ещё раз')).toBeInTheDocument()
  })

  it('history draft in replaced state has no buttons', async () => {
    server.use(
      http.get('/api/chat/history', () => HttpResponse.json({ items: [{ id: '88888888-8888-4888-8888-888888888888', ts: '2026-11-12T08:00:00Z', author: 'bot', reply: f.reply({ reply_id: '88888888-8888-4888-8888-888888888888', text: 'Черновик доступа', pending_action: accessDraft }) }] })),
      http.get(`/api/actions/${ACC_ID}`, () => HttpResponse.json({ ...accessDraft, state: 'replaced', result: null })),
    )
    renderApp('/')
    expect(await screen.findByText('Черновик заменён новым')).toBeInTheDocument()
    expect(within(chat()).queryByRole('button', { name: 'Подтвердить' })).toBeNull()
  })

  it('history draft state failure is shown', async () => {
    server.use(
      http.get('/api/chat/history', () => HttpResponse.json({ items: [{ id: '88888888-8888-4888-8888-888888888888', ts: '2026-11-12T08:00:00Z', author: 'bot', reply: f.reply({ reply_id: '88888888-8888-4888-8888-888888888888', text: 'Черновик доступа', pending_action: accessDraft }) }] })),
      http.get(`/api/actions/${ACC_ID}`, () => errorBody(503, 'UNAVAILABLE', 'нет')),
    )
    renderApp('/')
    expect(await screen.findByText(/Состояние черновика не загрузилось/)).toBeInTheDocument()
  })
})
