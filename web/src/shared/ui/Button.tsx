import type { ButtonHTMLAttributes } from 'react'

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'link'
  size?: 's' | 'm'
  loading?: boolean
}

/** Кнопка дизайн-системы: основная — чёрная (контрастный стиль), второстепенная — серая. */
export function Button({ variant = 'secondary', size = 'm', loading = false, className, children, disabled, type = 'button', ...rest }: Props) {
  return (
    <button
      type={type}
      className={['btn', `btn--${variant}`, `btn--${size}`, loading ? 'btn--loading' : '', className ?? ''].join(' ').trim()}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading && <span className="spinner" aria-hidden="true" />}
      {children}
    </button>
  )
}
