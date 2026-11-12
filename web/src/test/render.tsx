import { render } from '@testing-library/react'
import { createMemoryHistory } from '@tanstack/react-router'
import { QueryClient } from '@tanstack/react-query'
import { App } from '@/App'
import { ApiError } from '@/shared/api/client'
import { resetClientState } from '@/app/session'
import { useToasts } from '@/shared/ui/toastStore'

export function testQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: (n, e) => n < 0 && !(e instanceof ApiError), gcTime: Infinity, staleTime: 0, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  })
}

/** Рендерит всё приложение с роутером на заданном адресе. */
export function renderApp(path = '/', queryClient = testQueryClient()) {
  resetClientState(queryClient)
  useToasts.getState().clear()
  const history = createMemoryHistory({ initialEntries: [path] })
  const utils = render(<App queryClient={queryClient} history={history} />)
  return { ...utils, history, queryClient }
}
