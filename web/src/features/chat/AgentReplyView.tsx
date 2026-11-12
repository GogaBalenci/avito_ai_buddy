import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { apiRequest, errorText } from '@/shared/api/client'
import type { AgentReply, PathView, ReplyAction } from '@/shared/api/schemas'
import { Button } from '@/shared/ui/Button'
import { Plaque } from '@/shared/ui/Chip'
import { toast } from '@/shared/ui/toastStore'
import { useMarkStep } from '@/features/path/usePath'
import { TimeOffFlow } from '@/features/time-off/TimeOffFlow'
import { PendingActionCard } from '@/features/actions/PendingActionCard'
import { sendPayload, useChat } from './chatStore'
import { CopyButton, EscalationDraft } from './CopyBox'
import { kbSlug, ReplyText } from './ReplyText'
import { NOTICE_TEXT, NOTICE_TONE, sourceLabel } from './replyLogic'
import type { Notice } from '@/shared/api/schemas'

function Feedback({ replyId, actions }: { replyId: string; actions: ReplyAction[] }) {
  const [voted, setVoted] = useState<'up' | 'down' | null>(null)
  return (
    <div className="row feedback" role="group" aria-label="Оценка ответа">
      {actions.map((a) => (
        <Button
          key={a.value}
          size="s"
          variant={voted === a.value ? 'primary' : 'ghost'}
          aria-pressed={voted === a.value}
          onClick={async () => {
            if (voted === a.value || !a.value) return
            const prev = voted
            setVoted(a.value)
            try {
              const r = await apiRequest('/api/feedback', { body: { reply_id: replyId, value: a.value } })
              // двойное «Не то» → эскалация (11 §2.6.5, X-Buddy-Escalation)
              if (r.headers.get('X-Buddy-Escalation')) void sendPayload('intent:ask_mentor', 'Спросить наставника')
            } catch (e) {
              setVoted(prev)
              toast(errorText(e), { tone: 'error' })
            }
          }}
        >
          {a.value === 'up' ? '👍 ' : '👎 '}
          {a.label}
        </Button>
      ))}
    </div>
  )
}

function MarkDoneButton({ action }: { action: ReplyAction }) {
  const qc = useQueryClient()
  const mark = useMarkStep()
  const busy = mark.isPending
  return (
    <Button
      size="s"
      variant="primary"
      loading={busy}
      onClick={() => {
        const id = action.step_id ?? ''
        const step = qc.getQueryData<PathView>(['path'])?.steps.find((s) => s.step_id === id)
        if (step?.status === 'done') {
          toast(`Шаг «${step.title}» уже отмечен`)
          return
        }
        mark.mark(id, step?.status ?? 'in_progress')
      }}
    >
      {action.label}
    </Button>
  )
}

type Props = { reply: AgentReply; notices: Notice[]; fromHistory?: boolean; prevUserText?: string; formActive?: boolean }

/** Ответ бадди (09 §9.1): текст с маркерами, плашки, источники, черновики, действия. */
export function AgentReplyView({ reply, notices, fromHistory, prevUserText, formActive }: Props) {
  const busy = useChat((s) => s.inFlight !== null)
  const feedback = reply.actions.filter((a) => a.type === 'feedback')
  const hasEscalationCopy = !!reply.escalation
  const buttons = reply.actions.filter(
    (a) => !['feedback', 'open_form', 'confirm', 'cancel', 'edit'].includes(a.type) && !(hasEscalationCopy && a.type === 'copy' && a.copy_event === 'escalation_copied'),
  )
  const form = reply.actions.find((a) => a.type === 'open_form' && a.form === 'time_off')

  return (
    <div className="reply">
      {notices.map((n) => (
        <Plaque key={n} tone={NOTICE_TONE[n]}>
          {NOTICE_TEXT[n]}
        </Plaque>
      ))}
      <ReplyText text={reply.text} sources={reply.sources} />

      {reply.sources.length > 0 && (
        <ul className="sources" aria-label="Источники">
          {reply.sources.map((s) => {
            const slug = kbSlug(s.url)
            return (
              <li key={s.n} className="source">
                <span className="source__n">[{s.n}]</span>
                {slug ? (
                  <Link to="/kb/$slug" params={{ slug }}>
                    {s.title}
                  </Link>
                ) : (
                  <span>{s.title}</span>
                )}
                <span className={`chip ${s.label === 'test_description' ? 'chip--warning' : 'chip--test'}`}>{sourceLabel(s)}</span>
                {s.dated && <span className="subtle">{s.dated}</span>}
              </li>
            )
          })}
        </ul>
      )}

      {reply.escalation && <EscalationDraft draft={reply.escalation.draft} replyId={reply.reply_id} />}

      {form && <TimeOffFlow flowId={reply.reply_id} action={form} inactive={fromHistory || !formActive} />}

      {reply.pending_action && <PendingActionCard initial={reply.pending_action} fromHistory={fromHistory} />}

      {buttons.length > 0 && (
        <div className="row reply__actions">
          {buttons.map((a, i) => {
            switch (a.type) {
              case 'quick_reply':
              case 'request_access':
                return a.payload ? (
                  <Button key={i} size="s" disabled={busy} onClick={() => void sendPayload(a.payload!, a.label)}>
                    {a.label}
                  </Button>
                ) : null
              case 'retry':
                return (
                  <Button
                    key={i}
                    size="s"
                    variant="primary"
                    disabled={busy}
                    onClick={() => {
                      if (a.payload) void sendPayload(a.payload, a.label)
                      else if (prevUserText) void useChat.getState().send({ text: prevUserText })
                    }}
                  >
                    {a.label}
                  </Button>
                )
              case 'copy':
                return a.text ? <CopyButton key={i} label={a.label} text={a.text} event={a.copy_event} replyId={reply.reply_id} /> : null
              case 'link':
                return a.url?.startsWith('/') ? (
                  <Link key={i} to={a.url} className="btn btn--ghost btn--s">
                    {a.label}
                  </Link>
                ) : null
              case 'mark_done':
                return <MarkDoneButton key={i} action={a} />
              default:
                return null
            }
          })}
        </div>
      )}

      {feedback.length > 0 && <Feedback replyId={reply.reply_id} actions={feedback} />}
    </div>
  )
}
