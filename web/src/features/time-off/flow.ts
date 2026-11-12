// Автомат формы «от — до» (10 §9.2): 14 состояний, чистый редьюсер + стор Zustand.
import { create } from 'zustand'
import type { FieldError, PendingAction, ReplyAction, RequestView, TimeOffConfig } from '@/shared/api/schemas'
import { registerClientReset } from '@/app/session'
import { validateTimeOff, violationMessage, type TimeOffField } from './rules'

export type FlowState =
  | 'empty' | 'prefilled' | 'editing' | 'invalid_client' | 'invalid_server' | 'submitting' | 'draft'
  | 'confirming' | 'done' | 'service_error' | 'failed' | 'expired' | 'cancelled' | 'superseded'

export type FieldErr = { code: string; message: string; requestId?: string }
export type Errors = Partial<Record<TimeOffField, FieldErr>>

export type Flow = {
  flowId: string
  state: FlowState
  from: string
  to: string
  errors: Errors
  prefilled: boolean
  existing: NonNullable<ReplyAction['existing']>
  draft?: PendingAction
  overlapUnchecked?: boolean
  result?: RequestView
  copyText?: string
  message?: string
  slow?: boolean
  /** Пользователь пытался отправить — показываем и REQUIRED. */
  touched?: boolean
}

export type FlowEvent =
  | { type: 'set'; field: 'from' | 'to'; value: string; config?: TimeOffConfig }
  | { type: 'submit_start' }
  | { type: 'client_invalid'; errors: Errors }
  | { type: 'submit_ok'; draft: PendingAction; overlapUnchecked: boolean }
  | { type: 'server_invalid'; errors: Errors }
  | { type: 'submit_failed'; message: string }
  | { type: 'edit' }
  | { type: 'cancel' }
  | { type: 'confirm_start' }
  | { type: 'confirm_ok'; result: RequestView; copyText?: string }
  | { type: 'service_error'; message: string }
  | { type: 'expired'; keepDates: boolean }
  | { type: 'slow' }
  | { type: 'supersede' }

/** Состояния, в которых форма ещё «жива» и её можно заменить новой. */
export const LIVE: FlowState[] = ['empty', 'prefilled', 'editing', 'invalid_client', 'invalid_server', 'failed', 'expired', 'draft', 'service_error']

export function errorsFromViolations(from: string, to: string, config: TimeOffConfig | undefined, showRequired: boolean): Errors {
  if (!config) return {}
  const out: Errors = {}
  for (const v of validateTimeOff(from, to, config).violations) {
    if (v.code === 'REQUIRED' && !showRequired) continue
    if (!out[v.field]) out[v.field] = { code: v.code, message: violationMessage(v, config) }
  }
  return out
}

export function errorsFromServer(list: FieldError[] | undefined): Errors {
  const out: Errors = {}
  for (const e of list ?? []) {
    const field = e.field === 'date_from' || e.field === 'date_to' ? e.field : 'range'
    if (!out[field]) out[field] = { code: e.code, message: e.message, requestId: typeof e.params?.request_id === 'string' ? e.params.request_id : undefined }
  }
  return out
}

export function initialFlow(flowId: string, action: Pick<ReplyAction, 'prefill' | 'existing'>, config?: TimeOffConfig): Flow {
  const from = action.prefill?.date_from ?? ''
  const to = action.prefill?.date_to ?? ''
  const prefilled = !!action.prefill
  const errors = prefilled ? errorsFromViolations(from, to, config, false) : {}
  return { flowId, state: prefilled ? 'prefilled' : 'empty', from, to, errors, prefilled, existing: action.existing ?? [] }
}

const hasErrors = (e: Errors) => Object.keys(e).length > 0

export function reduce(f: Flow, ev: FlowEvent): Flow {
  switch (ev.type) {
    case 'set': {
      if (!['empty', 'prefilled', 'editing', 'invalid_client', 'invalid_server', 'failed', 'expired'].includes(f.state)) return f
      let { from, to } = f
      if (ev.field === 'from') {
        from = ev.value
        // F-03: «от» выбрано, «до» пустое → «до» = «от»
        if (!to && from) to = from
      } else to = ev.value
      const errors = errorsFromViolations(from, to, ev.config, !!f.touched)
      return { ...f, from, to, errors, state: hasErrors(errors) ? 'invalid_client' : 'editing', message: undefined }
    }
    case 'submit_start':
      return { ...f, state: 'submitting', touched: true, slow: false, message: undefined }
    case 'client_invalid':
      return { ...f, state: 'invalid_client', errors: ev.errors, touched: true }
    case 'submit_ok':
      return { ...f, state: 'draft', draft: ev.draft, overlapUnchecked: ev.overlapUnchecked, errors: {}, slow: false }
    case 'server_invalid':
      return { ...f, state: 'invalid_server', errors: ev.errors, slow: false }
    case 'submit_failed':
      return { ...f, state: 'failed', message: ev.message, slow: false }
    case 'edit':
      return f.state === 'draft' ? { ...f, state: 'editing', errors: {} } : f
    case 'cancel':
      return { ...f, state: 'cancelled', slow: false }
    case 'confirm_start':
      return { ...f, state: 'confirming', slow: false, message: undefined }
    case 'confirm_ok':
      return { ...f, state: 'done', result: ev.result, copyText: ev.copyText, slow: false }
    case 'service_error':
      return { ...f, state: 'service_error', message: ev.message, slow: false }
    case 'expired':
      return { ...f, state: 'expired', from: ev.keepDates ? f.from : '', to: ev.keepDates ? f.to : '', draft: undefined, errors: {}, slow: false }
    case 'slow':
      return f.state === 'submitting' || f.state === 'confirming' ? { ...f, slow: true } : f
    case 'supersede':
      return LIVE.includes(f.state) ? { ...f, state: 'superseded' } : f
  }
}

type Store = {
  flows: Record<string, Flow>
  activeId: string | null
  open: (flowId: string, action: ReplyAction, config?: TimeOffConfig) => void
  dispatch: (flowId: string, ev: FlowEvent) => void
  reset: () => void
}

export const useTimeOffFlows = create<Store>((set, get) => ({
  flows: {},
  activeId: null,
  open: (flowId, action, config) => {
    if (get().flows[flowId]) return
    set((s) => {
      const flows = { ...s.flows }
      // одна активная форма: предыдущая становится superseded (10 §5.1, F-16)
      if (s.activeId && flows[s.activeId]) flows[s.activeId] = reduce(flows[s.activeId], { type: 'supersede' })
      flows[flowId] = initialFlow(flowId, action, config)
      return { flows, activeId: flowId }
    })
  },
  dispatch: (flowId, ev) =>
    set((s) => (s.flows[flowId] ? { flows: { ...s.flows, [flowId]: reduce(s.flows[flowId], ev) } } : s)),
  reset: () => set({ flows: {}, activeId: null }),
}))

registerClientReset(() => useTimeOffFlows.getState().reset())
