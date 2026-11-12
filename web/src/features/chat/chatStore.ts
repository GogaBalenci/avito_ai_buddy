// Чат: один запрос в полёте, client_message_id создаёт клиент, повтор — с тем же UUID
// (09 §2.1, AG-18). `thinking` у Вити — только на клиенте; через 5 с — «дольше обычного».
import { create } from 'zustand'
import { api, ApiError, errorText } from '@/shared/api/client'
import { AgentReplySchema, type AgentReply, type HistoryItem } from '@/shared/api/schemas'
import { newId } from '@/shared/lib/uuid'
import { useMascot } from '@/shared/mascot/mascotStore'
import { toast } from '@/shared/ui/toastStore'
import { getQueryClient } from '@/app/queryClientRef'
import { registerClientReset } from '@/app/session'

export const SLOW_MS = 5000

export type Outgoing = { client_message_id: string; text?: string; payload?: string; buttonLabel?: string }

export type ChatEntry = {
  id: string
  author: 'user' | 'bot'
  ts: string
  text?: string
  buttonLabel?: string
  reply?: AgentReply
  fromHistory?: boolean
  /** Не дошло: можно повторить тем же client_message_id. */
  failed?: { text: string; retryable: boolean }
  outgoing?: Outgoing
}

type ChatState = {
  entries: ChatEntry[]
  historyLoaded: boolean
  inFlight: Outgoing | null
  slow: boolean
  loadHistory: (items: HistoryItem[]) => void
  send: (msg: { text?: string; payload?: string; buttonLabel?: string }) => Promise<AgentReply | null>
  retry: (entryId: string) => Promise<AgentReply | null>
  cancel: () => void
  appendBot: (reply: AgentReply) => void
  reset: () => void
}

let controller: AbortController | null = null
let slowTimer: ReturnType<typeof setTimeout> | null = null

const now = () => new Date().toISOString()

/** Ответы, после которых сервер мог поменять шаг или заявку. */
const PATH_INTENTS = new Set(['step_explain', 'step_done', 'my_path', 'access_request', 'time_off'])

function afterReply(reply: AgentReply) {
  useMascot.getState().setServer(reply.mascot_state)
  const qc = getQueryClient()
  if (qc && PATH_INTENTS.has(reply.intent)) void qc.invalidateQueries({ queryKey: ['path'] })
}

function clearTimers() {
  if (slowTimer) clearTimeout(slowTimer)
  slowTimer = null
  controller = null
}

async function deliver(out: Outgoing, set: (p: Partial<ChatState> | ((s: ChatState) => Partial<ChatState>)) => void): Promise<AgentReply | null> {
  controller = new AbortController()
  set({ inFlight: out, slow: false })
  slowTimer = setTimeout(() => set({ slow: true }), SLOW_MS)
  const body: Record<string, string> = { client_message_id: out.client_message_id }
  if (out.payload) body.payload = out.payload
  else body.text = out.text ?? ''
  try {
    const reply = await api('/api/chat/messages', { body, schema: AgentReplySchema, signal: controller.signal })
    clearTimers()
    set((s) => ({
      inFlight: null,
      slow: false,
      entries: [
        ...s.entries.map((e) => (e.id === out.client_message_id ? { ...e, failed: undefined } : e)),
        { id: reply.reply_id, author: 'bot' as const, ts: now(), reply },
      ],
    }))
    afterReply(reply)
    return reply
  } catch (e) {
    clearTimers()
    const err = e instanceof ApiError ? e : null
    // Сервер вернул готовый ответ бадди в теле ошибки (например, 503 с текстом «сервис недоступен»)
    if (err?.body?.reply) {
      const reply = err.body.reply
      set((s) => ({ inFlight: null, slow: false, entries: [...s.entries, { id: reply.reply_id, author: 'bot' as const, ts: now(), reply }] }))
      afterReply(reply)
      return reply
    }
    let text = errorText(e)
    let retryable = err ? err.retryable : true
    if (err?.kind === 'aborted') text = 'Отменено'
    if (err?.status === 400 && out.payload) {
      text = 'Не получилось обработать кнопку — обнови страницу'
      retryable = false
    }
    if (err?.status === 404 && out.payload) {
      // PF-10: шага нет в пути — тост и перечитать путь
      toast(err.message, { tone: 'error' })
      void getQueryClient()?.invalidateQueries({ queryKey: ['path'] })
      retryable = false
    }
    if (err?.status === 403 && out.payload) retryable = false
    if (err?.kind === 'network' || err?.kind === 'bad_response' || (err && err.status >= 500)) useMascot.getState().setClient('oops')
    set((s) => ({
      inFlight: null,
      slow: false,
      entries: s.entries.map((x) => (x.id === out.client_message_id ? { ...x, failed: { text, retryable } } : x)),
    }))
    return null
  }
}

export const useChat = create<ChatState>((set, get) => ({
  entries: [],
  historyLoaded: false,
  inFlight: null,
  slow: false,

  loadHistory: (items) => {
    if (get().historyLoaded) return
    const fromHistory: ChatEntry[] = items.map((h) => ({
      id: h.id,
      author: h.author,
      ts: h.ts,
      text: h.text ?? undefined,
      buttonLabel: h.button_label ?? undefined,
      reply: h.reply ?? undefined,
      fromHistory: true,
    }))
    // локальные сообщения, отправленные до загрузки истории, остаются в конце
    set((s) => ({ historyLoaded: true, entries: [...fromHistory, ...s.entries.filter((e) => !fromHistory.some((h) => h.id === e.id))] }))
    const lastBot = [...fromHistory].reverse().find((e) => e.reply)
    if (lastBot?.reply) useMascot.getState().setServer(lastBot.reply.mascot_state)
  },

  send: async ({ text, payload, buttonLabel }) => {
    if (get().inFlight) return null
    const trimmed = text?.trim()
    if (!payload && !trimmed) return null
    const out: Outgoing = { client_message_id: newId(), text: trimmed, payload, buttonLabel }
    set((s) => ({
      entries: [...s.entries, { id: out.client_message_id, author: 'user', ts: now(), text: trimmed, buttonLabel, outgoing: out }],
    }))
    return deliver(out, set)
  },

  retry: async (entryId) => {
    if (get().inFlight) return null
    const entry = get().entries.find((e) => e.id === entryId)
    if (!entry?.outgoing) return null
    set((s) => ({ entries: s.entries.map((x) => (x.id === entryId ? { ...x, failed: undefined } : x)) }))
    return deliver(entry.outgoing, set)
  },

  cancel: () => controller?.abort(),

  appendBot: (reply) => {
    set((s) => ({ entries: [...s.entries, { id: reply.reply_id, author: 'bot', ts: now(), reply }] }))
    afterReply(reply)
  },

  reset: () => {
    controller?.abort()
    clearTimers()
    set({ entries: [], historyLoaded: false, inFlight: null, slow: false })
    useMascot.getState().reset()
  },
}))

registerClientReset(() => useChat.getState().reset())

/** Кнопки шагов, быстрые ответы, «Оформить отгул»: payload с новым UUID на каждое нажатие. */
export function sendPayload(payload: string, buttonLabel: string) {
  return useChat.getState().send({ payload, buttonLabel })
}
