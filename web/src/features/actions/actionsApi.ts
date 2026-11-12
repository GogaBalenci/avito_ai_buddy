import { api } from '@/shared/api/client'
import { ActionCreatedSchema, ActionViewSchema, ConfirmResponseSchema } from '@/shared/api/schemas'
import { getQueryClient } from '@/app/queryClientRef'

/** Черновики и подтверждение (10 §8.1). confirm — единственный путь записи во внешний сервис (AG-05). */
export const createTimeOffAction = (date_from: string, date_to: string) =>
  api('/api/actions', { body: { kind: 'time_off', params: { date_from, date_to } }, schema: ActionCreatedSchema })

export const confirmAction = (id: string, signal?: AbortSignal) =>
  api(`/api/actions/${encodeURIComponent(id)}/confirm`, { method: 'POST', schema: ConfirmResponseSchema, signal })

export const cancelAction = (id: string) => api(`/api/actions/${encodeURIComponent(id)}/cancel`, { method: 'POST', schema: ActionViewSchema })

export const getAction = (id: string, signal?: AbortSignal) => api(`/api/actions/${encodeURIComponent(id)}`, { schema: ActionViewSchema, signal })

/** После созданной заявки обновляем «Мои заявки», путь и экраны наставника (F-09). */
export function invalidateAfterRequest() {
  const qc = getQueryClient()
  if (!qc) return
  void qc.invalidateQueries({ queryKey: ['requests'] })
  void qc.invalidateQueries({ queryKey: ['path'] })
  void qc.invalidateQueries({ queryKey: ['mentor'] })
}
