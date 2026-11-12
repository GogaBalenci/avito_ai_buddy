import { Button } from './Button'

export function ErrorState({ text, onRetry }: { text: string; onRetry?: () => void }) {
  return (
    <div className="error-state" role="alert">
      <p>{text}</p>
      {onRetry && (
        <Button variant="primary" onClick={onRetry}>
          Повторить
        </Button>
      )}
    </div>
  )
}
