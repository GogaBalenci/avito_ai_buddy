import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import type { AgentReply, PendingAction, RequestView } from '@/shared/api/schemas'
import { useMascot } from '@/shared/mascot/mascotStore'

const chat = () => screen.getByRole('region', { name: 'Чат с Витей' })
const ACTION_ID = '44444444-4444-4444-8444-444444444444'

const formReply = (over: Partial<AgentReply> = {}): Partial<AgentReply> => ({
  intent: 'time_off',
  text: 'Отгул оформляется по тестовому порядку [1]: выбираешь даты, я создаю заявку в тестовом сервисе отсутствий. Выбери даты.',
  sources: [{ n: 1, slug: 'time-off-test-order', title: 'Порядок оформления отгула', url: '/kb/time-off-test-order', label: 'test_instruction', source_level: 'team' }],
  actions: [{ type: 'open_form', label: 'Выбрать даты', form: 'time_off', prefill: null, existing: [] }],
  ...over,
})

const draft = (over: Partial<PendingAction> = {}): PendingAction => ({
  action_id: ACTION_ID,
  kind: 'time_off',
  state: 'active',
  expires_at: '2026-11-10T09:15:00Z',
  summary: { date_from: '2026-11-13', date_to: '2026-11-13', working_days: 1 },
  ...over,
})

const done: RequestView = { kind: 'time_off', request_id: 'ABS-000123', status: 'registered', created_at: '2026-11-10T09:01:00Z', date_from: '2026-11-13', date_to: '2026-11-13', working_days: 1 }

function chatReplies(...replies: Partial<AgentReply>[]) {
  const sent: { client_message_id: string; payload?: string; text?: string }[] = []
  let i = 0
  server.use(http.post('/api/chat/messages', async ({ request }) => {
    const b = (await request.json()) as { client_message_id: string; payload?: string; text?: string }
    sent.push(b)
    const over = replies[Math.min(i++, replies.length - 1)]
    return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, reply_id: crypto.randomUUID(), ...over }))
  }))
  return sent
}

async function openForm(over: Partial<AgentReply> = {}) {
  const sent = chatReplies(formReply(over))
  const r = renderApp('/')
  await within(await screen.findByRole('region', { name: 'Чат с Витей' })).findByTestId('greeting')
  await userEvent.click(within(chat()).getAllByRole('button', { name: 'Оформить отгул' })[0])
  const form = await within(chat()).findByRole('form', { name: 'Даты отгула' })
  return { ...r, sent, form }
}

const fromInput = (form: HTMLElement) => within(form).getByLabelText('От') as HTMLInputElement
const toInput = (form: HTMLElement) => within(form).getByLabelText('До') as HTMLInputElement

