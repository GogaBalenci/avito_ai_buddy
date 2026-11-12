import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api } from '@/shared/api/client'
import { MeSchema, type Me } from '@/shared/api/schemas'
import { resetClientState } from '@/app/session'

export function homeFor(me: Pick<Me, 'role'>): '/' | '/mentor/interns' {
  return me.role === 'mentor' ? '/mentor/interns' : '/'
}

export function useLogin() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: (userId: string) => api('/api/auth/login', { body: { user_id: userId }, schema: MeSchema, silent401: true }),
    onSuccess: (me) => {
      resetClientState(qc)
      qc.setQueryData(['me'], me)
      void navigate({ to: homeFor(me) })
    },
  })
}

export function useLogout() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: () => api('/api/auth/logout', { method: 'POST', silent401: true }).catch(() => undefined),
    onSettled: () => {
      resetClientState(qc)
      qc.removeQueries({ queryKey: ['me'] })
      void navigate({ to: '/login' })
    },
  })
}
