import type { ReactNode } from 'react'

export type ChipTone = 'neutral' | 'info' | 'success' | 'warning' | 'danger' | 'test'

export function Chip({ tone = 'neutral', icon, children, title }: { tone?: ChipTone; icon?: ReactNode; children: ReactNode; title?: string }) {
  return (
    <span className={`chip chip--${tone}`} title={title}>
      {icon && (
        <span className="chip__icon" aria-hidden="true">
          {icon}
        </span>
      )}
      {children}
    </span>
  )
}

/** Плашка: тестовая инструкция, тестовый трек, режим LLM и т. п. */
export function Plaque({ tone = 'test', children }: { tone?: 'test' | 'warning' | 'info'; children: ReactNode }) {
  return <div className={`plaque plaque--${tone}`}>{children}</div>
}
