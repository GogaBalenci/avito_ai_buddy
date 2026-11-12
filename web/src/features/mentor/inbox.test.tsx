import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import type { RequestView } from '@/shared/api/schemas'

const req: RequestView = { ...f.requestAcc, employee: { user_id: 'u-intern-be', display_name: 'Аня Смирнова' } }
const timerReq: RequestView = { ...f.requestAcc, request_id: 'ACC-000103', resource_title: 'Канал Tech-dep', policy: 'timer', auto_approve_at: '2026-11-12T09:02:00Z', employee: { user_id: 'u-intern-fe', display_name: 'Миша Котов' } }

function inbox(items: RequestView[]) {
  server.use(http.get('/api/mentor/access-requests', () => HttpResponse.json({ items, total: items.length })))
}

describe('mentor inbox (wave 6, XF-05…XF-07)', () => {
  beforeEach(() => server.use(http.get('/api/me', () => HttpResponse.json(f.meMentor))))

  it('lists requests with intern, resource, step, timer hint; badge in header', async () => {
    inbox([req, timerReq])
    renderApp('/mentor/inbox')
    const card = await screen.findByRole('listitem', { name: 'Заявка ACC-000101' })
    expect(within(card).getByText('Аня Смирнова')).toBeInTheDocument()
    expect(within(card).getByText('Пространство кластера · чтение')).toBeInTheDocument()
    expect(within(card).getByText(/Шаг «Запроси доступ в пространство своего кластера»/)).toBeInTheDocument()
    expect(within(screen.getByRole('listitem', { name: 'Заявка ACC-000103' })).getByText('Одобрится автоматически около 12:02')).toBeInTheDocument()
    expect(screen.getByLabelText('2 новых')).toBeInTheDocument()
  })

  it('empty state', async () => {
    inbox([])
    renderApp('/mentor/inbox')
    expect(await screen.findByText('Входящих заявок нет')).toBeInTheDocument()
  })

  it('approve removes the card optimistically and sends the decision', async () => {
    inbox([req])
    let body: unknown
    server.use(http.post('/api/mentor/access-requests/:id/decision', async ({ request }) => {
      body = await request.json()
      await delay(30)
      return HttpResponse.json({ ...req, status: 'approved', decided_by: 'mentor' })
    }))
    renderApp('/mentor/inbox')
    const card = await screen.findByRole('listitem', { name: 'Заявка ACC-000101' })
    await userEvent.click(within(card).getByRole('button', { name: 'Одобрить' }))
    expect(screen.queryByRole('listitem', { name: 'Заявка ACC-000101' })).toBeNull()
    expect(await screen.findByText('Одобрено: «Пространство кластера»')).toBeInTheDocument()
    expect(body).toMatchObject({ decision: 'approve' })
    expect(body).not.toHaveProperty('reject_reason')
  })

  it('error returns the card back', async () => {
    inbox([req])
    server.use(http.post('/api/mentor/access-requests/:id/decision', () => errorBody(503, 'UNAVAILABLE', 'Сервис доступов не отвечает')))
    renderApp('/mentor/inbox')
    await userEvent.click(within(await screen.findByRole('listitem', { name: 'Заявка ACC-000101' })).getByRole('button', { name: 'Одобрить' }))
    expect(await screen.findByText(/Сервис доступов не отвечает/)).toBeInTheDocument()
    expect(screen.getByRole('listitem', { name: 'Заявка ACC-000101' })).toBeInTheDocument()
  })

  it('reject needs a reason (XF-06)', async () => {
    inbox([req])
    let body: unknown
    server.use(http.post('/api/mentor/access-requests/:id/decision', async ({ request }) => {
      body = await request.json()
      return HttpResponse.json({ ...req, status: 'rejected' })
    }))
    renderApp('/mentor/inbox')
    const card = await screen.findByRole('listitem', { name: 'Заявка ACC-000101' })
    await userEvent.click(within(card).getByRole('button', { name: 'Отклонить' }))
    const submit = within(card).getByRole('button', { name: 'Отклонить заявку' })
    expect(submit).toBeDisabled()
    await userEvent.click(within(card).getByRole('radio', { name: 'Обсудим на 1:1' }))
    expect(submit).toBeEnabled()
    await userEvent.click(submit)
    await waitFor(() => expect(body).toMatchObject({ decision: 'reject', reject_reason: 'discuss_1on1' }))
    expect(await screen.findByText('Заявка отклонена')).toBeInTheDocument()
  })

  it('«Назад» leaves reject mode', async () => {
    inbox([req])
    renderApp('/mentor/inbox')
    const card = await screen.findByRole('listitem', { name: 'Заявка ACC-000101' })
    await userEvent.click(within(card).getByRole('button', { name: 'Отклонить' }))
    await userEvent.click(within(card).getByRole('button', { name: 'Назад' }))
    expect(within(card).getByRole('button', { name: 'Одобрить' })).toBeInTheDocument()
  })

  it('409 ALREADY_DECIDED → «Уже решено» and the list is refreshed (XF-07)', async () => {
    let calls = 0
    server.use(
      http.get('/api/mentor/access-requests', () => {
        calls++
        return HttpResponse.json(calls === 1 ? { items: [req], total: 1 } : { items: [], total: 0 })
      }),
      http.post('/api/mentor/access-requests/:id/decision', () => errorBody(409, 'ALREADY_DECIDED', 'Решение уже принято')),
    )
    renderApp('/mentor/inbox')
    await userEvent.click(within(await screen.findByRole('listitem', { name: 'Заявка ACC-000101' })).getByRole('button', { name: 'Одобрить' }))
    expect(await screen.findByText('Уже решено')).toBeInTheDocument()
    expect(await screen.findByText('Входящих заявок нет')).toBeInTheDocument()
  })

  it('error and stale states', async () => {
    server.use(http.get('/api/mentor/access-requests', () => errorBody(503, 'UNAVAILABLE', 'нет')))
    const a = renderApp('/mentor/inbox')
    expect(await screen.findByText(/Не получилось загрузить заявки/)).toBeInTheDocument()
    a.unmount()
    server.use(http.get('/api/mentor/access-requests', () => HttpResponse.json({ items: [req], total: 1, stale: true })))
    renderApp('/mentor/inbox')
    expect(await screen.findByText(/показаны сохранённые данные/)).toBeInTheDocument()
  })
})
