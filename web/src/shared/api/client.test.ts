import { http, HttpResponse } from 'msw'
import { z } from 'zod'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import { api, apiRequest, ApiError, errorText, isApiError, setUnauthorizedHandler } from './client'
import { MeSchema } from './schemas'

describe('api client', () => {
  afterEach(() => setUnauthorizedHandler(null))

  it('parses a response with the Zod schema', async () => {
    const me = await api('/api/me', { schema: MeSchema })
    expect(me.role).toBe('intern')
  })

  it('returns undefined for 204', async () => {
    await expect(api('/api/demo/reset', { method: 'POST' })).resolves.toBeUndefined()
  })

  it('sends JSON body with POST and same-origin credentials', async () => {
    let seen: { method: string; body: unknown; ct: string | null } | null = null
    server.use(
      http.post('/api/feedback', async ({ request }) => {
        seen = { method: request.method, body: await request.json(), ct: request.headers.get('content-type') }
        return new HttpResponse(null, { status: 204 })
      }),
    )
    await api('/api/feedback', { body: { reply_id: 'x', value: 'up' } })
    expect(seen).toEqual({ method: 'POST', body: { reply_id: 'x', value: 'up' }, ct: 'application/json' })
  })

  it('maps an Error body with request id into ApiError (F-13)', async () => {
    server.use(http.post('/api/actions', () => errorBody(422, 'VALIDATION_FAILED', 'Дата уже прошла', { errors: [{ field: 'date_from', code: 'DATE_IN_PAST', message: 'Дата уже прошла' }] })))
    const err = await api('/api/actions', { body: {} }).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    const e = err as ApiError
    expect(e.status).toBe(422)
    expect(e.code).toBe('VALIDATION_FAILED')
    expect(e.body?.errors?.[0].field).toBe('date_from')
    expect(e.retryable).toBe(false)
    expect(errorText(e)).toBe('Дата уже прошла (код для поддержки: req-test-1)')
    expect(isApiError(e, 422)).toBe(true)
    expect(isApiError(e, 400)).toBe(false)
  })

  it('unexpected response format becomes a readable error, not a crash (PF-01, F-13)', async () => {
    server.use(http.get('/api/me', () => HttpResponse.json({ nope: true })))
    const e = (await api('/api/me', { schema: MeSchema }).catch((x: unknown) => x)) as ApiError
    expect(e.kind).toBe('bad_response')
    expect(e.message).toMatch(/неожиданном формате/)
  })

  it('non-JSON body with schema is a bad_response', async () => {
    server.use(http.get('/api/me', () => new HttpResponse('oops', { status: 200 })))
    const e = (await api('/api/me', { schema: z.object({ a: z.string() }) }).catch((x: unknown) => x)) as ApiError
    expect(e.code).toBe('BAD_RESPONSE')
  })

  it('error without JSON body falls back to a generic text', async () => {
    server.use(http.get('/api/path', () => new HttpResponse('down', { status: 503 })))
    const e = (await api('/api/path').catch((x: unknown) => x)) as ApiError
    expect(e.message).toMatch(/временно недоступен/)
    expect(e.retryable).toBe(true)
    server.use(http.get('/api/path', () => new HttpResponse('x', { status: 418 })))
    const e2 = (await api('/api/path').catch((x: unknown) => x)) as ApiError
    expect(e2.code).toBe('HTTP_418')
    expect(e2.message).toMatch(/Что-то пошло не так/)
  })

  it('body.retryable wins over status', async () => {
    server.use(http.get('/api/path', () => errorBody(503, 'UNAVAILABLE', 'Нет', { retryable: false })))
    const e = (await api('/api/path').catch((x: unknown) => x)) as ApiError
    expect(e.retryable).toBe(false)
  })

  it('network failure is retryable and has no support code', async () => {
    server.use(http.get('/api/path', () => HttpResponse.error()))
    const e = (await api('/api/path').catch((x: unknown) => x)) as ApiError
    expect(e.kind).toBe('network')
    expect(e.retryable).toBe(true)
    expect(errorText(e)).toMatch(/Нет связи/)
    expect(errorText(new Error('x'))).toMatch(/Что-то пошло не так/)
  })

  it('abort gives an aborted error', async () => {
    const ctrl = new AbortController()
    ctrl.abort()
    const e = (await api('/api/path', { signal: ctrl.signal }).catch((x: unknown) => x)) as ApiError
    expect(e.kind).toBe('aborted')
    expect(e.retryable).toBe(true)
  })

  it('401 calls the unauthorized handler unless silent', async () => {
    const h = vi.fn()
    setUnauthorizedHandler(h)
    server.use(http.get('/api/me', () => errorBody(401, 'UNAUTHORIZED', 'Нет сессии')))
    await api('/api/me').catch(() => undefined)
    expect(h).toHaveBeenCalledTimes(1)
    await api('/api/me', { silent401: true }).catch(() => undefined)
    expect(h).toHaveBeenCalledTimes(1)
  })

  it('apiRequest exposes headers', async () => {
    server.use(http.post('/api/feedback', () => new HttpResponse(null, { status: 204, headers: { 'X-Buddy-Escalation': 'negative_feedback' } })))
    const r = await apiRequest('/api/feedback', { body: {} })
    expect(r.headers.get('X-Buddy-Escalation')).toBe('negative_feedback')
  })
})
