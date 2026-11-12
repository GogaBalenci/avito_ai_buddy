import { useRef, useState } from 'react'
import { api } from '@/shared/api/client'
import { copyText, MANUAL_COPY_HINT } from '@/shared/lib/clipboard'
import { Button } from '@/shared/ui/Button'
import { toast } from '@/shared/ui/toastStore'

type CopyEvent = 'escalation_copied' | 'doc_conflict_reported'

export function reportCopy(event: CopyEvent | undefined, replyId: string | undefined) {
  if (!event || !replyId) return
  void api('/api/client-events', { body: { name: event, reply_id: replyId } }).catch(() => undefined)
}

/**
 * «Скопировать»: в буфер обмена; если браузер запретил — текст выделяется в поле
 * с подсказкой «Скопируй вручную: Ctrl+C» (09 §8.2, F-10, KF-07).
 */
export function CopyButton({ label, text, event, replyId, variant = 'secondary' }: { label: string; text: string; event?: CopyEvent; replyId?: string; variant?: 'primary' | 'secondary' }) {
  const [manual, setManual] = useState(false)
  const ref = useRef<HTMLTextAreaElement>(null)
  return (
    <div className="copy">
      <Button
        size="s"
        variant={variant}
        onClick={async () => {
          const ok = await copyText(text)
          reportCopy(event, replyId)
          if (ok) {
            setManual(false)
            toast('Скопировано', { tone: 'success', durationMs: 2500 })
          } else {
            setManual(true)
            setTimeout(() => ref.current?.select(), 0)
          }
        }}
      >
        {label}
      </Button>
      {manual && (
        <div className="copy__manual">
          <textarea ref={ref} className="textarea" readOnly value={text} aria-label="Текст для копирования" />
          <p className="subtle">{MANUAL_COPY_HINT}</p>
        </div>
      )}
    </div>
  )
}

/** Черновик вопроса наставнику: редактируется, «Скопировать» берёт текущий текст (KF-04). */
export function EscalationDraft({ draft, replyId, readOnly }: { draft: string; replyId: string; readOnly?: boolean }) {
  const [text, setText] = useState(draft)
  return (
    <div className="escalation">
      <label className="field">
        Черновик сообщения наставнику
        <textarea className="textarea" value={text} readOnly={readOnly} onChange={(e) => setText(e.target.value)} />
      </label>
      <CopyButton label="Скопировать" text={text} event="escalation_copied" replyId={replyId} variant="primary" />
      <p className="subtle">Сам я наставнику не пишу — отправь сообщение в мессенджере.</p>
    </div>
  )
}
