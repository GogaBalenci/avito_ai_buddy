import { create } from 'zustand'
import type { VityaState } from './Vitya'

/**
 * Состояние маскота. Сервер присылает hello/done/oops/ask_mentor в ответе,
 * `thinking` ставит только клиент (09 §10.2); клиент также ставит `oops` при
 * ошибке сети и `done`, когда опрос увидел одобрение заявки.
 */
type MascotStore = {
  server: VityaState
  client: VityaState | null
  setServer: (s: VityaState) => void
  setClient: (s: VityaState | null) => void
  reset: () => void
}

export const useMascot = create<MascotStore>((set) => ({
  server: 'hello',
  client: null,
  setServer: (s) => set({ server: s, client: null }),
  setClient: (s) => set({ client: s }),
  reset: () => set({ server: 'hello', client: null }),
}))

export function selectMascot(s: Pick<MascotStore, 'server' | 'client'>, thinking: boolean): VityaState {
  if (thinking) return 'thinking'
  return s.client ?? s.server
}
