import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useOutletContext } from 'react-router'
import { api, atLeast, roles, type Invite, type Member, type Membership, type Role } from '../api'
import { refreshSession, useSession } from '../session'
import { Avatar, ConfirmButton, ErrorAlert, Field, Loading, Notice } from '../ui'
import { PageBand } from './app'

const invitable: readonly Role[] = roles.filter((r) => r !== 'owner')

export function MembersPage() {
  const ws = useOutletContext<Membership>()
  const members = useQuery({ queryKey: ['ws', ws.slug, 'members'], queryFn: () => api.members(ws.slug) })
  const canManage = atLeast(ws.role, 'admin')
  const count = members.data?.length

  return (
    <>
      <PageBand kicker={`${ws.name} › Members`} title="Members">
        {count !== undefined && (
          <p className="label-up pb-1">
            {count} {count === 1 ? 'member' : 'members'} · you are {ws.role}
          </p>
        )}
      </PageBand>
      <main className="mx-auto grid max-w-5xl gap-12 px-7 py-10 lg:grid-cols-[1fr_340px]">
        <section aria-labelledby="members-h">
          <h2 id="members-h" className="label-up mb-3">
            People
          </h2>
          {members.isPending ? (
            <Loading />
          ) : members.isError ? (
            <ErrorAlert error={members.error} />
          ) : (
            <MemberList ws={ws} members={members.data} />
          )}
          {canManage && <OpenInvites ws={ws} />}
        </section>
        {canManage && <InviteForm ws={ws} />}
      </main>
    </>
  )
}

function MemberList({ ws, members }: { ws: Membership; members: Member[] }) {
  const [error, setError] = useState<unknown>(null)
  return (
    <>
      <ErrorAlert error={error} />
      <ul className="rows" aria-label="Members">
        {members.map((m) => (
          <MemberRow key={m.userId} ws={ws} m={m} onError={setError} />
        ))}
      </ul>
    </>
  )
}

function MemberRow({ ws, m, onError }: { ws: Membership; m: Member; onError: (e: unknown) => void }) {
  const { user } = useSession()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const self = m.userId === user.id
  // Mirrors the server's rules (docs/API.md); the server enforces them either way.
  const canTouch = atLeast(ws.role, 'admin') && (ws.role === 'owner' || m.role !== 'owner')
  const options = ws.role === 'owner' ? roles : invitable

  const refresh = async () => {
    await qc.invalidateQueries({ queryKey: ['ws', ws.slug] })
    if (self) await refreshSession(qc)
  }
  const setRole = useMutation({
    mutationFn: (r: Role) => api.setRole(ws.slug, m.userId, r),
    onMutate: () => onError(null),
    onSuccess: refresh,
    onError,
  })
  const remove = useMutation({
    mutationFn: () => api.removeMember(ws.slug, m.userId),
    onMutate: () => onError(null),
    onSuccess: async () => {
      if (self) {
        await refreshSession(qc)
        navigate('/', { replace: true })
      } else {
        await refresh()
      }
    },
    onError,
  })

  return (
    <li>
      <Avatar id={m.userId} name={m.displayName} size={38} />
      <div className="min-w-0 flex-1">
        <p className="truncate font-extrabold">
          {m.displayName}
          {self && <span className="text-muted font-semibold"> (you)</span>}
        </p>
        <p className="text-muted truncate text-sm font-medium">{m.email}</p>
      </div>
      {canTouch ? (
        <select
          className="select select-sm w-36 font-bold capitalize"
          aria-label={`Role for ${m.displayName}`}
          value={m.role}
          disabled={setRole.isPending}
          onChange={(e) => setRole.mutate(e.target.value as Role)}
        >
          {options.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
      ) : (
        <span className="label-up w-36 text-right">{m.role}</span>
      )}
      <div className="w-32 text-right">
        {(self || canTouch) && (
          <ConfirmButton
            label={self ? 'Leave' : 'Remove'}
            confirm={self ? 'Confirm leave' : 'Confirm remove'}
            name={m.displayName}
            busy={remove.isPending}
            onConfirm={() => remove.mutate()}
          />
        )}
      </div>
    </li>
  )
}

function OpenInvites({ ws }: { ws: Membership }) {
  const invites = useQuery({ queryKey: ['ws', ws.slug, 'invites'], queryFn: () => api.invites(ws.slug) })
  if (!invites.data?.length) return null
  return (
    <>
      <h2 className="label-up mt-12 mb-3">Open invites</h2>
      <ul className="rows" aria-label="Open invites">
        {invites.data.map((inv) => (
          <InviteRow key={inv.id} ws={ws} inv={inv} />
        ))}
      </ul>
    </>
  )
}

const expiry = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' })

function InviteRow({ ws, inv }: { ws: Membership; inv: Invite }) {
  const qc = useQueryClient()
  const revoke = useMutation({
    mutationFn: () => api.revokeInvite(ws.slug, inv.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ws', ws.slug, 'invites'] }),
  })
  return (
    <li>
      <div className="min-w-0 flex-1">
        <p className="truncate font-extrabold">{inv.email}</p>
        <p className="text-muted text-sm font-medium">Expires {expiry.format(new Date(inv.expiresAt))}</p>
      </div>
      <span className="label-up w-36 text-right">{inv.role}</span>
      <div className="w-32 text-right">
        <button type="button" className="btn btn-sm btn-ol" aria-label={`Revoke invite for ${inv.email}`} disabled={revoke.isPending} onClick={() => revoke.mutate()}>
          Revoke
        </button>
      </div>
    </li>
  )
}

function InviteForm({ ws }: { ws: Membership }) {
  const qc = useQueryClient()
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('editor')
  const [sentTo, setSentTo] = useState<string | null>(null)
  const invite = useMutation({
    mutationFn: () => api.invite(ws.slug, { email, role }),
    onMutate: () => setSentTo(null),
    onSuccess: async (inv) => {
      setSentTo(inv.email)
      setEmail('')
      await qc.invalidateQueries({ queryKey: ['ws', ws.slug, 'invites'] })
    },
  })
  return (
    <section aria-labelledby="invite-h" className="bg-base-200 border-line self-start border p-6">
      <h2 id="invite-h" className="up mb-5 text-xl">
        Invite someone
      </h2>
      {sentTo && <Notice>Invite sent to {sentTo}.</Notice>}
      <ErrorAlert error={invite.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          invite.mutate()
        }}
      >
        <Field label="Email">
          <input className="input w-full" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Role">
          <select className="select w-full font-bold capitalize" value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {invitable.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </Field>
        <button className="btn btn-primary mt-2 w-full" disabled={invite.isPending}>
          {invite.isPending ? 'Sending…' : 'Send invite'}
        </button>
      </form>
      <p className="text-muted mt-4 text-sm font-medium">They get an email with a link that works once and expires in 7 days.</p>
    </section>
  )
}
