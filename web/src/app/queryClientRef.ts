import type { QueryClient } from '@tanstack/react-query'

/** Сторы Zustand вне React инвалидируют кэш через этот указатель (его ставит App). */
let current: QueryClient | null = null

export function setQueryClient(qc: QueryClient) {
  current = qc
}

export function getQueryClient(): QueryClient | null {
  return current
}
