import { useMentorInbox } from './InboxPage'

/** Бейдж «Входящие · N» в шапке наставника. */
export function InboxBadge() {
  const q = useMentorInbox()
  const n = q.data?.items.length ?? 0
  if (!n) return null
  return (
    <span className="badge" aria-label={`${n} новых`}>
      {n}
    </span>
  )
}
