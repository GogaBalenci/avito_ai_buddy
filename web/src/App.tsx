import { useState } from 'react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { RouterProvider, type RouterHistory } from '@tanstack/react-router'
import { createQueryClient } from './app/queryClient'
import { createAppRouter } from './app/router'

export function App({ queryClient, history }: { queryClient?: QueryClient; history?: RouterHistory } = {}) {
  const [qc] = useState(() => queryClient ?? createQueryClient())
  const [router] = useState(() => createAppRouter(qc, history))
  return (
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
