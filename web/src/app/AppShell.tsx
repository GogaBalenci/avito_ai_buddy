import { useQuery } from '@tanstack/react-query'
import { Link, Outlet } from '@tanstack/react-router'
import { configQuery, meQuery } from '@/shared/api/queries'
import { Button } from '@/shared/ui/Button'
import { Chip } from '@/shared/ui/Chip'
import { Vitya } from '@/shared/mascot/Vitya'
import { useLogout } from '@/features/auth/useAuth'
import { DemoPanel } from '@/features/demo/DemoPanel'
import { InboxBadge } from '@/features/mentor/InboxBadge'
import { ModeIndicator } from './ModeIndicator'

export function AppShell() {
  const me = useQuery(meQuery)
  const config = useQuery(configQuery)
  const logout = useLogout()
  const mentor = me.data?.role === 'mentor'
  return (
    <div className="shell">
      <header className="header">
        <Link to="/" className="brand" aria-label="Авиитик Витя — на главную">
          <Vitya state="hello" size={36} />
          <span>
            <strong>Авиитик Витя</strong>
            <span className="subtle brand__sub">AI-buddy для стажёров</span>
          </span>
        </Link>
        <nav className="nav" aria-label="Разделы">
          {mentor ? (
            <>
              <Link to="/mentor/interns" activeProps={{ className: 'nav__link--active' }} className="nav__link">
                Мои стажёры
              </Link>
              <Link to="/mentor/inbox" activeProps={{ className: 'nav__link--active' }} className="nav__link">
                Входящие <InboxBadge />
              </Link>
            </>
          ) : (
            <>
              <Link to="/" activeOptions={{ exact: true }} activeProps={{ className: 'nav__link--active' }} className="nav__link">
                Мой путь и чат
              </Link>
              <Link to="/requests" activeProps={{ className: 'nav__link--active' }} className="nav__link">
                Мои заявки
              </Link>
            </>
          )}
        </nav>
        <div className="header__right">
          <ModeIndicator />
          {config.data?.demo_tools_enabled && <DemoPanel />}
          {me.data && (
            <span className="user">
              {me.data.display_name}
              <Chip tone={mentor ? 'info' : 'neutral'}>{mentor ? 'Наставник' : 'Стажёр'}</Chip>
            </span>
          )}
          <Button variant="link" onClick={() => logout.mutate()}>
            Выйти
          </Button>
        </div>
      </header>
      <div className="shell__body">
        <Outlet />
      </div>
    </div>
  )
}
