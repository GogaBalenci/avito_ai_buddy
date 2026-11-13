import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import type { AgentReply } from '@/shared/api/schemas'
import { useMascot } from '@/shared/mascot/mascotStore'
import { useChat } from './chatStore'
import { kbSlug, splitMarkers } from './ReplyText'
import { previousUserText, visibleNotices } from './replyLogic'

const chat = () => screen.getByRole('region', { name: 'Чат с Витей' })
const input = () => screen.getByLabelText('Сообщение Вите')

function replyWith(over: Partial<AgentReply>) {
  return (body: { client_message_id: string }) => HttpResponse.json(f.reply({ client_message_id: body.client_message_id, reply_id: crypto.randomUUID(), ...over }))
}

function onMessage(over: Partial<AgentReply>, seen?: unknown[]) {
  const make = replyWith(over)
  server.use(http.post('/api/chat/messages', async ({ request }) => {
    const b = (await request.json()) as { client_message_id: string }
    seen?.push(b)
    return make(b)
  }))
}

async function openChat() {
  const r = renderApp('/')
  await within(await screen.findByRole('region', { name: 'Чат с Витей' })).findByTestId('greeting')
  return r
}

async function say(text: string) {
  await userEvent.type(input(), text)
  await userEvent.click(within(chat()).getByRole('button', { name: 'Отправить' }))
}

const kbReply: Partial<AgentReply> = {
  intent: 'kb_question',
  text: 'Ревью делают не позже следующего дня [1]. Ещё [2] и [9].',
  sources: [
    { n: 1, slug: 'neighborhood-agreement', title: 'Соседское соглашение', url: '/kb/neighborhood-agreement', label: 'test_instruction', source_level: 'playbook', dated: 'по мотивам Playbook 2020–2021' },
    { n: 2, slug: 'term-stash', title: 'Stash', url: '/kb/term-stash', label: 'test_description', source_level: 'none' },
  ],
  actions: [
    { type: 'feedback', label: 'Помогло', value: 'up' },
    { type: 'feedback', label: 'Не то', value: 'down' },
  ],
}

