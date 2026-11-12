import { useQuery } from '@tanstack/react-query'
import { configQuery, llmHealthQuery } from '@/shared/api/queries'
import { Chip } from '@/shared/ui/Chip'
import type { AppConfig, LlmHealth } from '@/shared/api/schemas'

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
      return { label: 'Упрощённый режим', tone: 'warning', title: 'LLM не настроена (нет LLM_BASE_URL или LLM_MODEL) — работаю по правилам' }
    case 'disabled':
      return { label: 'Упрощённый режим', tone: 'warning', title: 'LLM выключена флагом — работаю по правилам' }
    case 'circuit_open':
    case 'degraded':
      return { label: `LLM${model} · сбои`, tone: 'warning', title: 'Модель отвечает с ошибками — временно работаю по правилам' }
    default:
      return { label: `LLM${model}`, tone: 'neutral', title: 'Проверяю модель…' }
  }
}

export function ModeIndicator() {
  const config = useQuery(configQuery)
  const health = useQuery({ ...llmHealthQuery, enabled: config.data?.llm_provider === 'openai_compatible' })
  const mode = describeMode(config.data, health.data)
  return (
    <span className="mode" title={mode.title}>
      <Chip tone={mode.tone}>{mode.label}</Chip>
      <span className="sr-only">{mode.title}</span>
    </span>
  )
}
