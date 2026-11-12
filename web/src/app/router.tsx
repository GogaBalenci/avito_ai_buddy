import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect, type RouterHistory } from '@tanstack/react-router'
import { ApiError, setUnauthorizedHandler } from '@/shared/api/client'
import { meQuery } from '@/shared/api/queries'
import type { Me } from '@/shared/api/schemas'
import { Toaster } from '@/shared/ui/Toaster'
import { toast } from '@/shared/ui/toastStore'
import { ErrorState } from '@/shared/ui/ErrorState'
import { LoginPage } from '@/features/auth/LoginPage'
import { homeFor } from '@/features/auth/useAuth'
import { HomePage } from '@/features/home/HomePage'
import { RequestsPage } from '@/features/requests/requests'
import { KbPage } from '@/features/kb/KbPage'
import { MentorInternsPage, MentorInternPathPage } from '@/features/mentor/MentorPages'
import { InboxPage } from '@/features/mentor/InboxPage'
import { AppShell } from './AppShell'
import { resetClientState } from './session'

export type RouterContext = { queryClient: QueryClient }

const SESSION_EXPIRED = 'Сессия истекла — войди снова'

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <>
      <Outlet />
      <Toaster />
    </>
  ),
})

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: '/login', component: LoginPage })

/** Гард по ['me'] (12 §10.1): 401 → /login (с тостом, если сессия была). */
const authedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'authed',
  beforeLoad: async ({ context }) => {
    const hadSession = context.queryClient.getQueryData(['me']) !== undefined
    try {
      const me = await context.queryClient.ensureQueryData(meQuery)
      return { me }
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        if (hadSession) toast(SESSION_EXPIRED, { tone: 'error' })
        context.queryClient.removeQueries({ queryKey: ['me'] })
        throw redirect({ to: '/login' })
      }
      throw e
    }
  },
  component: AppShell,
  errorComponent: ({ error, reset }) => (
    <main className="page">
      <ErrorState text={error instanceof ApiError ? error.message : 'Не получилось открыть приложение'} onRetry={reset} />
    </main>
  ),
})

const requireRole = (role: Me['role']) => ({ context }: { context: { me: Me } }) => {
  if (context.me.role !== role) throw redirect({ to: homeFor(context.me) })
}

const indexRoute = createRoute({ getParentRoute: () => authedRoute, path: '/', beforeLoad: requireRole('intern'), component: HomePage })
const requestsRoute = createRoute({ getParentRoute: () => authedRoute, path: '/requests', beforeLoad: requireRole('intern'), component: RequestsPage })
const kbRoute = createRoute({ getParentRoute: () => authedRoute, path: '/kb/$slug', component: KbPage })
const mentorInternsRoute = createRoute({ getParentRoute: () => authedRoute, path: '/mentor/interns', beforeLoad: requireRole('mentor'), component: MentorInternsPage })
const mentorInternRoute = createRoute({ getParentRoute: () => authedRoute, path: '/mentor/interns/$userId', beforeLoad: requireRole('mentor'), component: MentorInternPathPage })
const mentorInboxRoute = createRoute({ getParentRoute: () => authedRoute, path: '/mentor/inbox', beforeLoad: requireRole('mentor'), component: InboxPage })

export const routeTree = rootRoute.addChildren([
  loginRoute,
  authedRoute.addChildren([indexRoute, requestsRoute, kbRoute, mentorInternsRoute, mentorInternRoute, mentorInboxRoute]),
])

export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  const router = createRouter({ routeTree, context: { queryClient }, history, defaultPreload: false })
  // Любой запрос получил 401 посреди работы → вход с тостом (09 §2.1)
  setUnauthorizedHandler(() => {
    if (router.state.location.pathname === '/login') return
    toast(SESSION_EXPIRED, { tone: 'error' })
    resetClientState(queryClient)
    queryClient.removeQueries({ queryKey: ['me'] })
    void router.navigate({ to: '/login' })
  })
  return router
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
