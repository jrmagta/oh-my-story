import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router'
import { api, ApiError } from '../api'
import { refreshSession } from '../session'
import { AuthShell, ErrorAlert, Field, Loading, Notice } from '../ui'

/** Asks for a reset link. The answer never says whether the account exists. */
export function ForgotPage() {
  const [email, setEmail] = useState('')
  const send = useMutation({ mutationFn: () => api.requestReset({ email }) })
  return (
    <AuthShell kicker="Password" title="Reset your password">
      {send.isSuccess ? (
        <>
          <Notice>Check your inbox.</Notice>
          <p className="text-muted mb-6 font-medium">
            If an account with a password uses {email}, we've sent it a link to choose a new one. The link works once, for one
            hour. Accounts that sign in with single sign-on have no password here to reset.
          </p>
          <Link className="btn btn-ol" to="/login">
            Back to sign in
          </Link>
        </>
      ) : (
        <>
          <p className="text-muted mb-6 font-medium">Enter your email and we'll send you a link to choose a new password.</p>
          <ErrorAlert error={send.error} />
          <form
            onSubmit={(e) => {
              e.preventDefault()
              send.mutate()
            }}
          >
            <Field label="Email">
              <input className="input w-full" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <button className="btn btn-primary mt-2 w-full" disabled={send.isPending}>
              Send reset link
            </button>
          </form>
          <p className="mt-6 text-sm font-semibold">
            <Link className="underline" to="/login">
              Back to sign in
            </Link>
          </p>
        </>
      )}
    </AuthShell>
  )
}

/** Opened from the email: choose a new password, and be signed in. */
export function ResetPage() {
  const { token = '' } = useParams()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const preview = useQuery({ queryKey: ['reset', token], queryFn: () => api.resetPreview(token), retry: false })
  const [password, setPassword] = useState('')
  const reset = useMutation({
    mutationFn: () => api.resetPassword(token, { password }),
    onSuccess: async () => {
      await refreshSession(qc)
      navigate('/', { replace: true })
    },
  })

  if (preview.isPending) return <Loading />
  if (preview.isError) {
    const gone = preview.error instanceof ApiError && preview.error.status === 410
    return (
      <AuthShell kicker="Password" title="Link no longer valid">
        <p className="text-muted mb-6 font-medium">
          {gone ? 'This reset link was used, replaced by a newer one, or is over an hour old.' : 'Something went wrong. Try again.'}
        </p>
        <Link className="btn btn-primary" to="/forgot">
          Send a new link
        </Link>
      </AuthShell>
    )
  }
  return (
    <AuthShell kicker="Password" title="Choose a new password">
      <p className="text-muted mb-6 font-medium">
        For <b className="text-base-content break-all">{preview.data.email}</b>. Saving it signs you out everywhere else.
      </p>
      <ErrorAlert error={reset.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          reset.mutate()
        }}
      >
        {/* Lets password managers file the new password under the right account. */}
        <input type="email" autoComplete="username" value={preview.data.email} readOnly hidden />
        <Field label="New password" hint="At least 12 characters.">
          <input
            className="input w-full"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={256}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <button className="btn btn-primary mt-2 w-full" disabled={reset.isPending}>
          Save and sign in
        </button>
      </form>
    </AuthShell>
  )
}
