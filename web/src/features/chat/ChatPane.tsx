import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, errorText } from '@/shared/api/client'
import { HistoryPageSchema } from '@/shared/api/schemas'
import { Button } from '@/shared/ui/Button'
import { Vitya } from '@/shared/mascot/Vitya'
import { selectMascot, useMascot } from '@/shared/mascot/mascotStore'
import { sendPayload, useChat, type ChatEntry } from './chatStore'
import { AgentReplyView } from './AgentReplyView'
import { previousUserText, visibleNotices } from './replyLogic'

const MAX_LEN = 1000

const STARTERS: { label: string; payload: string }[] = [
  { label: 'Что мне делать дальше?', payload: 'intent:my_path' },
  { label: 'Оформить отгул', payload: 'intent:time_off' },
  { label: 'Запросить доступ', payload: 'intent:access_request' },
  { label: 'Что ты умеешь?', payload: 'intent:help' },
]

export function useChatHistory() {
  const loadHistory = useChat((s) => s.loadHistory)
  const q = useQuery({
    queryKey: ['chat', 'history'],
    queryFn: ({ signal }) => api('/api/chat/history?limit=50', { schema: HistoryPageSchema, signal }),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  })
  useEffect(() => {
    if (q.data) loadHistory(q.data.items)
  }, [q.data, loadHistory])
  return q
}

function UserMessage({ e }: { e: ChatEntry }) {
  const retry = useChat((s) => s.retry)
  const busy = useChat((s) => s.inFlight !== null)
  return (
    <div className="msg msg--user">
      <div className="bubble bubble--user">{e.buttonLabel ?? e.text}</div>
      {e.failed && (
        <div className="msg__failed" role="alert">
          <span>{e.failed.text}</span>
          {e.failed.retryable && (
            <Button size="s" variant="link" disabled={busy} onClick={() => void retry(e.id)}>
              Повторить
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

/** Чат с Витей (09 §2.1, AG-18): один запрос в полёте, поле ввода и панель не блокируются. */
export function ChatPane() {
  const history = useChatHistory()
  const entries = useChat((s) => s.entries)
  const inFlight = useChat((s) => s.inFlight)
  const slow = useChat((s) => s.slow)
  const send = useChat((s) => s.send)
  const cancel = useChat((s) => s.cancel)
  const mascot = useMascot((s) => selectMascot(s, inFlight !== null))
  const [text, setText] = useState('')
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [entries.length, inFlight])

  const lastFormIndex = entries.reduce((acc, e, i) => (e.reply?.actions.some((a) => a.type === 'open_form') ? i : acc), -1)
  const canSend = !inFlight && text.trim().length > 0

  const submit = () => {
    if (!canSend) return
    const t = text
    setText('')
    void send({ text: t })
  }

  return (
    <div className="chat">
      <div className="chat__head">
        <Vitya state={mascot} size={56} />
        <div>
          <strong>Витя</strong>
          <div className="subtle" aria-live="polite">
            {inFlight ? (slow ? 'Думаю чуть дольше обычного…' : 'Думаю…') : 'Твой бадди на стажировке'}
          </div>
        </div>
        {inFlight && slow && (
          <Button size="s" variant="ghost" onClick={cancel}>
            Отменить
          </Button>
        )}
      </div>

      <div className="chat__list" ref={listRef} role="log" aria-label="Сообщения">
        {history.isPending && <p className="subtle">Загружаю историю…</p>}
        {history.isError && (
          <p className="subtle" role="status">
            История не загрузилась: {errorText(history.error)}{' '}
            <Button variant="link" onClick={() => void history.refetch()}>
              Повторить
            </Button>
          </p>
        )}
        {!history.isPending && entries.length === 0 && (
          <div className="msg msg--bot">
            <div className="bubble">
              <p className="reply__text">Привет! Я Витя, твой бадди на стажировке. Подскажу, что делать дальше, отвечу на вопросы о команде и инструментах, оформлю отгул или заявку на доступ.</p>
              <div className="row reply__actions">
                {STARTERS.map((s) => (
                  <Button key={s.payload} size="s" disabled={!!inFlight} onClick={() => void sendPayload(s.payload, s.label)}>
                    {s.label}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        )}
        {entries.map((e, i) =>
          e.author === 'user' ? (
            <UserMessage key={e.id} e={e} />
          ) : e.reply ? (
            <div key={e.id} className="msg msg--bot">
              <div className="bubble">
                <AgentReplyView reply={e.reply} notices={visibleNotices(entries, i)} fromHistory={e.fromHistory} prevUserText={previousUserText(entries, e.reply.reply_id)} formActive={i === lastFormIndex} />
              </div>
            </div>
          ) : null,
        )}
        {inFlight && (
          <div className="msg msg--bot">
            <div className="bubble bubble--thinking" aria-label="Витя печатает ответ">
              <span className="dots" aria-hidden="true">
                <i />
                <i />
                <i />
              </span>
            </div>
          </div>
        )}
      </div>

      <form
        className="composer"
        onSubmit={(ev) => {
          ev.preventDefault()
          submit()
        }}
      >
        <label className="sr-only" htmlFor="composer-input">
          Сообщение Вите
        </label>
        <textarea
          id="composer-input"
          className="composer__input"
          value={text}
          maxLength={MAX_LEN}
          rows={1}
          placeholder="Спроси Витю: «что мне делать дальше?», «хочу отгул», «что такое tech-dep»"
          onChange={(ev) => setText(ev.target.value)}
          onKeyDown={(ev) => {
            if (ev.key === 'Enter' && !ev.shiftKey) {
              ev.preventDefault()
              submit()
            }
          }}
        />
        <Button type="submit" variant="primary" disabled={!canSend}>
          Отправить
        </Button>
      </form>
    </div>
  )
}
