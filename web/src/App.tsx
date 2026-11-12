import { useState } from 'react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { RouterProvider, type RouterHistory } from '@tanstack/react-router'
import { createQueryClient } from './app/queryClient'
import { createAppRouter } from './app/router'
import { setQueryClient } from './app/queryClientRef'

export function App({ queryClient, history }: { queryClient?: QueryClient; history?: RouterHistory } = {}) {
  const [qc] = useState(() => {
    const c = queryClient ?? createQueryClient()
    setQueryClient(c)
    return c
  })
  const [router] = useState(() => createAppRouter(qc, history))
  return (
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
