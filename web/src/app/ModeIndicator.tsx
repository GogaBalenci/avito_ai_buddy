import { useQuery } from '@tanstack/react-query'
import { configQuery, llmHealthQuery } from '@/shared/api/queries'
import { Chip } from '@/shared/ui/Chip'
import type { AppConfig, LlmHealth, Notice } from '@/shared/api/schemas'
import { useChat } from '@/features/chat/chatStore'
import { NOTICE_TEXT } from '@/features/chat/replyLogic'

export type Mode = { label: string; tone: 'neutral' | 'success' | 'warning'; title: string }

/** Индикатор режима в шапке (09 §13): demo — штатный режим без модели. */
export function describeMode(config?: AppConfig, health?: LlmHealth): Mode {
  if (!config || config.llm_provider === 'demo') {
    return { label: 'Режим demo', tone: 'neutral', title: 'Ответы по правилам и шаблонам, без языковой модели' }
  }
  const model = health?.model ? ` · ${health.model}` : ''
  switch (health?.status) {
    case 'ok':
      return { label: `LLM${model}`, tone: 'success', title: 'Модель распознаёт намерения и переформулирует ответы' }
    case 'auth_error':
      return { label: 'Упрощённый режим', tone: 'warning', title: 'Ключ LLM не принят — работаю по правилам. Проверь LLM_API_KEY' }
    case 'misconfigured':
      return health?.last_error_code === 'invalid_LLM_PROVIDER'
        ? { label: 'Упрощённый режим', tone: 'warning', title: 'LLM_PROVIDER: допустимы demo и openai_compatible — работаю по правилам' }
        : { label: 'Упрощённый режим', tone: 'warning', title: 'LLM не настроена (нет LLM_BASE_URL или LLM_MODEL) — работаю по правилам' }
    case 'disabled':
      return { label: 'Упрощённый режим', tone: 'warning', title: 'LLM выключена флагом — работаю по правилам' }
    case 'circuit_open':
    case 'degraded':
      return { label: `LLM${model} · сбои`, tone: 'warning', title: 'Модель отвечает с ошибками — временно работаю по правилам' }
    default:
      return { label: `LLM${model}`, tone: 'neutral', title: 'Проверяю модель…' }
  }
}

const LLM_NOTICES: Notice[] = ['llm_auth_error', 'llm_misconfigured', 'simplified_mode']

/** Последняя плашка llm_* из ответов чата — остаётся значком у индикатора режима (09 §9.1). */
export function lastLlmNotice(notices: Notice[][]): Notice | null {
  for (let i = notices.length - 1; i >= 0; i--) {
    const n = notices[i].find((x) => LLM_NOTICES.includes(x))
    if (n) return n
  }
  return null
}

export function ModeIndicator() {
  const config = useQuery(configQuery)
  const health = useQuery({ ...llmHealthQuery, enabled: config.data?.llm_provider === 'openai_compatible' })
  const notice = useChat((s) => lastLlmNotice(s.entries.map((e) => e.reply?.notices ?? [])))
  const mode = describeMode(config.data, health.data)
  return (
    <span className="mode" title={mode.title}>
      <Chip tone={mode.tone}>{mode.label}</Chip>
      {notice && (
        <span className="mode__warn" role="img" aria-label={NOTICE_TEXT[notice]} title={NOTICE_TEXT[notice]}>
          ⚠
        </span>
      )}
      <span className="sr-only">{mode.title}</span>
    </span>
  )
}
