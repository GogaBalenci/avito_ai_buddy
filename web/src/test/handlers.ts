import { http, HttpResponse } from 'msw'
import * as f from './fixtures'

/** Счастливый путь; тесты переопределяют нужное через server.use(...). */
export const handlers = [
  http.get('/api/config', () => HttpResponse.json(f.config)),
  http.get('/api/llm/health', () => HttpResponse.json(f.llmHealth)),
  http.get('/api/auth/accounts', () => HttpResponse.json({ items: f.accounts })),
  http.post('/api/auth/login', () => HttpResponse.json(f.meIntern)),
  http.post('/api/auth/logout', () => new HttpResponse(null, { status: 204 })),
  http.get('/api/me', () => HttpResponse.json(f.meIntern)),
  http.get('/api/path', () => HttpResponse.json(f.path())),
  http.get('/api/chat/history', () => HttpResponse.json({ items: [] })),
  http.post('/api/chat/messages', async ({ request }) => {
    const body = (await request.json()) as { client_message_id: string }
    return HttpResponse.json(f.reply({ client_message_id: body.client_message_id, reply_id: crypto.randomUUID() }))
  }),
  http.get('/api/time-off/config', () => HttpResponse.json(f.timeOffConfig)),
  http.get('/api/requests', () => HttpResponse.json({ items: [f.requestAbs, f.requestAcc], total: 2 })),
  http.get('/api/mentor/interns', () => HttpResponse.json({ items: f.interns })),
  http.get('/api/mentor/interns/:id/path', () => HttpResponse.json(f.path({ read_only: true }))),
  http.get('/api/mentor/access-requests', () => HttpResponse.json({ items: [], total: 0 })),
  http.get('/api/demo/flags', () => HttpResponse.json({ items: f.flags })),
  http.post('/api/demo/reset', () => new HttpResponse(null, { status: 204 })),
  http.post('/api/client-events', () => new HttpResponse(null, { status: 204 })),
  http.post('/api/feedback', () => new HttpResponse(null, { status: 204 })),
  http.get('/api/access/resources', () => HttpResponse.json({ items: f.resources })),
  http.get('/api/kb/:slug', () => HttpResponse.json(f.kbDoc)),
]

export const errorBody = (status: number, code: string, message: string, extra: Record<string, unknown> = {}) =>
  HttpResponse.json({ code, message, ...extra }, { status, headers: { 'X-Request-Id': 'req-test-1' } })
