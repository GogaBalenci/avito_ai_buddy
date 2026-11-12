import { useQuery } from '@tanstack/react-query'
import { accountsQuery } from '@/shared/api/queries'
import { errorText } from '@/shared/api/client'
import { Button } from '@/shared/ui/Button'
import { Chip } from '@/shared/ui/Chip'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'
import { Vitya } from '@/shared/mascot/Vitya'
import { useLogin } from './useAuth'

const ROLE: Record<string, string> = { intern: 'Стажёр', mentor: 'Наставник' }

/** Экран входа: тестовые учётки в один клик (имитация SSO, 12 §6). */
export function LoginPage() {
  const accounts = useQuery(accountsQuery)
  const login = useLogin()
  return (
    <main className="login">
      <section className="login__hero">
        <Vitya state="hello" size={112} />
        <div>
          <h1>Привет! Я Авиитик Витя</h1>
          <p className="muted">Авиитик — от «ИИ», коротко Витя. Я бадди на стажировке: подскажу, что делать дальше, отвечу про команду и инструменты, оформлю отгул и заявку на доступ.</p>
          <p className="subtle">Прототип для хакатона: все данные синтетические, инструкции тестовые.</p>
        </div>
      </section>
      <section className="card login__accounts" aria-labelledby="login-title">
        <h2 id="login-title">Войти тестовой учёткой</h2>
        <p className="subtle">Имитация корпоративного входа (SSO): выбери, за кого смотреть прототип.</p>
        {accounts.isPending && <Skeleton lines={3} label="Загружаю учётки" />}
        {accounts.isError && <ErrorState text={errorText(accounts.error)} onRetry={() => void accounts.refetch()} />}
        {accounts.data && (
          <ul className="login__list">
            {accounts.data.items.map((a) => (
              <li key={a.user_id} className="login__item">
                <div>
                  <div className="row">
                    <strong>{a.display_name}</strong>
                    <Chip tone={a.role === 'mentor' ? 'info' : 'neutral'}>{ROLE[a.role]}</Chip>
                  </div>
                  <div className="subtle">{a.description}</div>
                </div>
                <Button
                  variant="primary"
                  loading={login.isPending && login.variables === a.user_id}
                  disabled={login.isPending}
                  onClick={() => login.mutate(a.user_id)}
                >
                  Войти
                </Button>
              </li>
            ))}
          </ul>
        )}
        {login.isError && (
          <p className="fieldset__caption--error" role="alert">
            {errorText(login.error)}
          </p>
        )}
      </section>
    </main>
  )
}
