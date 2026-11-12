import { useEffect } from 'react'
import { useToasts, type Toast } from './toastStore'

function ToastItem({ t }: { t: Toast }) {
  const dismiss = useToasts((s) => s.dismiss)
  useEffect(() => {
    const id = setTimeout(() => dismiss(t.id), t.durationMs)
    return () => clearTimeout(id)
  }, [t.id, t.durationMs, dismiss])
  return (
    <div className={`toast toast--${t.tone}`} role={t.tone === 'error' ? 'alert' : 'status'}>
      <span>{t.text}</span>
      {t.action && (
        <button
          type="button"
          className="toast__action"
          onClick={() => {
            t.action?.onClick()
            dismiss(t.id)
          }}
        >
          {t.action.label}
        </button>
      )}
      <button type="button" className="toast__close" aria-label="Закрыть уведомление" onClick={() => dismiss(t.id)}>
        ×
      </button>
    </div>
  )
}

export function Toaster() {
  const toasts = useToasts((s) => s.toasts)
  return (
    <div className="toaster" aria-live="polite">
      {toasts.map((t) => (
        <ToastItem key={t.id} t={t} />
      ))}
    </div>
  )
}
