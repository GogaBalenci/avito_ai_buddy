import { useEffect, useState } from 'react'

// Заглушка каркаса (C0): показывает, что web → nginx → api → PostgreSQL/Redis работает.
// Экраны «Мой путь» и чат появятся на C3.

type Health = { status: 'ok' | 'degraded'; postgres: boolean; redis: boolean }
type State = { kind: 'loading' } | { kind: 'ready'; health: Health } | { kind: 'error'; message: string }

export function App() {
  const [state, setState] = useState<State>({ kind: 'loading' })

  const check = () => {
    setState({ kind: 'loading' })
    fetch('/healthz', { credentials: 'same-origin' })
      .then(async (res) => {
        const health = (await res.json()) as Health
        setState({ kind: 'ready', health })
      })
      .catch(() => setState({ kind: 'error', message: 'API недоступен' }))
  }

  useEffect(check, [])

  return (
    <main className="card">
      <h1>Авиитик Витя</h1>
      <p className="muted">AI-buddy для стажёров · каркас стенда</p>
      {state.kind === 'loading' && <p>Проверяю сервисы…</p>}
      {state.kind === 'error' && <p className="bad">{state.message}</p>}
      {state.kind === 'ready' && (
        <ul>
          <li className={state.health.status === 'ok' ? 'good' : 'bad'}>
            Статус: {state.health.status === 'ok' ? 'всё работает' : 'есть проблемы'}
          </li>
          <li>PostgreSQL: {state.health.postgres ? 'доступен' : 'недоступен'}</li>
          <li>Redis: {state.health.redis ? 'доступен' : 'недоступен'}</li>
        </ul>
      )}
      <button type="button" onClick={check}>
        Проверить ещё раз
      </button>
    </main>
  )
}
