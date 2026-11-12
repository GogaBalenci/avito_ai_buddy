import { addDays, dayTime, ddmm, ddmmyyyy, hhmm, isWeekend, parseIsoDate, plural, rangeText, toIso, weekday, workingDays } from './dates'
import { newId } from './uuid'
import { copyText } from './clipboard'

describe('dates', () => {
  it('parses strict ISO dates', () => {
    expect(parseIsoDate('2026-11-13')).toBe(Date.UTC(2026, 10, 13))
    expect(parseIsoDate('2026-02-30')).toBeNull()
    expect(parseIsoDate('13.11.2026')).toBeNull()
    expect(parseIsoDate('')).toBeNull()
  })
  it('weekdays and working days do not depend on the browser time zone', () => {
    const fri = parseIsoDate('2026-11-13')!
    const mon = parseIsoDate('2026-11-16')!
    expect(weekday(fri)).toBe(5)
    expect(isWeekend(parseIsoDate('2026-11-14')!)).toBe(true)
    expect(workingDays(fri, mon)).toBe(2)
  })
  it('formats', () => {
    expect(ddmm('2026-11-13')).toBe('13.11')
    expect(ddmm('bad')).toBe('bad')
    expect(ddmmyyyy('2027-02-08')).toBe('08.02.2027')
    expect(ddmmyyyy('bad')).toBe('bad')
    expect(rangeText('2026-11-13', '2026-11-16')).toBe('13.11–16.11')
    expect(rangeText('2026-11-13', '2026-11-13')).toBe('13.11')
    expect(rangeText('2026-11-13')).toBe('13.11')
    expect(rangeText()).toBe('')
    expect(addDays('2026-11-13', 3)).toBe('2026-11-16')
    expect(addDays('bad', 3)).toBe('bad')
    expect(toIso(Date.UTC(2026, 0, 2))).toBe('2026-01-02')
    expect(hhmm('2026-11-12T09:05:00Z')).toBe('12:05')
    expect(hhmm('nope')).toBe('')
    expect(dayTime('2026-11-12T09:05:00Z')).toBe('12.11 в 12:05')
    expect(dayTime('nope')).toBe('')
  })
  it('plural', () => {
    expect(plural(1, ['день', 'дня', 'дней'])).toBe('день')
    expect(plural(3, ['день', 'дня', 'дней'])).toBe('дня')
    expect(plural(5, ['день', 'дня', 'дней'])).toBe('дней')
    expect(plural(12, ['день', 'дня', 'дней'])).toBe('дней')
    expect(plural(21, ['день', 'дня', 'дней'])).toBe('день')
  })
})

describe('uuid', () => {
  it('creates v4 ids, also without crypto.randomUUID', () => {
    const re = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    expect(newId()).toMatch(re)
    const orig = globalThis.crypto.randomUUID
    Object.defineProperty(globalThis.crypto, 'randomUUID', { value: undefined, configurable: true })
    try {
      expect(newId()).toMatch(re)
    } finally {
      Object.defineProperty(globalThis.crypto, 'randomUUID', { value: orig, configurable: true })
    }
  })
})

describe('clipboard', () => {
  it('copies when allowed and reports failure otherwise', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    expect(await copyText('a')).toBe(true)
    writeText.mockRejectedValueOnce(new Error('denied'))
    expect(await copyText('a')).toBe(false)
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    expect(await copyText('a')).toBe(false)
  })
})
