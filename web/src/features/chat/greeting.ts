// Приветствие пустого чата по данным пути (09 §17.6, AG-32): чистые функции.
import type { PathStep, PathView } from '@/shared/api/schemas'
import { ddmm, plural } from '@/shared/lib/dates'

export const STATIC_GREETING =
  'Привет! Я Витя, твой бадди на стажировке. Подскажу, что делать дальше, отвечу на вопросы о команде и инструментах, оформлю отгул или заявку на доступ.'

/** Час (0–23) в часовом поясе стенда; неверный пояс → локальное время браузера. */
export function hourIn(now: Date, timeZone?: string): number {
  try {
    const h = new Intl.DateTimeFormat('ru-RU', { hour: 'numeric', hourCycle: 'h23', timeZone }).format(now)
    return Number.parseInt(h, 10) % 24
  } catch {
    return now.getHours()
  }
}

/** 5–11 утро, 12–17 день, 18–22 вечер, иначе ночь. */
export function partOfDay(now: Date, timeZone?: string): string {
  const h = hourIn(now, timeZone)
  if (h >= 5 && h < 12) return 'Доброе утро'
  if (h >= 12 && h < 18) return 'Добрый день'
  if (h >= 18 && h < 23) return 'Добрый вечер'
  return 'Доброй ночи'
}

/** «Неделя 1 — Вход в команду» → «Неделя 1». */
export function shortPeriod(title: string): string {
  return title.split(' — ')[0].trim()
}

export type Greeting = { text: string; step: PathStep | null }

export function contextGreeting(p: PathView, now: Date, timeZone?: string): Greeting {
  const hi = partOfDay(now, timeZone)
  switch (p.state) {
    case 'pre_start':
      return { text: `${hi}! Стажировка начнётся ${ddmm(p.start_date)}. Можно заглянуть в первую неделю.`, step: null }
    case 'finished':
      return { text: `${hi}! Все шаги пройдены 🎉 Обсуди итоги с наставником на 1:1.`, step: null }
    case 'only_waiting': {
      const w = p.steps.find((s) => s.step_id === p.waiting[0])
      const what = w ? ` по «${w.title}»` : ''
      return { text: `${hi}! Осталось дождаться ответа${what}. А пока спрашивай меня о команде и процессах.`, step: null }
    }
  }
  const period = p.periods.find((x) => x.id === p.current_period)
  const parts: string[] = []
  if (period) {
    const day = p.day - period.from_day + 1
    parts.push(shortPeriod(period.title))
    parts.push(period.to_day == null ? `день ${day}` : `день ${day} из ${period.to_day - period.from_day + 1}`)
  }
  const left = p.progress.period_total - p.progress.period_done
  if (left > 0) parts.push(`осталось ${left} ${plural(left, ['шаг', 'шага', 'шагов'])}`)
  const step = p.next_step ? (p.steps.find((s) => s.step_id === p.next_step?.step_id) ?? null) : null
  let text = `${hi}!`
  if (parts.length) text += ` ${parts.join(', ')}.`
  if (step) text += ` Начнём с «${step.title}»?`
  return { text, step }
}
