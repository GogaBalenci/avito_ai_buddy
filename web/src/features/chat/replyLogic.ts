import type { ChatEntry } from './chatStore'
import type { Notice, Source } from '@/shared/api/schemas'

export const NOTICE_TEXT: Record<Notice, string> = {
  simplified_mode: 'Модель сейчас не отвечает — работаю в упрощённом режиме',
  llm_auth_error: 'Ключ LLM не принят — работаю в упрощённом режиме. Проверь LLM_API_KEY',
  llm_misconfigured: 'LLM не настроена — работаю в упрощённом режиме',
  synthetic_track: 'Тестовый трек: шаги придуманы для прототипа',
  pii_masked: 'Скрыл личные данные из сообщения — они не сохраняются',
  restricted_doc_exists: 'Есть документ с ограниченным доступом — подробности у наставника',
  doc_conflict: 'Инструкции расходятся — показываю обе, уточни у наставника',
  overlap_unchecked: 'Пересечения с другими отгулами проверю при отправке',
}

export const NOTICE_TONE: Record<Notice, 'test' | 'warning' | 'info'> = {
  simplified_mode: 'warning',
  llm_auth_error: 'warning',
  llm_misconfigured: 'warning',
  synthetic_track: 'test',
  pii_masked: 'info',
  restricted_doc_exists: 'info',
  doc_conflict: 'warning',
  overlap_unchecked: 'info',
}

const ONCE: Notice[] = ['simplified_mode', 'llm_auth_error', 'llm_misconfigured']

/** Плашки llm_* — один раз за сессию (09 §9.1): только у первого ответа с этой пометкой. */
export function visibleNotices(entries: ChatEntry[], index: number): Notice[] {
  const notices = entries[index]?.reply?.notices ?? []
  return notices.filter((n) => {
    if (!ONCE.includes(n)) return true
    return !entries.slice(0, index).some((e) => e.reply?.notices.includes(n))
  })
}

export function sourceLabel(s: Pick<Source, 'label'>): string {
  return s.label === 'test_description' ? 'Тестовое описание — уточни у наставника' : 'Тестовая инструкция'
}

/** Текст последнего сообщения стажёра перед ответом — для «Повторить» без payload. */
export function previousUserText(entries: ChatEntry[], replyId: string): string | undefined {
  const i = entries.findIndex((e) => e.reply?.reply_id === replyId)
  for (let j = i - 1; j >= 0; j--) if (entries[j].author === 'user' && entries[j].text) return entries[j].text
  return undefined
}
