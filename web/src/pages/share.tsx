import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from 'react-router'
import { api, ApiError, type Membership } from '../api'
import { MapDnd } from '../map/dnd'
import { FilterBar } from '../map/FilterBar'
import { useViewPrefs } from '../map/viewPrefs'
import { ago } from '../map/history'
import { ErrorAlert, Loading, Logo, Stripe, ThemeToggle } from '../ui'
import { deriveMap, mapKey, MapGrid, MapHeader, MapProvider, useMap, useMapSearch, type MapCtx } from './map'
import { StepPanel } from './step-panel'
import { Roadmap } from './roadmap'

const expiries: [number | null, string][] = [
  [7, '7 days'],
  [30, '30 days'],
  [90, '90 days'],
  [null, 'Never'],
]

/** The map header's Share button: editors make, copy and revoke read-only links. */
export function ShareButton() {
  const { canEdit } = useMap()
  const [open, setOpen] = useState(false)
  if (!canEdit) return null
  return (
    <>
      <button type="button" className="btn btn-xs btn-ol" onClick={() => setOpen(true)}>
        Share
      </button>
      {open && <ShareDialog close={() => setOpen(false)} />}
    </>
  )
}

function ShareDialog({ close }: { close: () => void }) {
  const { ws, view } = useMap()
  const qc = useQueryClient()
  const key = [...mapKey(ws.slug, view.map.id), 'share-links']
  const links = useQuery({ queryKey: key, queryFn: () => api.shareLinks(ws.slug, view.map.id) })
  const [days, setDays] = useState<number | null>(30)
  const [made, setMade] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const create = useMutation({
    mutationFn: () => api.createShareLink(ws.slug, view.map.id, { expiresInDays: days }),
    onSuccess: (l) => {
      setMade(l.url)
      setCopied(false)
    },
    onSettled: () => qc.invalidateQueries({ queryKey: key }),
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeShareLink(ws.slug, view.map.id, id),
    onSettled: () => qc.invalidateQueries({ queryKey: key }),
  })
  return (
    <div className="palette-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div
        className="palette share-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="Share this map"
        onKeyDown={(e) => {
          if (e.key === 'Escape') close()
        }}
      >
        <div className="flex items-center gap-3">
          <h2 className="up flex-1 text-xl">Share read-only</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={close}>
            <span>✕</span>
          </button>
        </div>
        <p className="text-muted text-sm font-medium">
          Anyone with a link sees this map without signing in: steps, their details, labels and personas. They do not see
          comments, history, or who is assigned.
        </p>
        <ErrorAlert error={create.error ?? revoke.error} />
        <div className="flex items-end gap-3">
          <label className="flex-1">
            <span className="label-up mb-1 block">Link expires</span>
            <select className="select select-sm w-full" value={String(days)} onChange={(e) => setDays(e.target.value === 'null' ? null : Number(e.target.value))}>
              {expiries.map(([d, name]) => (
                <option key={name} value={String(d)}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="btn btn-sm btn-primary" autoFocus disabled={create.isPending} onClick={() => create.mutate()}>
            Make a link
          </button>
        </div>
        {made && (
          <div className="share-made" role="status">
            <input className="input input-sm w-full" readOnly aria-label="New link" value={made} onFocus={(e) => e.target.select()} />
            <button
              type="button"
              className="btn btn-sm btn-ol"
              onClick={() => {
                void navigator.clipboard?.writeText(made).then(() => setCopied(true))
              }}
            >
              {copied ? 'Copied' : 'Copy'}
            </button>
            <p className="text-muted col-span-2 text-[13px] font-medium">Copy it now: it is shown only once. Lost links can be revoked and made again.</p>
          </div>
        )}
        <h3 className="label-up mt-2">Links</h3>
        {links.data?.length === 0 && <p className="text-muted text-sm font-medium">No links yet.</p>}
        <ul className="share-links" aria-label="Share links">
          {links.data?.map((l) => {
            const expired = l.expiresAt !== null && new Date(l.expiresAt) < new Date()
            return (
              <li key={l.id}>
                <span className="flex-1 text-sm font-semibold">
                  By {l.creatorName || 'a former member'}, {ago(l.createdAt)}
                  <span className="text-muted block text-[12px]">
                    {l.expiresAt ? (expired ? 'Expired' : `Expires ${new Date(l.expiresAt).toLocaleDateString()}`) : 'Never expires'}
                    {' · '}
                    {l.lastUsedAt ? `opened ${ago(l.lastUsedAt)}` : 'not opened yet'}
                  </span>
                </span>
                <button type="button" className="btn btn-xs btn-ol" aria-label={`Revoke link made ${ago(l.createdAt)} by ${l.creatorName}`} onClick={() => revoke.mutate(l.id)}>
                  Revoke
                </button>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}

/** /s/:token, the map read-only for anyone with the link. No sign-in, no live updates. */
export function SharedMapPage() {
  const { token = '' } = useParams()
  const shared = useQuery({ queryKey: ['shared', token], queryFn: () => api.sharedMap(token), retry: false })
  const { selectedStep, openStep, filter, setFilter, mode, setMode } = useMapSearch()
  const [prefs, setPrefs] = useViewPrefs(shared.data?.view.map.id ?? 'shared')
  const ctx = useMemo<MapCtx | null>(() => {
    const d = shared.data
    if (!d) return null
    const ws: Membership = { id: '', name: d.workspaceName, slug: '', keyPrefix: '', estimateUnit: d.estimateUnit, role: 'viewer' }
    return {
      ws,
      shared: true,
      mode,
      setMode,
      view: d.view,
      canEdit: false,
      ...deriveMap(d.view, filter),
      filter,
      setFilter,
      prefs,
      setPrefs,
      run: () => Promise.resolve(false),
      selectedStep,
      openStep,
      move: () => {},
      adding: null,
      setAdding: () => {},
    }
  }, [shared.data, filter, setFilter, prefs, setPrefs, mode, setMode, selectedStep, openStep])

  return (
    <>
      <header className="topnav">
        <Logo />
        <div className="flex-1" />
        <ThemeToggle />
      </header>
      <Stripe />
      {shared.isPending ? (
        <Loading />
      ) : !ctx ? (
        <main className="mx-auto max-w-xl px-7 py-16">
          {shared.error instanceof ApiError && shared.error.status === 404 ? (
            <>
              <h1 className="up mb-3 text-3xl">Link not available</h1>
              <p className="text-muted font-medium">This link was revoked or has expired. Ask whoever sent it for a new one.</p>
            </>
          ) : (
            <ErrorAlert error={shared.error} />
          )}
        </main>
      ) : (
        <MapProvider value={ctx}>
          <MapHeader panelOpen={!!ctx.selectedStep} onRefresh={() => void shared.refetch()} />
          <FilterBar panelOpen={!!ctx.selectedStep} />
          <div className={`map-scroll ${ctx.selectedStep ? 'with-panel' : ''}`}>
            <MapDnd view={ctx.view} onMove={() => {}}>
              {ctx.mode === 'roadmap' ? <Roadmap /> : <MapGrid />}
            </MapDnd>
          </div>
          {ctx.selectedStep && ctx.view.steps.some((s) => s.id === ctx.selectedStep) && <StepPanel stepId={ctx.selectedStep} />}
        </MapProvider>
      )}
    </>
  )
}
