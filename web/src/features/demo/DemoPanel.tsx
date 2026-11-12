import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorText } from '@/shared/api/client'
import { DemoFlagListSchema, DemoFlagSchema, type DemoFlag } from '@/shared/api/schemas'
import { Button } from '@/shared/ui/Button'
import { toast } from '@/shared/ui/toastStore'
import { resetClientState } from '@/app/session'

const FLAG_LABELS: Record<string, { title: string; values: Record<string, string> }> = {
  mock_absences_fault: { title: 'Сбой сервиса отсутствий', values: { off: 'нет сбоя', unavailable: 'недоступен', slow: 'отвечает медленно' } },
  access_mentor_approval: { title: 'Одобрение доступа наставником', values: { on: 'включено', off: 'выключено (сразу одобряется)' } },
}

export function useDemoFlags(enabled: boolean) {
  return useQuery({
    queryKey: ['demo', 'flags'],
    queryFn: ({ signal }) => api('/api/demo/flags', { schema: DemoFlagListSchema, signal }),
    enabled,
  })
}

export function useSetFlag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (f: { name: string; value: string }) => api('/api/demo/flags', { body: f, schema: DemoFlagSchema }),
    onSuccess: (flag) => {
      qc.setQueryData(['demo', 'flags'], (old: { items: DemoFlag[] } | undefined) =>
        old ? { items: old.items.map((x) => (x.name === flag.name ? flag : x)) } : old,
      )
      toast(`${FLAG_LABELS[flag.name]?.title ?? flag.name}: ${FLAG_LABELS[flag.name]?.values[flag.value] ?? flag.value}`)
    },
    onError: (e) => toast(errorText(e), { tone: 'error' }),
  })
}

export function useResetDemo() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api('/api/demo/reset', { method: 'POST' }),
    onSuccess: () => {
      resetClientState(qc, { keepMe: true })
      toast('Демо-данные сброшены — можно пройти сценарий заново', { tone: 'success' })
    },
    onError: (e) => toast(errorText(e), { tone: 'error' }),
  })
}

/** Демо-панель для проверяющего: сброс данных и флаги имитаций (10 §11.6). */
export function DemoPanel() {
  const [open, setOpen] = useState(false)
  const flags = useDemoFlags(open)
  const setFlag = useSetFlag()
  const reset = useResetDemo()
  return (
    <div className="demo">
      <Button variant="ghost" size="s" aria-expanded={open} aria-controls="demo-panel" onClick={() => setOpen((v) => !v)}>
        Демо
      </Button>
      {open && (
        <div id="demo-panel" className="demo__panel card" role="dialog" aria-label="Демо-инструменты">
          <h3>Демо-инструменты</h3>
          <p className="subtle">Для проверки прототипа: вернуть данные к исходным и включить сбои имитаций.</p>
          <Button variant="primary" loading={reset.isPending} onClick={() => reset.mutate()}>
            Сбросить демо-данные
          </Button>
          {flags.isError && <p className="fieldset__caption--error">{errorText(flags.error)}</p>}
          {flags.data?.items.map((f) => (
            <label key={f.name} className="field">
              {FLAG_LABELS[f.name]?.title ?? f.name}
              <select
                className="input"
                value={f.value}
                disabled={setFlag.isPending}
                onChange={(e) => setFlag.mutate({ name: f.name, value: e.target.value })}
              >
                {f.allowed_values.map((v) => (
                  <option key={v} value={v}>
                    {FLAG_LABELS[f.name]?.values[v] ?? v}
                  </option>
                ))}
              </select>
            </label>
          ))}
          <Button variant="link" onClick={() => setOpen(false)}>
            Закрыть
          </Button>
        </div>
      )}
    </div>
  )
}