describe('chat (wave 4)', () => {
  it('empty chat greets with starter buttons that send intent payloads', async () => {
    const seen: { payload?: string }[] = []
    onMessage({ intent: 'my_path', text: 'Ты на этапе «Неделя 1»' }, seen)
    server.use(http.get('/api/path', () => HttpResponse.json(f.path({ state: 'finished', next_step: null }))))
    await openChat()
    await within(chat()).findByText(/Все шаги пройдены/)
    await userEvent.click(within(chat()).getByRole('button', { name: 'Что мне делать дальше?' }))
    expect(await within(chat()).findByText('Ты на этапе «Неделя 1»')).toBeInTheDocument()
    expect(seen[0]).toMatchObject({ payload: 'intent:my_path' })
    // в истории — подпись кнопки, а не payload
    expect(within(chat()).getByText('Что мне делать дальше?', { selector: '.bubble--user' })).toBeInTheDocument()
  })

  it('renders text, [n] markers as links to sources, plaques by label and dated (KF-01, KF-02)', async () => {
    onMessage(kbReply)
    await openChat()
    await say('как делать ревью')
    const text = (await within(chat()).findByText(/Ревью делают/)).closest('p')!
    const markers = within(text).getAllByRole('link')
    expect(markers.map((m) => m.textContent)).toEqual(['[1]', '[2]'])
    expect(markers[0]).toHaveAttribute('href', '/kb/neighborhood-agreement')
    expect(text).toHaveTextContent('[9]')
    const sources = within(chat()).getByRole('list', { name: 'Источники' })
    expect(within(sources).getByText('Тестовая инструкция')).toBeInTheDocument()
    expect(within(sources).getByText('Тестовое описание — уточни у наставника')).toBeInTheDocument()
    expect(within(sources).getByText('по мотивам Playbook 2020–2021')).toBeInTheDocument()
  })

  it('reply text with HTML is shown as text (AG-26, §14.1-18, KF-06)', async () => {
    onMessage({ text: '<img src=x onerror=alert(1)><script>alert(1)</script> [1]', sources: [{ n: 1, slug: 'x', title: 't', url: 'javascript:alert(1)', label: 'test_instruction', source_level: 'team' }] })
    await openChat()
    await say('xss')
    const t = (await within(chat()).findByText(/<script>alert\(1\)<\/script>/)).closest('p')!
    expect(t.querySelector('script')).toBeNull()
    expect(t.querySelector('img')).toBeNull()
    expect(within(chat()).queryByRole('link', { name: '[1]' })).toBeNull()
    expect(within(chat()).queryByRole('link', { name: 't' })).toBeNull()
  })

  it('one request in flight: send disabled, input and panel stay usable; Vitya thinking (AG-18)', async () => {
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string }
      await delay(80)
      return replyWith({ text: 'Ответ' })(b)
    }))
    await openChat()
    await say('привет')
    expect(within(chat()).getByLabelText('Витя печатает ответ')).toBeInTheDocument()
    expect(within(chat()).getByRole('img', { name: 'Витя думает' })).toBeInTheDocument()
    await userEvent.type(input(), 'ещё')
    expect(within(chat()).getByRole('button', { name: 'Отправить' })).toBeDisabled()
    expect(input()).toBeEnabled()
    expect(await useChat.getState().send({ text: 'параллельно' })).toBeNull()
    expect(await within(chat()).findByText('Ответ')).toBeInTheDocument()
    expect(within(chat()).getByRole('button', { name: 'Отправить' })).toBeEnabled()
  })

  it('Enter sends, Shift+Enter does not; empty text is ignored', async () => {
    const seen: { text?: string }[] = []
    onMessage({ text: 'ок' }, seen)
    await openChat()
    await userEvent.type(input(), '   ')
    await userEvent.keyboard('{Enter}')
    expect(seen).toHaveLength(0)
    await userEvent.clear(input())
    await userEvent.type(input(), 'строка{Shift>}{Enter}{/Shift}вторая{Enter}')
    await waitFor(() => expect(seen).toHaveLength(1))
    expect(seen[0].text).toBe('строка\nвторая')
  })

  it('network failure → «Повторить» resends the same client_message_id (09 §2.1, F-15)', async () => {
    const ids: string[] = []
    let fail = true
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string }
      ids.push(b.client_message_id)
      if (fail) return HttpResponse.error()
      return replyWith({ text: 'Дошло' })(b)
    }))
    await openChat()
    await say('хочу отгул')
    expect(await within(chat()).findByRole('alert')).toHaveTextContent(/Нет связи/)
    expect(useMascot.getState().client).toBe('oops')
    fail = false
    await userEvent.click(within(chat()).getByRole('button', { name: 'Повторить' }))
    expect(await within(chat()).findByText('Дошло')).toBeInTheDocument()
    expect(ids).toHaveLength(2)
    expect(ids[0]).toBe(ids[1])
    expect(within(chat()).queryByRole('alert')).toBeNull()
  })

  it('after 5 s shows «дольше обычного» and «Отменить» aborts; retry keeps the id (F-06)', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const ids: string[] = []
      server.use(http.post('/api/chat/messages', async ({ request }) => {
        const b = (await request.json()) as { client_message_id: string }
        ids.push(b.client_message_id)
        if (ids.length === 1) await delay('infinite')
        return replyWith({ text: 'Готов ответ' })(b)
      }))
      await openChat()
      await say('долгий вопрос')
      await act(async () => {
        vi.advanceTimersByTime(5100)
      })
      expect(within(chat()).getByText('Думаю чуть дольше обычного…')).toBeInTheDocument()
      await userEvent.click(within(chat()).getByRole('button', { name: 'Отменить' }))
      expect(await within(chat()).findByText('Отменено')).toBeInTheDocument()
      await userEvent.click(within(chat()).getByRole('button', { name: 'Повторить' }))
      expect(await within(chat()).findByText('Готов ответ')).toBeInTheDocument()
      expect(ids[0]).toBe(ids[1])
    } finally {
      vi.useRealTimers()
    }
  })

  it('error with a reply body renders the reply; 400 on a button is not retryable', async () => {
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string; payload?: string }
      if (b.payload) return errorBody(400, 'BAD_REQUEST', 'payload')
      return errorBody(503, 'UNAVAILABLE', 'нет', { reply: f.reply({ client_message_id: b.client_message_id, mascot_state: 'oops', text: 'Ой, сервис не отвечает', actions: [{ type: 'retry', label: 'Повторить', payload: 'intent:my_path' }] }) })
    }))
    await openChat()
    await say('что дальше')
    expect(await within(chat()).findByText('Ой, сервис не отвечает')).toBeInTheDocument()
    expect(useMascot.getState().server).toBe('oops')
    await userEvent.click(within(chat()).getByRole('button', { name: 'Повторить' }))
    expect(await within(chat()).findByText('Не получилось обработать кнопку — обнови страницу')).toBeInTheDocument()
    expect(within(chat()).queryAllByRole('button', { name: 'Повторить' })).toHaveLength(1)
  })

  it('history: shows saved messages with button labels, then appends new ones', async () => {
    server.use(http.get('/api/chat/history', () => HttpResponse.json({
      items: [
        { id: '11111111-1111-4111-8111-111111111111', ts: '2026-11-12T08:00:00Z', author: 'user', text: null, button_label: 'Разобраться: Изучи «Авито в цифрах»', reply: null },
        { id: '22222222-2222-4222-8222-222222222222', ts: '2026-11-12T08:00:01Z', author: 'bot', reply: f.reply({ reply_id: '22222222-2222-4222-8222-222222222222', text: 'Сохранённый ответ', mascot_state: 'done' }) },
      ],
    })))
    renderApp('/')
    expect(await screen.findByText('Сохранённый ответ')).toBeInTheDocument()
    expect(screen.getByText('Разобраться: Изучи «Авито в цифрах»')).toBeInTheDocument()
    expect(screen.queryByTestId('greeting')).toBeNull()
    expect(useMascot.getState().server).toBe('done')
  })

  it('history failure is shown with «Повторить»', async () => {
    server.use(http.get('/api/chat/history', () => errorBody(503, 'UNAVAILABLE', 'История недоступна')))
    renderApp('/')
    expect(await screen.findByText(/История не загрузилась/)).toBeInTheDocument()
    server.use(http.get('/api/chat/history', () => HttpResponse.json({ items: [] })))
    await userEvent.click(within(chat()).getByRole('button', { name: 'Повторить' }))
    expect(await screen.findByTestId('greeting')).toBeInTheDocument()
  })

  it('notices: llm plaques once per session, others every time', async () => {
    onMessage({ text: 'раз', notices: ['simplified_mode', 'pii_masked'] })
    await openChat()
    await say('один')
    await within(chat()).findByText('раз')
    onMessage({ text: 'ответ два', notices: ['simplified_mode', 'pii_masked'] })
    await say('два')
    await within(chat()).findByText('ответ два')
    expect(within(chat()).getAllByText(/упрощённом режиме/)).toHaveLength(1)
    expect(within(chat()).getAllByText(/Скрыл личные данные/)).toHaveLength(2)
  })

  it('escalation draft is editable; «Скопировать» copies the edited text and reports the event (KF-04)', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const events: unknown[] = []
    server.use(http.post('/api/client-events', async ({ request }) => {
      events.push(await request.json())
      return new HttpResponse(null, { status: 204 })
    }))
    const replyId = '33333333-3333-4333-8333-333333333333'
    onMessage({ reply_id: replyId, intent: 'ask_mentor', mascot_state: 'ask_mentor', text: 'Давай спросим наставника — вот черновик.', escalation: { reason: 'no_answer', draft: 'Привет! Вопрос: где логи?' }, actions: [{ type: 'copy', label: 'Скопировать', text: 'x', copy_event: 'escalation_copied' }] })
    await openChat()
    await say('где логи')
    const box = await within(chat()).findByLabelText('Черновик сообщения наставнику')
    await userEvent.type(box, ' Спасибо!')
    await userEvent.click(within(chat()).getByRole('button', { name: 'Скопировать' }))
    expect(writeText).toHaveBeenCalledWith('Привет! Вопрос: где логи? Спасибо!')
    await waitFor(() => expect(events).toEqual([{ name: 'escalation_copied', reply_id: replyId }]))
    expect(await screen.findByText('Скопировано')).toBeInTheDocument()
    expect(useMascot.getState().server).toBe('ask_mentor')
  })

  it('copy without clipboard → selected text and manual hint (KF-07, F-10)', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    onMessage({ text: 'Готово', mascot_state: 'done', actions: [{ type: 'copy', label: 'Скопировать сообщение наставнику', text: 'Привет! Оформил(а) отгул', copy_event: undefined }] })
    await openChat()
    await say('ок')
    await userEvent.click(await within(chat()).findByRole('button', { name: 'Скопировать сообщение наставнику' }))
    expect(await within(chat()).findByLabelText('Текст для копирования')).toHaveValue('Привет! Оформил(а) отгул')
    expect(within(chat()).getByText('Скопируй вручную: Ctrl+C')).toBeInTheDocument()
  })

  it('feedback: optimistic, no second request for the same value, escalation header → ask_mentor (KF-05)', async () => {
    const votes: unknown[] = []
    const sent: { payload?: string }[] = []
    server.use(http.post('/api/feedback', async ({ request }) => {
      const b = (await request.json()) as { value: string }
      votes.push(b)
      return new HttpResponse(null, { status: 204, headers: b.value === 'down' ? { 'X-Buddy-Escalation': 'negative_feedback' } : {} })
    }))
    onMessage(kbReply, sent)
    await openChat()
    await say('вопрос')
    const up = await within(chat()).findByRole('button', { name: /Помогло/ })
    await userEvent.click(up)
    expect(up).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(up)
    await waitFor(() => expect(votes).toHaveLength(1))
    await userEvent.click(within(chat()).getByRole('button', { name: /Не то/ }))
    await waitFor(() => expect(sent.some((s) => s.payload === 'intent:ask_mentor')).toBe(true))
  })

  it('feedback failure rolls back', async () => {
    server.use(http.post('/api/feedback', () => errorBody(404, 'NOT_FOUND', 'Ответ не найден')))
    onMessage(kbReply)
    await openChat()
    await say('вопрос')
    const up = await within(chat()).findByRole('button', { name: /Помогло/ })
    await userEvent.click(up)
    expect(await screen.findByText(/Ответ не найден/)).toBeInTheDocument()
    expect(up).toHaveAttribute('aria-pressed', 'false')
  })

  it('quick replies, link, retry without payload and mark_done actions', async () => {
    const seen: { text?: string; payload?: string }[] = []
    const statusCalls: unknown[] = []
    server.use(http.post('/api/path/steps/:id/status', async ({ request }) => {
      statusCalls.push(await request.json())
      return HttpResponse.json({ step: { ...f.path().steps[4], status: 'done' }, changed: true, path_summary: { state: 'ready', next_step: { step_id: 'cm-1.7', reason: 'current' }, waiting: [], progress: { period_done: 3, period_total: 6, overall_done: 3, overall_total: 7 } } })
    }))
    onMessage({ intent: 'step_explain', text: 'Объяснение шага', actions: [
      { type: 'quick_reply', label: 'Оформить отгул', payload: 'intent:time_off' },
      { type: 'link', label: 'Мои заявки', url: '/requests' },
      { type: 'retry', label: 'Повторить' },
      { type: 'mark_done', label: 'Отметить пройденным', step_id: 'be-1.11' },
      { type: 'link', label: 'Внешняя', url: 'https://evil.example' },
    ] }, seen)
    await openChat()
    await within(screen.getByRole('complementary', { name: 'Мой путь' })).findByText('Что дальше')
    await say('объясни шаг')
    await within(chat()).findByText('Объяснение шага')
    expect(within(chat()).getByRole('link', { name: 'Мои заявки' })).toHaveAttribute('href', '/requests')
    expect(within(chat()).queryByRole('link', { name: 'Внешняя' })).toBeNull()
    await userEvent.click(within(chat()).getAllByRole('button', { name: 'Отметить пройденным' })[0])
    await waitFor(() => expect(statusCalls).toEqual([expect.objectContaining({ status: 'done', expected_status: 'in_progress' })]))
    await userEvent.click(within(chat()).getAllByRole('button', { name: 'Повторить' })[0])
    await waitFor(() => expect(seen.filter((s) => s.text === 'объясни шаг')).toHaveLength(2))
    await userEvent.click(within(chat()).getAllByRole('button', { name: 'Оформить отгул' })[0])
    await waitFor(() => expect(seen.some((s) => s.payload === 'intent:time_off')).toBe(true))
  })

  it('mark_done on an already done step only says so', async () => {
    onMessage({ text: 'Шаг', actions: [{ type: 'mark_done', label: 'Отметить пройденным', step_id: 'cm-1.6' }] })
    await openChat()
    await within(screen.getByRole('complementary', { name: 'Мой путь' })).findByText('Что дальше')
    await say('x')
    await userEvent.click(await within(chat()).findByRole('button', { name: 'Отметить пройденным' }))
    expect(await screen.findByText('Шаг «Познакомься с командой» уже отмечен')).toBeInTheDocument()
  })
})

