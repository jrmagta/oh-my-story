import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Membership } from '../api'
import { ErrorAlert } from '../ui'
import { ago } from '../map/history'

const expiries: [number | null, string][] = [
  [90, '90 days'],
  [30, '30 days'],
  [365, '1 year'],
  [null, 'Never'],
]

/**
 * Admins make tokens for scripts and other tools to call the API as this
 * workspace: read (like a viewer) or write (like an editor). A token is shown
 * once; the list shows its prefix, its last use and when it expires.
 */
export function ApiTokens({ ws }: { ws: Membership }) {
  const qc = useQueryClient()
  const key = ['ws', ws.slug, 'api-tokens']
  const tokens = useQuery({ queryKey: key, queryFn: () => api.apiTokens(ws.slug) })
  const [name, setName] = useState('')
  const [scope, setScope] = useState<'read' | 'write'>('read')
  const [days, setDays] = useState<number | null>(90)
  const [made, setMade] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const create = useMutation({
    mutationFn: () => api.createApiToken(ws.slug, { name: name.trim(), scope, expiresInDays: days }),
    onSuccess: (t) => {
      setMade(t.token)
      setCopied(false)
      setName('')
      void qc.invalidateQueries({ queryKey: key })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeApiToken(ws.slug, id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  })
  return (
    <section className="mt-12" aria-labelledby="api-tokens">
      <h2 id="api-tokens" className="up text-xl">
        API tokens
      </h2>
      <p className="text-muted mt-1 mb-4 text-sm font-medium">
        Scripts and other tools use a token as <code>Authorization: Bearer oms_…</code> on this workspace's API. Read tokens act
        like a viewer, write tokens like an editor; history names the token. The{' '}
        <a className="underline" href="/docs/api" target="_blank" rel="noreferrer">
          API documentation
        </a>{' '}
        lists every endpoint and can try them out (the{' '}
        <a className="underline" href="/api/openapi.yaml" target="_blank" rel="noreferrer">
          OpenAPI file
        </a>{' '}
        is there for tools).
      </p>
      <ErrorAlert error={create.error ?? revoke.error} />
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <label className="min-w-48 flex-1">
          <span className="label-up mb-1 block">Name</span>
          <input
            className="input input-sm w-full"
            required
            maxLength={100}
            placeholder="CI bot"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label>
          <span className="label-up mb-1 block">Access</span>
          <select className="select select-sm" value={scope} onChange={(e) => setScope(e.target.value as 'read' | 'write')}>
            <option value="read">Read</option>
            <option value="write">Read and write</option>
          </select>
        </label>
        <label>
          <span className="label-up mb-1 block">Expires</span>
          <select className="select select-sm" value={String(days)} onChange={(e) => setDays(e.target.value === 'null' ? null : Number(e.target.value))}>
            {expiries.map(([d, label]) => (
              <option key={label} value={String(d)}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <button className="btn btn-sm btn-primary" disabled={create.isPending || !name.trim()}>
          Make a token
        </button>
      </form>
      {made && (
        <div className="share-made mt-4" role="status">
          <input className="input input-sm w-full font-mono" readOnly aria-label="New token" value={made} onFocus={(e) => e.target.select()} />
          <button
            type="button"
            className="btn btn-sm btn-ol"
            onClick={() => {
              void navigator.clipboard?.writeText(made).then(() => setCopied(true))
            }}
          >
            {copied ? 'Copied' : 'Copy'}
          </button>
          <p className="text-muted col-span-2 text-[13px] font-medium">Copy it now: it is shown only once. A lost token can be revoked and made again.</p>
        </div>
      )}
      {tokens.data?.length === 0 && <p className="text-muted mt-4 text-sm font-medium">No tokens yet.</p>}
      <ul className="share-links mt-4" aria-label="API tokens">
        {tokens.data?.map((t) => {
          const expired = t.expiresAt !== null && new Date(t.expiresAt) < new Date()
          return (
            <li key={t.id}>
              <span className="flex-1 text-sm font-semibold">
                {t.name} <code className="text-muted text-[12px]">{t.prefix}…</code>
                <span className="text-muted block text-[12px]">
                  {t.scope === 'write' ? 'Read and write' : 'Read'} · by {t.creatorName || 'a former member'}, {ago(t.createdAt)} ·{' '}
                  {t.expiresAt ? (expired ? 'expired' : `expires ${new Date(t.expiresAt).toLocaleDateString()}`) : 'never expires'} ·{' '}
                  {t.lastUsedAt ? `used ${ago(t.lastUsedAt)}` : 'not used yet'}
                </span>
              </span>
              <button type="button" className="btn btn-xs btn-ol" aria-label={`Revoke token ${t.name}`} onClick={() => revoke.mutate(t.id)}>
                Revoke
              </button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
