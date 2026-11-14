import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router'
import { api, ApiError, type InvitePreview, type Workspace } from '../api'
import { refreshSession, useMe } from '../session'
import { AuthShell, ErrorAlert, Field, Loading } from '../ui'
import { SsoButton } from './auth'

export function InvitePage() {
  const { token = '' } = useParams()
  const preview = useQuery({ queryKey: ['invite', token], queryFn: () => api.invitePreview(token), retry: false })
  const me = useMe()

  if (preview.isPending || me.isPending) return <Loading />
  if (preview.isError) {
    const gone = preview.error instanceof ApiError && preview.error.status === 410
    return (
      <AuthShell kicker="Invite" title={gone ? 'Invite no longer valid' : 'Invite not found'}>
        <p className="text-muted mb-6 font-medium">
          {gone
            ? 'This invite has already been used or has expired. Ask the person who invited you for a new one.'
            : 'Check that you copied the whole link from the email.'}
        </p>
        <Link className="btn btn-primary" to="/">
          Go to oh-my-story
        </Link>
      </AuthShell>
    )
  }

  const inv = preview.data
  return (
    <AuthShell kicker="You're invited" title={`Join ${inv.workspaceName}`}>
      <ul className="rows mb-8">
        <li>
          <span className="label-up w-20">Email</span>
          <span className="font-bold break-all">{inv.email}</span>
        </li>
        <li>
          <span className="label-up w-20">Role</span>
          <span className="font-bold capitalize">{inv.role}</span>
        </li>
      </ul>
      {me.data ? <AcceptAsUser token={token} inv={inv} email={me.data.user.email} /> : <AcceptAsNew token={token} inv={inv} />}
    </AuthShell>
  )
}

function useJoined() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return async (ws: Workspace) => {
    await refreshSession(qc)
    navigate(`/w/${ws.slug}/members`, { replace: true })
  }
}

function AcceptAsUser({ token, inv, email }: { token: string; inv: InvitePreview; email: string }) {
  const joined = useJoined()
  const qc = useQueryClient()
  const accept = useMutation({ mutationFn: () => api.acceptInvite(token), onSuccess: (r) => joined(r.workspace) })

  if (email.toLowerCase() !== inv.email.toLowerCase()) {
    return (
      <>
        <ErrorAlert error={`You're signed in as ${email}, but this invite is for ${inv.email}.`} />
        <button
          className="btn btn-ol w-full"
          onClick={async () => {
            await api.logout()
            await refreshSession(qc)
          }}
        >
          Sign out and continue
        </button>
      </>
    )
  }
  return (
    <>
      <ErrorAlert error={accept.error} />
      <button className="btn btn-primary w-full" disabled={accept.isPending} onClick={() => accept.mutate()}>
        Accept invite
      </button>
    </>
  )
}

function AcceptAsNew({ token, inv }: { token: string; inv: InvitePreview }) {
  const joined = useJoined()
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const accept = useMutation({
    mutationFn: () => api.acceptInvite(token, { displayName, password }),
    onSuccess: (r) => joined(r.workspace),
  })
  const here = `/invites/${token}`
  return (
    <>
      <h2 className="up mb-4 text-lg">Create your account</h2>
      <ErrorAlert error={accept.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          accept.mutate()
        }}
      >
        <input type="email" hidden readOnly autoComplete="username" value={inv.email} />
        <Field label="Your name">
          <input className="input w-full" autoComplete="name" required maxLength={100} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
        <Field label="Password" hint="At least 12 characters.">
          <input className="input w-full" type="password" autoComplete="new-password" required minLength={12} maxLength={256} value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <button className="btn btn-primary mt-2 w-full" disabled={accept.isPending}>
          Create account and join
        </button>
      </form>
      <SsoButton next={here} />
      <p className="text-muted mt-8 text-sm font-medium">
        Already have an account?{' '}
        <Link className="link font-bold" to={`/login?next=${encodeURIComponent(here)}`}>
          Sign in to accept
        </Link>
        .
      </p>
    </>
  )
}
