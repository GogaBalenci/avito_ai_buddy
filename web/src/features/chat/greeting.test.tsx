import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { server } from '@/test/server'
import { errorBody } from '@/test/handlers'
import * as f from '@/test/fixtures'
import { renderApp } from '@/test/render'
import { contextGreeting, hourIn, partOfDay, shortPeriod, STATIC_GREETING } from './greeting'

const chat = () => screen.getByRole('region', { name: 'Чат с Витей' })
const greeting = async () => within(await screen.findByRole('region', { name: 'Чат с Витей' })).findByTestId('greeting')

// 09:00 по Москве = 06:00 UTC
const morning = new Date('2026-11-12T06:00:00Z')

describe('context greeting (09 §17.6, AG-32): pure functions', () => {
  it('part of day by config.timezone', () => {
    expect(partOfDay(morning, 'Europe/Moscow')).toBe('Доброе утро')
    expect(partOfDay(new Date('2026-11-12T10:00:00Z'), 'Europe/Moscow')).toBe('Добрый день')
    expect(partOfDay(new Date('2026-11-12T16:00:00Z'), 'Europe/Moscow')).toBe('Добрый вечер')
    expect(partOfDay(new Date('2026-11-12T21:30:00Z'), 'Europe/Moscow')).toBe('Доброй ночи')
    expect(partOfDay(morning, 'Asia/Tokyo')).toBe('Добрый день') // 15:00 в Токио
    expect(hourIn(morning, 'Not/AZone')).toBe(morning.getHours())
  })

  it('ready: period, day, steps left and the next step', () => {
    const p = f.path({ progress: { period_done: 2, period_total: 12, overall_done: 2, overall_total: 13 } })
    const g = contextGreeting(p, morning, 'Europe/Moscow')
    expect(g.text).toBe('Доброе утро! Неделя 1, день 4 из 7, осталось 10 шагов. Начнём с «Попроси добавить тебя во все ключевые каналы и чаты»?')
    expect(g.step?.step_id).toBe('cm-1.7')
    expect(shortPeriod('Месяц 2 — Самостоятельные задачи')).toBe('Месяц 2')
  })

  it('ready edge cases: open-ended period, plural, no next step, unknown period', () => {
    const open = f.path({
      day: 40,
      current_period: 'month2',
      periods: [{ id: 'month2', title: 'Месяц 2', from_day: 31, to_day: null, state: 'current', done: 0, total: 1 }],
      progress: { period_done: 0, period_total: 1, overall_done: 0, overall_total: 1 },
      next_step: null,
    })
    expect(contextGreeting(open, morning, 'Europe/Moscow').text).toBe('Доброе утро! Месяц 2, день 10, осталось 1 шаг.')
    const three = f.path({ progress: { period_done: 3, period_total: 6, overall_done: 3, overall_total: 7 }, current_period: 'after3' })
    expect(contextGreeting(three, morning, 'Europe/Moscow').text).toBe('Доброе утро! осталось 3 шага. Начнём с «Попроси добавить тебя во все ключевые каналы и чаты»?')
    const none = f.path({ progress: { period_done: 6, period_total: 6, overall_done: 6, overall_total: 7 }, current_period: 'after3', next_step: null })
    expect(contextGreeting(none, morning, 'Europe/Moscow')).toEqual({ text: 'Доброе утро!', step: null })
  })

  it('pre_start, finished, only_waiting', () => {
    expect(contextGreeting(f.path({ state: 'pre_start', start_date: '2026-11-16' }), morning, 'Europe/Moscow').text).toBe(
      'Доброе утро! Стажировка начнётся 16.11. Можно заглянуть в первую неделю.',
    )
    expect(contextGreeting(f.path({ state: 'finished' }), morning, 'Europe/Moscow').text).toBe('Доброе утро! Все шаги пройдены 🎉 Обсуди итоги с наставником на 1:1.')
    expect(contextGreeting(f.path({ state: 'only_waiting' }), morning, 'Europe/Moscow').text).toBe(
      'Доброе утро! Осталось дождаться ответа по «Запроси доступ в пространство своего кластера и команды и изучи его». А пока спрашивай меня о команде и процессах.',
    )
    expect(contextGreeting(f.path({ state: 'only_waiting', waiting: [] }), morning, 'Europe/Moscow').text).toBe(
      'Доброе утро! Осталось дождаться ответа. А пока спрашивай меня о команде и процессах.',
    )
  })
})

describe('context greeting in the chat (MSW)', () => {
  it('loading: the static greeting until the path arrives', async () => {
    server.use(http.get('/api/path', async () => {
      await delay('infinite')
      return HttpResponse.json(f.path())
    }))
    renderApp('/')
    const g = await greeting()
    expect(within(g).getByText(STATIC_GREETING)).toBeInTheDocument()
    expect(within(g).getByRole('button', { name: 'Что мне делать дальше?' })).toBeInTheDocument()
  })

  it('error: the static greeting', async () => {
    let calls = 0
    server.use(http.get('/api/path', () => {
      calls++
      return errorBody(400, 'BAD_REQUEST', 'Сломано')
    }))
    renderApp('/')
    const g = await greeting()
    await waitFor(() => expect(calls).toBeGreaterThan(0))
    await new Promise((r) => setTimeout(r, 50))
    expect(within(g).getByText(STATIC_GREETING)).toBeInTheDocument()
  })

  it('ready: text from the path and the next step button sends step:<id>', async () => {
    const seen: { payload?: string }[] = []
    server.use(http.post('/api/chat/messages', async ({ request }) => {
      const b = (await request.json()) as { client_message_id: string; payload?: string }
      seen.push(b)
      return HttpResponse.json(f.reply({ client_message_id: b.client_message_id, reply_id: crypto.randomUUID(), text: 'Ок' }))
    }))
    renderApp('/')
    const g = await greeting()
    expect(await within(g).findByText(/Неделя 1, день 4 из 7, осталось 4 шага\. Начнём с «Попроси добавить тебя во все ключевые каналы и чаты»\?/)).toBeInTheDocument()
    expect(within(g).getByText(/^(Доброе утро|Добрый день|Добрый вечер|Доброй ночи)!/)).toBeInTheDocument()
    expect(within(g).getByRole('button', { name: 'Оформить отгул' })).toBeInTheDocument()
    await userEvent.click(within(g).getByRole('button', { name: 'Запросить доступ: Попроси добавить тебя во все ключевые каналы и чаты' }))
    expect(await within(chat()).findByText('Ок')).toBeInTheDocument()
    expect(seen[0]).toMatchObject({ payload: 'step:cm-1.7' })
  })

  it.each([
    ['pre_start', { state: 'pre_start' as const, start_date: '2026-11-16' }, /Стажировка начнётся 16\.11/],
    ['finished', { state: 'finished' as const, next_step: null }, /Все шаги пройдены 🎉/],
    ['only_waiting', { state: 'only_waiting' as const, next_step: null }, /Осталось дождаться ответа по «Запроси доступ/],
  ])('%s', async (_name, over, text) => {
    server.use(http.get('/api/path', () => HttpResponse.json(f.path(over))))
    renderApp('/')
    const g = await greeting()
    expect(await within(g).findByText(text)).toBeInTheDocument()
    expect(within(g).getByRole('button', { name: 'Что мне делать дальше?' })).toBeInTheDocument()
  })
})
