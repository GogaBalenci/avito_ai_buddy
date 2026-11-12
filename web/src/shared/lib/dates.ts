// Даты без времени — только через Date.UTC/getUTC*: часовой пояс браузера эксперта
// не сдвигает день недели (10 §6.3). «Сегодня» берётся с сервера, не из часов браузера.

const ISO = /^(\d{4})-(\d{2})-(\d{2})$/

/** Строгий разбор YYYY-MM-DD → миллисекунды UTC, или null (2026-02-30 → null). */
export function parseIsoDate(s: string): number | null {
  const m = ISO.exec(s.trim())
  if (!m) return null
  const y = Number(m[1])
  const mo = Number(m[2])
  const d = Number(m[3])
  const t = Date.UTC(y, mo - 1, d)
  const back = new Date(t)
  if (back.getUTCFullYear() !== y || back.getUTCMonth() !== mo - 1 || back.getUTCDate() !== d) return null
  return t
}

export const DAY_MS = 86_400_000

export function toIso(t: number): string {
  return new Date(t).toISOString().slice(0, 10)
}

export function addDays(iso: string, n: number): string {
  const t = parseIsoDate(iso)
  return t === null ? iso : toIso(t + n * DAY_MS)
}

/** 0 — воскресенье … 6 — суббота. */
export function weekday(t: number): number {
  return new Date(t).getUTCDay()
}

export function isWeekend(t: number): boolean {
  const w = weekday(t)
  return w === 0 || w === 6
}

export function workingDays(from: number, to: number): number {
  let n = 0
  for (let t = from; t <= to; t += DAY_MS) if (!isWeekend(t)) n++
  return n
}

/** 13.11 */
export function ddmm(iso: string): string {
  const m = ISO.exec(iso)
  return m ? `${m[3]}.${m[2]}` : iso
}

/** 13.11.2026 */
export function ddmmyyyy(iso: string): string {
  const m = ISO.exec(iso)
  return m ? `${m[3]}.${m[2]}.${m[1]}` : iso
}

/** 13.11–14.11 или 13.11 для одного дня. */
export function rangeText(from?: string, to?: string): string {
  if (!from) return ''
  if (!to || to === from) return ddmm(from)
  return `${ddmm(from)}–${ddmm(to)}`
}

/** HH:MM момента времени в часовом поясе стенда (по умолчанию Europe/Moscow). */
export function hhmm(dateTime: string, timeZone = 'Europe/Moscow'): string {
  const d = new Date(dateTime)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit', timeZone }).format(d)
}

/** «13.11 в 12:00» */
export function dayTime(dateTime: string, timeZone = 'Europe/Moscow'): string {
  const d = new Date(dateTime)
  if (Number.isNaN(d.getTime())) return ''
  const day = new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: '2-digit', timeZone }).format(d)
  return `${day} в ${hhmm(dateTime, timeZone)}`
}

/** Склонение: plural(2, ['день', 'дня', 'дней']). */
export function plural(n: number, forms: [string, string, string]): string {
  const a = Math.abs(n) % 100
  const b = a % 10
  if (a > 10 && a < 20) return forms[2]
  if (b > 1 && b < 5) return forms[1]
  if (b === 1) return forms[0]
  return forms[2]
}
