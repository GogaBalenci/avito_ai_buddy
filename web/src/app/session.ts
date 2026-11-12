import type { QueryClient } from '@tanstack/react-query'

/** Клиентские сторы регистрируют сброс: вход другой учёткой, выход, «Сбросить демо-данные». */
const resetters = new Set<() => void>()

export function registerClientReset(fn: () => void): () => void {
  resetters.add(fn)
  return () => resetters.delete(fn)
}

export function resetClientState(queryClient: QueryClient, opts: { keepMe?: boolean } = {}) {
  resetters.forEach((fn) => fn())
  queryClient.removeQueries({
    predicate: (q) => {
      const k = q.queryKey[0]
      if (k === 'config' || k === 'auth' || k === 'llm') return false
      if (k === 'me' && opts.keepMe) return false
      return true
    },
  })
}
