import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { api, errorText, isApiError } from '@/shared/api/client'
import { configQuery } from '@/shared/api/queries'
import { MentorInternListSchema, PathViewSchema } from '@/shared/api/schemas'
import { rangeText } from '@/shared/lib/dates'
import { Chip } from '@/shared/ui/Chip'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'
import { Vitya } from '@/shared/mascot/Vitya'
import { PathPanel } from '@/features/path/PathPanel'

const PERIOD: Record<string, string> = { week1: 'Неделя 1', week2: 'Неделя 2', month1: 'Недели 3–4', month2: 'Месяц 2', month3: 'Месяц 3', after3: 'После 3 месяцев' }

export function useMentorInterns() {
  const config = useQuery(configQuery)
  return useQuery({
    queryKey: ['mentor', 'interns'],
    queryFn: ({ signal }) => api('/api/mentor/interns', { schema: MentorInternListSchema, signal }),
    refetchInterval: config.data?.poll_interval_ms ?? 10_000,
    refetchIntervalInBackground: false,
  })
}

/** «Мои стажёры» (11 MP-08): прогресс, заявки у наставника, ближайший отгул. */
export function MentorInternsPage() {
  const q = useMentorInterns()
  return (
    <main className="page">
      <h1>Мои стажёры</h1>
      <p className="muted">Прогресс по пути адаптации — только просмотр. Отмечает шаги сам стажёр.</p>
      {q.isPending && <Skeleton lines={4} label="Загружаю стажёров" />}
      {q.isError && <ErrorState text={errorText(q.error)} onRetry={() => void q.refetch()} />}
      {q.data && q.data.items.length === 0 && (
        <div className="path__empty">
          <Vitya state="hello" size={64} />
          <p>Стажёров пока нет</p>
        </div>
      )}
      {q.data && q.data.items.length > 0 && (
        <ul className="interns">
          {q.data.items.map((i) => (
            <li key={i.user_id} className="card intern">
              <div className="row">
                <Link to="/mentor/interns/$userId" params={{ userId: i.user_id }} className="intern__name">
                  {i.display_name}
                </Link>
                <Chip>{i.track.title}</Chip>
                {i.track.synthetic && <Chip tone="test">тестовый трек</Chip>}
              </div>
              <p className="subtle">
                {PERIOD[i.current_period] ?? i.current_period} · день {i.day}
              </p>
              <div className="progress">
                <div className="progress__label">
                  <span>Готово на этапе</span>
                  <span>
                    {i.progress.period_done} из {i.progress.period_total}
                  </span>
                </div>
                <div className="progress__track" role="progressbar" aria-label={`Прогресс: ${i.display_name}`} aria-valuemin={0} aria-valuemax={i.progress.period_total} aria-valuenow={i.progress.period_done}>
                  <div className="progress__bar" style={{ width: `${i.progress.period_total ? (i.progress.period_done / i.progress.period_total) * 100 : 0}%` }} />
                </div>
              </div>
              <div className="row">
                {i.pending_access_requests > 0 ? (
                  <Link to="/mentor/inbox">
                    <Chip tone="warning">Заявок на доступ: {i.pending_access_requests}</Chip>
                  </Link>
                ) : (
                  <span className="subtle">Заявок на доступ нет</span>
                )}
                {i.upcoming_time_off.length > 0 ? (
                  <Chip tone="info">Ближайший отгул: {i.upcoming_time_off.map((t) => rangeText(t.date_from, t.date_to)).join(', ')}</Chip>
                ) : (
                  <span className="subtle">Отгулов не запланировано</span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}

/** Путь стажёра глазами наставника — только чтение (кнопок действий нет, PF-06). */
export function MentorInternPathPage() {
  const { userId } = useParams({ strict: false }) as { userId: string }
  const config = useQuery(configQuery)
  const q = useQuery({
    queryKey: ['mentor', 'path', userId],
    queryFn: ({ signal }) => api(`/api/mentor/interns/${encodeURIComponent(userId)}/path`, { schema: PathViewSchema, signal }),
  })
  return (
    <main className="page">
      <Link to="/mentor/interns">← Мои стажёры</Link>
      {q.isPending && <Skeleton lines={6} label="Загружаю путь стажёра" />}
      {q.isError &&
        (isApiError(q.error, 404) ? (
          <p>Такого стажёра нет среди твоих</p>
        ) : (
          <ErrorState text={`Не получилось загрузить путь. ${errorText(q.error)}`} onRetry={() => void q.refetch()} />
        ))}
      {q.data && (
        <>
          <h1>{q.data.employee.display_name}</h1>
          <p className="muted">Трек {q.data.employee.track.title} · только просмотр</p>
          <div className="card">
            <PathPanel path={q.data} timeZone={config.data?.timezone} />
          </div>
        </>
      )}
    </main>
  )
}
