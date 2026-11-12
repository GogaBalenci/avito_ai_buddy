import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import { safeUrl } from './KbPage'

describe('/kb/$slug (wave 7, KF-03, KF-06)', () => {
  it('renders the doc with label plaque, dated and sanitized markdown', async () => {
    const { container } = renderApp('/kb/dev-principles')
    expect(await screen.findByRole('heading', { level: 1, name: 'Принципы разработки' })).toBeInTheDocument()
    expect(screen.getAllByText('Тестовая инструкция').length).toBeGreaterThan(0)
    expect(screen.getByText('по мотивам Playbook 2020–2021')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: 'Принципы' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Ссылка' })).toHaveAttribute('href', 'https://example.com')
    expect(screen.getByRole('link', { name: 'Ссылка' })).toHaveAttribute('rel', 'noopener noreferrer')
    // javascript: ссылка не активна, сырой HTML не отрисован
    expect(screen.queryByRole('link', { name: 'плохая' })).toBeNull()
    expect(screen.getByText('плохая')).toBeInTheDocument()
    expect(container.querySelector('script')).toBeNull()
  })

  it('test description, internal links, images and conflicts', async () => {
    server.use(http.get('/api/kb/:slug', () => HttpResponse.json({ ...f.kbDoc, slug: 'term-stash', title: 'Stash', label: 'test_description', source_level: 'none', dated: null, kind: 'term', body_md: 'См. [сабмодули](/kb/term-avito-cli) и ![схема](https://x/y.png)', conflicts_with: ['code-review-team'] })))
    renderApp('/kb/term-stash')
    expect(await screen.findByRole('heading', { level: 1, name: 'Stash' })).toBeInTheDocument()
    expect(screen.getByText(/Тестовое описание для прототипа/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'сабмодули' })).toHaveAttribute('href', '/kb/term-avito-cli')
    expect(screen.getByText('[изображение: схема]')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'code-review-team' })).toHaveAttribute('href', '/kb/code-review-team')
  })

  it('403 shows the disclosed title without body', async () => {
    server.use(http.get('/api/kb/:slug', () => errorBody(403, 'PERMISSION_DENIED', 'Нет доступа', { title: 'Методичка наставника' })))
    renderApp('/kb/mentor-guide')
    expect(await screen.findByRole('heading', { name: 'Методичка наставника' })).toBeInTheDocument()
    expect(screen.getByText(/спроси наставника/)).toBeInTheDocument()
  })

  it('403 without title and 404', async () => {
    server.use(http.get('/api/kb/:slug', () => errorBody(403, 'PERMISSION_DENIED', 'Нет доступа')))
    const a = renderApp('/kb/secret')
    expect(await screen.findByRole('heading', { name: 'Документ с ограниченным доступом' })).toBeInTheDocument()
    a.unmount()
    server.use(http.get('/api/kb/:slug', () => errorBody(404, 'NOT_FOUND', 'Нет')))
    renderApp('/kb/nope')
    expect(await screen.findByRole('heading', { name: 'Документа нет' })).toBeInTheDocument()
  })

  it('503 → retry; back button', async () => {
    let fail = true
    server.use(http.get('/api/kb/:slug', () => (fail ? errorBody(503, 'UNAVAILABLE', 'Сервис знаний не отвечает') : HttpResponse.json(f.kbDoc))))
    renderApp('/kb/dev-principles')
    expect(await screen.findByText(/Не получилось загрузить документ/)).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Принципы разработки' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '← Назад' }))
  })

  it('source link in chat opens the doc page (KF-02)', async () => {
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string }
      return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, reply_id: crypto.randomUUID(), text: 'Пиши тесты [1].', sources: [{ n: 1, slug: 'dev-principles', title: 'Принципы разработки', url: '/kb/dev-principles', label: 'test_instruction', source_level: 'playbook' }] }))
    }))
    const { history } = renderApp('/')
    const input = await screen.findByLabelText('Сообщение Вите')
    await userEvent.type(input, 'принципы{Enter}')
    await userEvent.click(await screen.findByRole('link', { name: '[1]' }))
    await waitFor(() => expect(history.location.pathname).toBe('/kb/dev-principles'))
    expect(await screen.findByRole('heading', { level: 1, name: 'Принципы разработки' })).toBeInTheDocument()
  })

  it('doc conflict reply: two sources and «Сообщить о расхождении» reports the event (KF-08)', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
    const events: unknown[] = []
    const replyId = '99999999-9999-4999-8999-999999999999'
    server.use(
      http.post('/api/client-events', async ({ request }) => {
        events.push(await request.json())
        return new HttpResponse(null, { status: 204 })
      }),
      http.post('/api/chat/messages', async ({ request }) => {
        const b = (await request.json()) as { client_message_id: string }
        return HttpResponse.json(f.reply({
          client_message_id: b.client_message_id, reply_id: replyId, intent: 'kb_question', notices: ['doc_conflict'],
          text: 'Инструкции расходятся: в одной ревью за день [1], в другой — за два [2].',
          sources: [
            { n: 1, slug: 'code-review-general', title: 'Ревью: общее', url: '/kb/code-review-general', label: 'test_instruction', source_level: 'playbook' },
            { n: 2, slug: 'code-review-team', title: 'Ревью: команда', url: '/kb/code-review-team', label: 'test_instruction', source_level: 'team' },
          ],
          actions: [{ type: 'copy', label: 'Сообщить о расхождении', text: 'Привет! Нашёл расхождение', copy_event: 'doc_conflict_reported' }],
        }))
      }),
    )
    renderApp('/')
    await userEvent.type(await screen.findByLabelText('Сообщение Вите'), 'сроки ревью{Enter}')
    const chat = screen.getByRole('region', { name: 'Чат с Витей' })
    expect(await within(chat).findByText(/Инструкции расходятся — показываю обе/)).toBeInTheDocument()
    expect(within(within(chat).getByRole('list', { name: 'Источники' })).getAllByRole('listitem')).toHaveLength(2)
    await userEvent.click(within(chat).getByRole('button', { name: 'Сообщить о расхождении' }))
    await waitFor(() => expect(events).toEqual([{ name: 'doc_conflict_reported', reply_id: replyId }]))
  })
})

describe('safeUrl', () => {
  it.each([
    ['https://a.b/c', 'https://a.b/c'],
    ['http://a.b', 'http://a.b'],
    ['/kb/x', '/kb/x'],
    ['#part', '#part'],
    ['docs/x', 'docs/x'],
    ['javascript:alert(1)', ''],
    ['JAVASCRIPT:alert(1)', ''],
    ['data:text/html,x', ''],
    ['vbscript:x', ''],
    ['//evil.example', ''],
  ])('%s → %s', (input, out) => expect(safeUrl(input)).toBe(out))
})
