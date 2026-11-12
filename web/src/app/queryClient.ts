import { QueryClient } from '@tanstack/react-query'
import { ApiError } from '@/shared/api/client'

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // 4xx не повторяем: это ответ, а не сбой
        retry: (count, e) => count < 1 && !(e instanceof ApiError && e.status >= 400 && e.status < 500),
        refetchOnWindowFocus: true,
        staleTime: 5_000,
      },
      mutations: { retry: false },
    },
  })
}