describe('S3 time off form (wave 5)', () => {
  it('«Оформить отгул» sends intent:time_off with a new UUID; form has no min/max, noValidate, no reason field, test plaque (F-14, F-15, F-11)', async () => {
    const { sent, form } = await openForm()
    expect(sent[0].payload).toBe('intent:time_off')
    expect(form).toHaveAttribute('novalidate')
    expect(fromInput(form)).not.toHaveAttribute('min')
    expect(toInput(form)).not.toHaveAttribute('max')
    expect(within(form).queryByLabelText(/Причина/i)).toBeNull()
    expect(within(form).getByText(/Тестовый порядок, не регламент Авито/)).toBeInTheDocument()
    expect(within(form).getByText(/До 5 рабочих дней, начало и конец — в рабочий день, не позже 08\.02\.2027/)).toBeInTheDocument()
    expect(within(form).getByRole('button', { name: 'Проверить' })).toBeDisabled()
  })

  it('«до» follows «от»; caption shows working days; errors in caption with aria (F-03, F-04, F-14)', async () => {
    const { form } = await openForm()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-13' } })
    expect(toInput(form).value).toBe('2026-11-13')
    expect(within(form).getByText('1 рабочий день, выходные не считаются')).toBeInTheDocument()
    fireEvent.change(toInput(form), { target: { value: '2026-11-16' } })
    expect(within(form).getByText('2 рабочих дня, выходные не считаются')).toBeInTheDocument()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-09' } })
    const cap = within(form).getByText('Дата уже прошла — выбери сегодня или позже')
    expect(cap.className).toContain('error')
    expect(fromInput(form)).toHaveAttribute('aria-invalid', 'true')
    expect(fromInput(form).getAttribute('aria-describedby')).toBe(cap.id)
    expect(useMascot.getState().client).not.toBe('oops')
  })

  it('existing requests hint (F-17)', async () => {
    const { form } = await openForm({ actions: [{ type: 'open_form', label: 'Выбрать даты', form: 'time_off', existing: [{ request_id: 'ABS-000101', date_from: '2026-11-30', date_to: '2026-12-01' }] }] })
    expect(within(form).getByText('У тебя уже есть отгул 30.11–01.12')).toBeInTheDocument()
  })

  it('prefill from the model: hint and immediate errors', async () => {
    const { form } = await openForm({ actions: [{ type: 'open_form', label: 'Выбрать даты', form: 'time_off', prefill: { date_from: '2026-11-14', date_to: '2026-11-14' } }] })
    expect(within(form).getByText('Подставил даты из сообщения — проверь')).toBeInTheDocument()
    expect(within(form).getByText(/14\.11 — суббота/)).toBeInTheDocument()
  })

  it('submit with client errors does not call the server and focuses the field', async () => {
    let calls = 0
    server.use(http.post('/api/actions', () => {
      calls++
      return HttpResponse.json({}, { status: 500 })
    }))
    const { form } = await openForm()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-14' } })
    await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
    await waitFor(() => expect(document.activeElement).toBe(fromInput(form)))
    expect(calls).toBe(0)
  })

  it('happy path: draft → confirm → done card, copy text, requests invalidated (AC main, F-09, F-10)', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    let created: unknown
    let requestsCalls = 0
    server.use(
      http.post('/api/actions', async ({ request }) => {
        created = await request.json()
        return HttpResponse.json({ pending_action: draft(), notices: ['overlap_unchecked'] }, { status: 201 })
      }),
      http.post(`/api/actions/${ACTION_ID}/confirm`, () =>
        HttpResponse.json({ result: done, reply: f.reply({ mascot_state: 'done', intent: 'time_off', text: 'Готово', actions: [{ type: 'copy', label: 'Скопировать сообщение наставнику', text: 'Привет! Оформил(а) отгул на 13.11 (заявка ABS-000123).' }] }) }),
      ),
      http.get('/api/requests', ({ request }) => {
        requestsCalls++
        const upcoming = new URL(request.url).searchParams.get('scope') === 'upcoming'
        return HttpResponse.json({ items: requestsCalls > 1 && upcoming ? [done, f.requestAbs] : [f.requestAbs], total: 1 })
      }),
    )
    const { form } = await openForm()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-13' } })
    await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
    const card = await within(chat()).findByRole('generic', { name: 'Черновик заявки на отгул' })
    expect(created).toEqual({ kind: 'time_off', params: { date_from: '2026-11-13', date_to: '2026-11-13' } })
    expect(within(card).getByText('Проверь: отгул 13.11, 1 рабочий день.')).toBeInTheDocument()
    expect(within(card).getByText(/Ничего не отправлю, пока не нажмёшь «Подтвердить»/)).toBeInTheDocument()
    expect(within(card).getByText('Пересечения с другими отгулами проверю при отправке')).toBeInTheDocument()
    await userEvent.click(within(card).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(chat()).findByText(/Готово: заявка №ABS-000123/)).toBeInTheDocument()
    expect(useMascot.getState().server).toBe('done')
    await userEvent.click(within(chat()).getByRole('button', { name: 'Скопировать сообщение наставнику' }))
    expect(writeText).toHaveBeenCalledWith('Привет! Оформил(а) отгул на 13.11 (заявка ABS-000123).')
    const myreq = screen.getByRole('region', { name: 'Мои заявки' })
    expect(await within(myreq).findByText('Отгул 13.11 · 1 рабочий день')).toBeInTheDocument()
  })

  it('«Изменить» returns to the form with dates; «Отмена» cancels the draft', async () => {
    let cancelled = 0
    server.use(
      http.post('/api/actions', () => HttpResponse.json({ pending_action: draft(), notices: [] }, { status: 201 })),
      http.post(`/api/actions/${ACTION_ID}/cancel`, () => {
        cancelled++
        return HttpResponse.json({ ...draft({ state: 'cancelled' }), result: null })
      }),
    )
    const { form } = await openForm()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-13' } })
    await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
    await userEvent.click(await within(chat()).findByRole('button', { name: 'Изменить' }))
    const again = await within(chat()).findByRole('form', { name: 'Даты отгула' })
    expect(fromInput(again).value).toBe('2026-11-13')
    await userEvent.click(within(again).getByRole('button', { name: 'Проверить' }))
    await userEvent.click(await within(chat()).findByRole('button', { name: 'Отмена' }))
    expect(await within(chat()).findByText('Окей, ничего не отправляю')).toBeInTheDocument()
    expect(cancelled).toBe(1)
  })

  it('422 from server → error on the field, focus, OVERLAP link (F-05)', async () => {
    server.use(http.post('/api/actions', () => errorBody(422, 'VALIDATION_FAILED', 'Проверь даты', { errors: [{ field: 'range', code: 'OVERLAP', message: 'Пересекается с заявкой ABS-000101 на 30.11–01.12', params: { request_id: 'ABS-000101' } }] })))
    const { form } = await openForm()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-30' } })
    await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
    expect(await within(form).findByText('Пересекается с заявкой ABS-000101 на 30.11–01.12')).toBeInTheDocument()
    expect(within(form).getByRole('link', { name: /Посмотреть заявку ABS-000101/ })).toHaveAttribute('href', '/requests')
    await waitFor(() => expect(document.activeElement).toBe(toInput(form)))
    expect(toInput(form)).toHaveAttribute('aria-invalid', 'true')
  })

  it('422 on a date field focuses that field; 409 without field errors goes to range', async () => {
    server.use(http.post('/api/actions', () => errorBody(422, 'VALIDATION_FAILED', 'x', { errors: [{ field: 'date_from', code: 'DATE_IN_PAST', message: 'Дата уже прошла (сервер)' }] })))
    const { form } = await openForm()
    fireEvent.change(fromInput(form), { target: { value: '2026-11-13' } })
    await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
    expect(await within(form).findByText('Дата уже прошла (сервер)')).toBeInTheDocument()
    await waitFor(() => expect(document.activeElement).toBe(fromInput(form)))
    server.use(http.post('/api/actions', () => errorBody(409, 'DUPLICATE_ACTIVE', 'Уже есть активный черновик')))
    await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
    expect(await within(form).findByText('Уже есть активный черновик')).toBeInTheDocument()
  })

  it('submitting: buttons disabled, Vitya thinking, «дольше обычного» after 5 s; failure → «Повторить» (F-06)', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      let n = 0
      server.use(http.post('/api/actions', async () => {
        n++
        await delay(6000)
        return n === 1 ? errorBody(503, 'UNAVAILABLE', 'Сервис недоступен') : HttpResponse.json({ pending_action: draft(), notices: [] }, { status: 201 })
      }))
      const { form } = await openForm()
      fireEvent.change(fromInput(form), { target: { value: '2026-11-13' } })
      await userEvent.click(within(form).getByRole('button', { name: 'Проверить' }))
      expect(within(form).getByRole('button', { name: 'Проверить' })).toBeDisabled()
      expect(within(form).getByRole('button', { name: 'Отмена' })).toBeDisabled()
      expect(fromInput(form)).toBeDisabled()
      expect(within(chat()).getByRole('img', { name: 'Витя думает' })).toBeInTheDocument()
      await act(async () => {
        vi.advanceTimersByTime(5100)
      })
      expect(within(form).getByText('Сервис отвечает дольше обычного…')).toBeInTheDocument()
      await act(async () => {
        vi.advanceTimersByTime(1000)
      })
      expect(await within(form).findByText(/Не получилось проверить даты/)).toBeInTheDocument()
      expect(useMascot.getState().client).toBe('oops')
      await userEvent.click(within(form).getByRole('button', { name: 'Повторить' }))
      await act(async () => {
        vi.advanceTimersByTime(6100)
      })
      expect(await within(chat()).findByText(/Проверь: отгул/)).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  async function toDraft() {
    server.use(http.post('/api/actions', () => HttpResponse.json({ pending_action: draft(), notices: [] }, { status: 201 })))
    const r = await openForm()
    fireEvent.change(fromInput(r.form), { target: { value: '2026-11-13' } })
    await userEvent.click(within(r.form).getByRole('button', { name: 'Проверить' }))
    await within(chat()).findByText(/Проверь: отгул/)
    return r
  }

  it('503 on confirm → «Повторить» sends the same action_id (F-07)', async () => {
    const ids: string[] = []
    let fail = true
    server.use(http.post('/api/actions/:id/confirm', ({ params }) => {
      ids.push(String(params.id))
      return fail ? errorBody(503, 'UNAVAILABLE', 'нет') : HttpResponse.json({ result: done, reply: f.reply({ mascot_state: 'done', actions: [] }) })
    }))
    await toDraft()
    await userEvent.click(within(chat()).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(chat()).findByText('Сервис отсутствий не отвечает, заявка не создана')).toBeInTheDocument()
    fail = false
    await userEvent.click(within(chat()).getByRole('button', { name: 'Повторить' }))
    expect(await within(chat()).findByText(/Готово: заявка №ABS-000123/)).toBeInTheDocument()
    expect(ids).toEqual([ACTION_ID, ACTION_ID])
  })

  it('410 → form with previous dates; 404 → empty form (F-08)', async () => {
    server.use(http.post('/api/actions/:id/confirm', () => errorBody(410, 'ACTION_EXPIRED', 'Черновик истёк')))
    await toDraft()
    await userEvent.click(within(chat()).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(chat()).findByText('Черновик устарел — проверь даты ещё раз')).toBeInTheDocument()
    const form = within(chat()).getByRole('form', { name: 'Даты отгула' })
    expect(fromInput(form).value).toBe('2026-11-13')
  })

  it('404 on confirm → empty form', async () => {
    server.use(http.post('/api/actions/:id/confirm', () => errorBody(404, 'NOT_FOUND', 'Нет')))
    await toDraft()
    await userEvent.click(within(chat()).getByRole('button', { name: 'Подтвердить' }))
    await within(chat()).findByText('Черновик устарел — проверь даты ещё раз')
    expect(fromInput(within(chat()).getByRole('form', { name: 'Даты отгула' })).value).toBe('')
  })

  it('422 on confirm (overlap since the draft) → back to the form with the error', async () => {
    server.use(http.post('/api/actions/:id/confirm', () => errorBody(422, 'VALIDATION_FAILED', 'x', { errors: [{ field: 'range', code: 'OVERLAP', message: 'Пересекается с заявкой ABS-000555 на 13.11–13.11', params: { request_id: 'ABS-000555' } }] })))
    await toDraft()
    await userEvent.click(within(chat()).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(chat()).findByText(/ABS-000555/, { selector: 'p' })).toBeInTheDocument()
  })

  it('a new form makes the previous one superseded (F-16)', async () => {
    const { form } = await openForm()
    chatReplies(formReply())
    await userEvent.type(screen.getByLabelText('Сообщение Вите'), 'хочу отгул{Enter}')
    await waitFor(() => expect(within(chat()).getAllByRole('form', { name: 'Даты отгула' })).toHaveLength(2))
    expect(within(form).getByText('Открыта новая форма ниже')).toBeInTheDocument()
    expect(fromInput(form)).toBeDisabled()
  })

  it('form from history is inactive and offers «Оформить отгул»; a history draft reads its state (F-16, XF-09)', async () => {
    server.use(
      http.get('/api/chat/history', () => HttpResponse.json({ items: [
        { id: '55555555-5555-4555-8555-555555555555', ts: '2026-11-10T08:00:00Z', author: 'bot', reply: f.reply({ reply_id: '55555555-5555-4555-8555-555555555555', ...formReply() }) },
        { id: '66666666-6666-4666-8666-666666666666', ts: '2026-11-10T08:01:00Z', author: 'bot', reply: f.reply({ reply_id: '66666666-6666-4666-8666-666666666666', text: 'Черновик', pending_action: draft() }) },
      ] })),
      http.get(`/api/actions/${ACTION_ID}`, () => HttpResponse.json({ ...draft({ state: 'confirmed' }), result: done })),
    )
    renderApp('/')
    expect(await screen.findByText('Форма отгула из истории')).toBeInTheDocument()
    expect(await screen.findByText('Заявка №ABS-000123')).toBeInTheDocument()
    expect(within(chat()).queryByRole('button', { name: 'Подтвердить' })).toBeNull()
  })

  it('config failure shows retry', async () => {
    server.use(http.get('/api/time-off/config', () => errorBody(503, 'UNAVAILABLE', 'Конфиг недоступен')))
    chatReplies(formReply())
    renderApp('/')
    await userEvent.click((await within(await screen.findByRole('region', { name: 'Чат с Витей' })).findAllByRole('button', { name: 'Оформить отгул' }))[0])
    expect(await within(chat()).findByText(/Не получилось загрузить правила отгула/)).toBeInTheDocument()
  })
})