describe('chat helpers', () => {
  it('splitMarkers ignores unknown numbers; kbSlug accepts only /kb/<slug>', () => {
    const src = [{ n: 1, slug: 'a', title: 'A', url: '/kb/a', label: 'test_instruction' as const, source_level: 'team' as const }]
    expect(splitMarkers('x [1] y [2]', src).map((p) => p.kind)).toEqual(['text', 'ref', 'text'])
    expect(splitMarkers('[1]', src)).toHaveLength(1)
    expect(kbSlug('/kb/a-b')).toBe('a-b')
    expect(kbSlug('javascript:alert(1)')).toBeNull()
    expect(kbSlug('/kb/../x')).toBeNull()
  })

  it('visibleNotices and previousUserText', () => {
    const r = (id: string, notices: AgentReply['notices']) => ({ id, author: 'bot' as const, ts: '', reply: f.reply({ reply_id: id, notices }) })
    const entries = [{ id: 'u', author: 'user' as const, ts: '', text: 'вопрос' }, r('a', ['llm_auth_error']), r('b', ['llm_auth_error', 'doc_conflict'])]
    expect(visibleNotices(entries, 1)).toEqual(['llm_auth_error'])
    expect(visibleNotices(entries, 2)).toEqual(['doc_conflict'])
    expect(visibleNotices(entries, 5)).toEqual([])
    expect(previousUserText(entries, 'b')).toBe('вопрос')
    expect(previousUserText(entries.slice(1), 'a')).toBeUndefined()
  })

  it('store ignores retry for unknown entries and cancel without request', async () => {
    useChat.getState().reset()
    expect(await useChat.getState().retry('nope')).toBeNull()
    useChat.getState().cancel()
    expect(await useChat.getState().send({ text: '   ' })).toBeNull()
  })
})
