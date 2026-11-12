import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError, errorText } from '@/shared/api/client'
import { TimeOffConfigSchema, type ReplyAction } from '@/shared/api/schemas'
import { rangeText } from '@/shared/lib/dates'
import { useMascot } from '@/shared/mascot/mascotStore'
import { cancelAction, confirmAction, createTimeOffAction, invalidateAfterRequest } from '@/features/actions/actionsApi'
import { errorsFromServer, errorsFromViolations, useTimeOffFlows, type Errors } from './flow'

export const SLOW_MS = 5000

export const timeOffConfigQuery = {
  queryKey: ['timeOffConfig'],
  queryFn: ({ signal }: { signal: AbortSignal }) => api('/api/time-off/config', { schema: TimeOffConfigSchema, signal }),
  staleTime: 60_000,
}

export function mentorMessage(from?: string, to?: string, id?: string) {
  return `Привет! Оформил(а) отгул на ${rangeText(from, to)} (заявка ${id}). Если до этого нужно что-то передать — скажи.`
}

const FIRST: ('date_from' | 'date_to' | 'range')[] = ['date_from', 'date_to', 'range']

/**
 * Состояние формы — в сторе (useTimeOffFlows), запросы — здесь; компонент тонкий (10 §9.3).
 * Через 5 с ожидания — «Сервис отвечает дольше обычного…» (F-06).
 */
export function useTimeOffForm(flowId: string, action: ReplyAction, inactive: boolean) {
  const config = useQuery(timeOffConfigQuery)
  const flow = useTimeOffFlows((s) => s.flows[flowId])
  const open = useTimeOffFlows((s) => s.open)
  const dispatch = useTimeOffFlows((s) => s.dispatch)
  const focusRef = useRef<((field: 'date_from' | 'date_to' | 'range') => void) | null>(null)

  useEffect(() => {
    if (!inactive && config.data) open(flowId, action, config.data)
  }, [inactive, config.data, flowId, action, open])

  const withSlow = async <T,>(run: () => Promise<T>): Promise<T> => {
    const t = setTimeout(() => dispatch(flowId, { type: 'slow' }), SLOW_MS)
    useMascot.getState().setClient('thinking')
    try {
      return await run()
    } finally {
      clearTimeout(t)
    }
  }

  const focusFirst = (errors: Errors) => {
    const f = FIRST.find((k) => errors[k])
    if (f) setTimeout(() => focusRef.current?.(f), 0)
  }

  const submit = async () => {
    const f = useTimeOffFlows.getState().flows[flowId]
    if (!f || !config.data) return
    const errors = errorsFromViolations(f.from, f.to, config.data, true)
    if (Object.keys(errors).length) {
      dispatch(flowId, { type: 'client_invalid', errors })
      focusFirst(errors)
      return
    }
    dispatch(flowId, { type: 'submit_start' })
    try {
      const res = await withSlow(() => createTimeOffAction(f.from, f.to))
      useMascot.getState().setClient('hello')
      dispatch(flowId, { type: 'submit_ok', draft: res.pending_action, overlapUnchecked: res.notices.includes('overlap_unchecked') })
    } catch (e) {
      const err = e instanceof ApiError ? e : null
      if (err?.status === 422 || err?.status === 409) {
        useMascot.getState().setClient('hello')
        const errs = err.body?.errors?.length ? errorsFromServer(err.body.errors) : { range: { code: err.code, message: err.message } }
        dispatch(flowId, { type: 'server_invalid', errors: errs })
        focusFirst(errs)
      } else {
        useMascot.getState().setClient('oops')
        dispatch(flowId, { type: 'submit_failed', message: `Не получилось проверить даты. ${errorText(e)}` })
      }
    }
  }

  const confirm = async () => {
    const f = useTimeOffFlows.getState().flows[flowId]
    const id = f?.draft?.action_id
    if (!id) return
    dispatch(flowId, { type: 'confirm_start' })
    try {
      const res = await withSlow(() => confirmAction(id))
      useMascot.getState().setServer(res.reply.mascot_state)
      const copy = res.reply.actions.find((a) => a.type === 'copy')?.text
      dispatch(flowId, { type: 'confirm_ok', result: res.result, copyText: copy ?? mentorMessage(res.result.date_from, res.result.date_to, res.result.request_id) })
      invalidateAfterRequest()
    } catch (e) {
      const err = e instanceof ApiError ? e : null
      if (err?.status === 422) {
        useMascot.getState().setClient('hello')
        const errs = errorsFromServer(err.body?.errors)
        dispatch(flowId, { type: 'server_invalid', errors: Object.keys(errs).length ? errs : { range: { code: err.code, message: err.message } } })
        focusFirst(errs)
      } else if (err?.status === 410 || err?.status === 404) {
        // F-08: 410 — форма с прежними датами; 404 — пустая форма
        useMascot.getState().setClient('oops')
        dispatch(flowId, { type: 'expired', keepDates: err.status === 410 })
      } else {
        useMascot.getState().setClient('oops')
        dispatch(flowId, { type: 'service_error', message: err?.status === 403 || err?.status === 409 ? err.message : 'Сервис отсутствий не отвечает, заявка не создана' })
      }
    }
  }

  const cancel = async () => {
    const f = useTimeOffFlows.getState().flows[flowId]
    dispatch(flowId, { type: 'cancel' })
    useMascot.getState().setClient('hello')
    if (f?.draft && (f.state === 'draft' || f.state === 'service_error')) await cancelAction(f.draft.action_id).catch(() => undefined)
  }

  return {
    config: config.data,
    configError: config.isError ? errorText(config.error) : null,
    retryConfig: () => void config.refetch(),
    flow,
    set: (field: 'from' | 'to', value: string) => dispatch(flowId, { type: 'set', field, value, config: config.data }),
    submit,
    confirm,
    cancel,
    edit: () => dispatch(flowId, { type: 'edit' }),
    focusRef,
  }
}
