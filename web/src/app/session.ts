import type { QueryClient } from '@tanstack/react-query'

/** Клиентские сторы регистрируют сброс: вход другой учёткой, выход, «Сбросить демо-данные». */
const resetters = new Set<() => void>()

export function registerClientReset(fn: () => void): () => void {
  resetters.add(fn)
  return () => resetters.delete(fn)
}

export function resetClientState(queryClient: QueryClient, opts: { keepMe?: boolean; refetch?: boolean } = {}) {
  resetters.forEach((fn) => fn())
  const filters = {
    predicate: (q: { queryKey: readonly unknown[] }) => {
      const k = q.queryKey[0]
      if (k === 'config' || k === 'auth' || k === 'llm') return false
      if (k === 'me' && opts.keepMe) return false
      return true
    },
  }
  // Сброс демо при открытом экране: открытые панели («Мой путь», «Мои заявки»)
  // должны перечитать данные сразу. removeQueries оставлял их подписчиков на
  // удалённых запросах со старыми данными до ручного «Обновить».
  if (opts.refetch) void queryClient.resetQueries(filters)
  else queryClient.removeQueries(filters)
}
