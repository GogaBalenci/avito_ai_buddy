// API-клиент (12 §10.1): fetch с same-origin cookie, единый разбор Error,
// X-Request-Id — «код для поддержки», AbortController для «Отменить».
import type { z } from 'zod'
import { ErrorBodySchema, type ErrorBody } from './schemas'

export type ApiErrorKind = 'http' | 'network' | 'aborted' | 'bad_response'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly kind: ApiErrorKind
  readonly body: ErrorBody | null
  readonly requestId: string | null
  readonly headers: Headers | null

  constructor(opts: { status: number; code: string; kind: ApiErrorKind; message: string; body?: ErrorBody | null; requestId?: string | null; headers?: Headers | null }) {
    super(opts.message)
    this.name = 'ApiError'
    this.status = opts.status
    this.code = opts.code
    this.kind = opts.kind
    this.body = opts.body ?? null
    this.requestId = opts.requestId ?? null
    this.headers = opts.headers ?? null
  }

  /** Сеть, 429, 503 и таймаут можно повторить. */
  get retryable(): boolean {
    if (this.kind === 'network' || this.kind === 'aborted') return true
    if (this.body?.retryable !== undefined) return this.body.retryable
    return this.status === 429 || this.status === 503 || this.status === 504
  }
}

type UnauthorizedHandler = () => void
let onUnauthorized: UnauthorizedHandler | null = null

/** Роутер подписывается: 401 → экран входа с тостом (09 §2.1). */
export function setUnauthorizedHandler(h: UnauthorizedHandler | null) {
  onUnauthorized = h
}

export type RequestOptions<T> = {
  method?: 'GET' | 'POST'
  body?: unknown
  schema?: z.ZodType<T>
  signal?: AbortSignal
  /** Не вызывать глобальный обработчик 401 (проверка сессии на экране входа). */
  silent401?: boolean
}

export type ApiResult<T> = { data: T; headers: Headers }

/** Относительный путь → абсолютный от текущего origin (одинаково в браузере и в jsdom). */
function absolute(path: string): string {
  return typeof window !== 'undefined' ? new URL(path, window.location.href).toString() : path
}

const GENERIC: Record<number, string> = {
  401: 'Сессия истекла — войди снова',
  403: 'Недостаточно прав',
  404: 'Не найдено',
  429: 'Слишком быстро, подожди немного',
  503: 'Сервис временно недоступен, попробуй ещё раз',
}

export async function apiRequest<T = void>(path: string, opts: RequestOptions<T> = {}): Promise<ApiResult<T>> {
  const method = opts.method ?? (opts.body === undefined ? 'GET' : 'POST')
  const init: RequestInit = {
    method,
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(opts.body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    signal: opts.signal,
  }
  if (opts.body !== undefined) init.body = JSON.stringify(opts.body)

  let res: Response
  try {
    res = await fetch(absolute(path), init)
  } catch {
    if (opts.signal?.aborted) {
      throw new ApiError({ status: 0, code: 'ABORTED', kind: 'aborted', message: 'Запрос отменён' })
    }
    throw new ApiError({ status: 0, code: 'NETWORK', kind: 'network', message: 'Нет связи с сервером — проверь сеть и попробуй ещё раз' })
  }

  const requestId = res.headers.get('X-Request-Id')
  if (!res.ok) {
    let body: ErrorBody | null = null
    try {
      const parsed = ErrorBodySchema.safeParse(await res.json())
      body = parsed.success ? parsed.data : null
    } catch {
      body = null
    }
    if (res.status === 401 && !opts.silent401) onUnauthorized?.()
    throw new ApiError({
      status: res.status,
      code: body?.code ?? `HTTP_${res.status}`,
      kind: 'http',
      message: body?.message ?? GENERIC[res.status] ?? 'Что-то пошло не так, попробуй ещё раз',
      body,
      requestId,
      headers: res.headers,
    })
  }

  if (res.status === 204 || !opts.schema) {
    return { data: undefined as T, headers: res.headers }
  }
  let json: unknown
  try {
    json = await res.json()
  } catch {
    json = undefined
  }
  const parsed = opts.schema.safeParse(json)
  if (!parsed.success) {
    throw new ApiError({
      status: res.status,
      code: 'BAD_RESPONSE',
      kind: 'bad_response',
      message: 'Сервер ответил в неожиданном формате — обнови страницу или попробуй позже',
      requestId,
    })
  }
  return { data: parsed.data, headers: res.headers }
}

export async function api<T = void>(path: string, opts: RequestOptions<T> = {}): Promise<T> {
  return (await apiRequest(path, opts)).data
}

/** Текст ошибки для пользователя: сообщение + код для поддержки, если есть. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    return e.requestId && e.kind !== 'network' ? `${e.message} (код для поддержки: ${e.requestId})` : e.message
  }
  return 'Что-то пошло не так, попробуй ещё раз'
}

export function isApiError(e: unknown, status?: number): e is ApiError {
  return e instanceof ApiError && (status === undefined || e.status === status)
}
