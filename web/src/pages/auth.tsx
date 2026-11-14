import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router'
import { api, safeNext } from '../api'
import { refreshSession, useMe } from '../session'
import { AuthShell, ErrorAlert, Field, Loading } from '../ui'

// Codes from GET /auth/oidc/callback (docs/API.md).
const ssoErrors: Record<string, string> = {
  not_invited: 'There is no account or open invite for that email. Ask a workspace admin to invite you.',
  account_disabled: 'This account has been deactivated.',
  sso_email_unverified: 'Your identity provider has not confirmed your email address.',
  sso_denied: 'Sign-in was cancelled at the identity provider.',
  sso_expired: 'Sign-in took too long or was interrupted. Try again.',
  sso_state: 'Sign-in took too long or was interrupted. Try again.',
  sso_failed: 'Single sign-on failed. Try again, or contact your administrator.',
}

export function ssoHref(next: string): string {
  return `/auth/oidc/login?next=${encodeURIComponent(next)}`
}

export function SsoButton({ next }: { next: string }) {
  const providers = useQuery({ queryKey: ['providers'], queryFn: api.providers, staleTime: Infinity })
  const oidc = providers.data?.oidc
  if (!oidc?.enabled) return null
  return (
    <>
      <div className="divider text-muted text-xs font-extrabold uppercase">or</div>
      {/* A full navigation, not a fetch: the identity provider's pages take over. */}
      <a className="btn btn-ol w-full" href={ssoHref(next)}>
        Sign in with {oidc.name}
      </a>
    </>
  )
}

export function LoginPage() {
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const me = useMe()
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setup })
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  const login = useMutation({
    mutationFn: () => api.login({ email, password }),
    onSuccess: async () => {
      await refreshSession(qc)
      navigate(next, { replace: true })
    },
  })

  if (me.data) return <Navigate to={next} replace />
  if (setup.data?.needsBootstrap) return <Navigate to="/setup" replace />
  if (me.isPending) return <Loading />

  const ssoError = params.get('error')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate()
  }
  return (
    <AuthShell kicker="Welcome back" title="Sign in">
      <ErrorAlert error={login.error ?? (ssoError ? (ssoErrors[ssoError] ?? ssoErrors.sso_failed) : null)} />
      <form onSubmit={submit}>
        <Field label="Email">
          <input className="input w-full" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Password">
          <input className="input w-full" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <button className="btn btn-primary mt-2 w-full" disabled={login.isPending}>
          {login.isPending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <SsoButton next={next} />
      <p className="text-muted mt-8 text-sm font-medium">New here? Accounts are by invitation: ask a workspace admin to invite you.</p>
    </AuthShell>
  )
}

export function SetupPage() {
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setup })
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [form, setForm] = useState({ email: '', displayName: '', password: '' })

  const bootstrap = useMutation({
    mutationFn: () => api.bootstrap(form),
    onSuccess: async () => {
      qc.setQueryData(['setup'], { needsBootstrap: false })
      await refreshSession(qc)
      navigate('/', { replace: true })
    },
  })

  if (setup.isPending) return <Loading />
  if (setup.data && !setup.data.needsBootstrap && !bootstrap.isSuccess) return <Navigate to="/login" replace />

  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  return (
    <AuthShell kicker="First run" title="Create the owner account">
      <p className="text-muted mb-6 font-medium">
        This instance has no users yet. The account you create here can create workspaces and invite everyone else.
      </p>
      <ErrorAlert error={bootstrap.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          bootstrap.mutate()
        }}
      >
        <Field label="Email">
          <input className="input w-full" type="email" autoComplete="username" required value={form.email} onChange={set('email')} />
        </Field>
        <Field label="Your name">
          <input className="input w-full" autoComplete="name" required maxLength={100} value={form.displayName} onChange={set('displayName')} />
        </Field>
        <Field label="Password" hint="At least 12 characters.">
          <input className="input w-full" type="password" autoComplete="new-password" required minLength={12} maxLength={256} value={form.password} onChange={set('password')} />
        </Field>
        <button className="btn btn-primary mt-2 w-full" disabled={bootstrap.isPending}>
          Create account
        </button>
      </form>
    </AuthShell>
  )
}

export function NotFoundPage() {
  return (
    <AuthShell kicker="404" title="Page not found">
      <Link className="btn btn-primary" to="/">
        Go to your workspaces
      </Link>
    </AuthShell>
  )
}
