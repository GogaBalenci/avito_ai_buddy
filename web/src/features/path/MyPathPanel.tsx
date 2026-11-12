import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { errorText } from '@/shared/api/client'
import { configQuery } from '@/shared/api/queries'
import type { PathStep } from '@/shared/api/schemas'
import { Button } from '@/shared/ui/Button'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'
import { Vitya } from '@/shared/mascot/Vitya'
import { sendPayload, useChat } from '@/features/chat/chatStore'
import { buttonLabel, panelState } from './logic'
import { PathPanel } from './PathPanel'
import { reportNextStepClick, useMarkStep, usePath } from './usePath'

/** Панель «Мой путь» стажёра: все 7 состояний (11 §1.7). */
export function MyPathPanel({ children }: { children?: ReactNode }) {
  const q = usePath()
  const config = useQuery(configQuery)
  const mark = useMarkStep()
  const busy = useChat((s) => s.inFlight !== null)
  const state = panelState(q)

  const handlers = {
    onStep: (s: PathStep) => void sendPayload(`step:${s.step_id}`, buttonLabel(s)),
    onMark: (s: PathStep) => mark.mark(s.step_id, s.status),
    onReopen: (s: PathStep) => mark.reopen(s.step_id),
    onNextClick: (s: PathStep) => reportNextStepClick(s.step_id),
  }

  return (
    <div className="stack">
      <div className="panel-head">
        <h2>Мой путь</h2>
        <Button size="s" variant="ghost" loading={q.isFetching && !q.isPending} onClick={() => void q.refetch()}>
          Обновить
        </Button>
      </div>
      {state === 'loading' && <Skeleton lines={6} label="Загружаю твой путь" />}
      {state === 'error' && (
        <div className="path__empty">
          <Vitya state="oops" size={64} />
          <ErrorState text={`Не получилось загрузить твой путь. ${errorText(q.error)}`} onRetry={() => void q.refetch()} />
        </div>
      )}
      {q.data && (
        <PathPanel path={q.data} handlers={handlers} busy={busy} timeZone={config.data?.timezone} stale={state === 'stale'} onRetry={() => void q.refetch()}>
          {children}
        </PathPanel>
      )}
    </div>
  )
}
