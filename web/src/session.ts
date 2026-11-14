import { useQuery, type QueryClient } from '@tanstack/react-query'
import { api, type Me } from './api'

export const meKey = ['me'] as const

export function useMe() {
  return useQuery({ queryKey: meKey, queryFn: api.me })
}

/** The signed-in user. Only call below <RequireAuth>, which guarantees a session. */
export function useSession(): Me {
  const { data } = useMe()
  if (!data) throw new Error('useSession outside RequireAuth')
  return data
}

/**
 * Reload the session after it changes (sign-in, sign-out, joining or leaving a
 * workspace) and wait for it, so the next screen never sees the old one.
 */
export async function refreshSession(qc: QueryClient): Promise<Me | null> {
  return qc.fetchQuery({ queryKey: meKey, queryFn: api.me, staleTime: 0 })
}
