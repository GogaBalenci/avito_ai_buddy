import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, errorText } from '@/shared/api/client'
import { configQuery } from '@/shared/api/queries'
import { PathViewSchema, StepStatusResultSchema, type PathView, type StepStatus } from '@/shared/api/schemas'
import { newId } from '@/shared/lib/uuid'
import { useMascot } from '@/shared/mascot/mascotStore'
import { toast } from '@/shared/ui/toastStore'
import { applyStatus, REJECT_REASON, requestTransitions } from './logic'
import { celebrate } from './celebrate'

export const PATH_KEY = ['path'] as const

/** Общие опции запроса пути: панель и приветствие чата читают один кэш. */
export const pathQuery = {
  queryKey: PATH_KEY,
  queryFn: ({ signal }: { signal: AbortSignal }) => api('/api/path', { schema: PathViewSchema, signal }),
}

/** Путь стажёра. Опрос — только при has_pending_requests и только в активной вкладке (11 §3.6). */
export function usePath() {
  const config = useQuery(configQuery)
  const interval = config.data?.poll_interval_ms ?? 10_000
  const q = useQuery({
    ...pathQuery,
    refetchInterval: (query) => (query.state.data?.has_pending_requests ? interval : false),
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
  })
  usePathTransitions(q.data)
  return q
}

function usePathTransitions(data: PathView | undefined) {
  const prev = useRef<PathView | undefined>(undefined)
  useEffect(() => {
    if (!data) return
    const transitions = requestTransitions(prev.current, data)
    for (const t of transitions) {
      if (t.to === 'approved') {
        toast(`Доступ по шагу «${t.step.title}» одобрен`, { tone: 'success' })
        useMascot.getState().setClient('done')
      } else {
        const reason = t.step.request?.reject_reason
        toast(`Заявку по шагу «${t.step.title}» отклонили${reason ? `: ${REJECT_REASON[reason] ?? reason}` : ''}`, { tone: 'error', durationMs: 8000 })
      }
    }
    if (transitions.some((t) => t.to === 'approved')) celebrate(prev.current, data) // 09 §17.8
    prev.current = data
  }, [data])
}

export type MarkVars = { stepId: string; to: Extract<StepStatus, 'done' | 'in_progress'>; expected: StepStatus; key: string; undo?: boolean }

/**
 * «Отметить пройденным» / «Вернуть в работу»: оптимистично, откат при ошибке,
 * тост «Отменить» 5 с, повтор после сбоя сети — с тем же idempotency_key (PF-03, P-A8, P-A9).
 */
export function useMarkStep() {
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: (v: MarkVars) =>
      api(`/api/path/steps/${encodeURIComponent(v.stepId)}/status`, {
        body: { status: v.to, expected_status: v.expected, idempotency_key: v.key },
        schema: StepStatusResultSchema,
      }),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: PATH_KEY })
      const snapshot = qc.getQueryData<PathView>(PATH_KEY)
      if (snapshot) qc.setQueryData<PathView>(PATH_KEY, applyStatus(snapshot, v.stepId, v.to))
      return { snapshot }
    },
    onSuccess: (res, v, ctx) => {
      qc.setQueryData<PathView>(PATH_KEY, (old) =>
        old
          ? {
              ...applyStatus(old, res.step.step_id, res.step.status),
              steps: old.steps.map((s) => (s.step_id === res.step.step_id ? res.step : s)),
              state: res.path_summary.state,
              next_step: res.path_summary.next_step,
              waiting: res.path_summary.waiting,
              progress: res.path_summary.progress,
            }
          : old,
      )
      if (v.to === 'done') {
        useMascot.getState().setClient('done')
        celebrate(ctx?.snapshot, qc.getQueryData<PathView>(PATH_KEY)) // только по ответу сервера (09 §17.8)
        toast(`Отмечено: «${res.step.title}»`, {
          tone: 'success',
          action: { label: 'Отменить', onClick: () => m.mutate({ stepId: v.stepId, to: 'in_progress', expected: 'done', key: newId(), undo: true }) },
        })
      } else if (!v.undo) {
        toast(`Шаг «${res.step.title}» снова в работе`)
      }
    },
    onError: (e, v, ctx) => {
      if (ctx?.snapshot) qc.setQueryData(PATH_KEY, ctx.snapshot)
      const err = e instanceof ApiError ? e : null
      if (err?.code === 'STATUS_CONFLICT') {
        toast('Шаг уже обновлён', { tone: 'info' })
        void qc.invalidateQueries({ queryKey: PATH_KEY })
      } else if (err?.kind === 'network' || err?.retryable) {
        useMascot.getState().setClient('oops')
        toast('Не сохранилось', { tone: 'error', action: { label: 'Повторить', onClick: () => m.mutate(v) } })
      } else {
        toast(errorText(e), { tone: 'error' })
        void qc.invalidateQueries({ queryKey: PATH_KEY })
      }
    },
  })
  return {
    ...m,
    mark: (stepId: string, expected: StepStatus) => m.mutate({ stepId, to: 'done', expected, key: newId() }),
    reopen: (stepId: string) => m.mutate({ stepId, to: 'in_progress', expected: 'done', key: newId() }),
  }
}

export function reportNextStepClick(stepId: string) {
  void api('/api/client-events', { body: { name: 'next_step_clicked', step_id: stepId } }).catch(() => undefined)
}
