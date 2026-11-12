import { queryOptions } from '@tanstack/react-query'
import { api } from './client'
import { AppConfigSchema, LlmHealthSchema, MeSchema, TestAccountListSchema } from './schemas'

export const meQuery = queryOptions({
  queryKey: ['me'],
  // 401 здесь не вызывает глобальный обработчик: гард сам ведёт на /login
  queryFn: ({ signal }) => api('/api/me', { schema: MeSchema, signal, silent401: true }),
  staleTime: 5 * 60_000,
})

export const configQuery = queryOptions({
  queryKey: ['config'],
  queryFn: ({ signal }) => api('/api/config', { schema: AppConfigSchema, signal }),
  staleTime: Infinity,
})

export const llmHealthQuery = queryOptions({
  queryKey: ['llm', 'health'],
  queryFn: ({ signal }) => api('/api/llm/health', { schema: LlmHealthSchema, signal }),
  staleTime: 30_000,
  refetchInterval: 60_000,
  refetchIntervalInBackground: false,
})

export const accountsQuery = queryOptions({
  queryKey: ['auth', 'accounts'],
  queryFn: ({ signal }) => api('/api/auth/accounts', { schema: TestAccountListSchema, signal }),
  staleTime: Infinity,
})
