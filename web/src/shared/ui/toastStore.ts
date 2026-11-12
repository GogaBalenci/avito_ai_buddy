import { create } from 'zustand'

export type ToastTone = 'info' | 'success' | 'error'
export type Toast = {
  id: number
  text: string
  tone: ToastTone
  durationMs: number
  action?: { label: string; onClick: () => void }
}

type ToastState = {
  toasts: Toast[]
  push: (t: Omit<Toast, 'id' | 'durationMs' | 'tone'> & { tone?: ToastTone; durationMs?: number }) => number
  dismiss: (id: number) => void
  clear: () => void
}

let seq = 0

export const useToasts = create<ToastState>((set) => ({
  toasts: [],
  push: (t) => {
    const id = ++seq
    const toast: Toast = { tone: 'info', durationMs: 5000, ...t, id }
    set((s) => ({ toasts: [...s.toasts.slice(-3), toast] }))
    return id
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })),
  clear: () => set({ toasts: [] }),
}))

export const toast = (text: string, opts: Partial<Omit<Toast, 'id' | 'text'>> = {}) => useToasts.getState().push({ text, ...opts })
