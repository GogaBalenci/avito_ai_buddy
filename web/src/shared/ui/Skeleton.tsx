export function Skeleton({ lines = 3, label = 'Загрузка' }: { lines?: number; label?: string }) {
  return (
    <div className="skeleton" role="status" aria-label={label}>
      {Array.from({ length: lines }, (_, i) => (
        <div key={i} className="skeleton__line" style={{ width: `${90 - i * 12}%` }} />
      ))}
    </div>
  )
}
