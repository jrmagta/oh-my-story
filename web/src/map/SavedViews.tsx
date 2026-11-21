import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, atLeast, type SavedView } from '../api'
import { mapKey, useMap } from '../pages/map'
import { useSession } from '../session'
import { errorMessage } from '../ui'
import { isActive, writeFilter, type Filter } from './filter'

/** Two filters are the same view when they write the same URL query. */
const sameFilter = (a: Filter, b: Filter) => writeFilter(new URLSearchParams(), a).toString() === writeFilter(new URLSearchParams(), b).toString()

/**
 * The map's saved views: shared ones and the caller's own. Picking one applies
 * its filter. While a filter is on, it can be saved under a name, and editors
 * can share it with the workspace.
 */
export function SavedViews() {
  const { ws, view, filter, setFilter } = useMap()
  const { user } = useSession()
  const qc = useQueryClient()
  const key = [...mapKey(ws.slug, view.map.id), 'views']
  const views = useQuery({ queryKey: key, queryFn: () => api.views(ws.slug, view.map.id) })
  const write = useMutation({
    mutationFn: (w: () => Promise<unknown>) => w(),
    onSettled: () => qc.invalidateQueries({ queryKey: key }),
  })
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [share, setShare] = useState(false)
  const canShare = atLeast(ws.role, 'editor')
  const isAdmin = atLeast(ws.role, 'admin')
  const current = views.data?.find((v) => sameFilter(v.filter, filter))
  const shared = views.data?.filter((v) => v.shared) ?? []
  const mine = views.data?.filter((v) => !v.shared && v.createdBy === user.id) ?? []

  const save = () => {
    const n = name.trim()
    if (!n) return
    write.mutate(() => api.createView(ws.slug, view.map.id, { name: n, filter, shared: share }), {
      onSuccess: () => {
        setName('')
        setShare(false)
        setOpen(false)
      },
    })
  }

  const row = (v: SavedView) => (
    <li key={v.id}>
      <button
        type="button"
        className="view-pick"
        aria-current={current?.id === v.id}
        onClick={() => {
          setFilter(v.filter)
          setOpen(false)
        }}
      >
        {v.name}
        {v.shared && v.createdBy !== user.id && <span className="text-muted text-xs font-semibold"> · {v.creatorName}</span>}
      </button>
      {v.createdBy === user.id && canShare && (
        <button
          type="button"
          className="btn btn-xs btn-ol"
          aria-label={`${v.shared ? 'Stop sharing' : 'Share'} view ${v.name}`}
          onClick={() => write.mutate(() => api.patchView(ws.slug, view.map.id, v.id, { shared: !v.shared }))}
        >
          {v.shared ? 'Unshare' : 'Share'}
        </button>
      )}
      {(v.createdBy === user.id || (v.shared && isAdmin)) && (
        <button
          type="button"
          className="icon-btn"
          aria-label={`Delete view ${v.name}`}
          onClick={() => write.mutate(() => api.deleteView(ws.slug, view.map.id, v.id))}
        >
          <span>✕</span>
        </button>
      )}
    </li>
  )

  return (
    <span
      className="relative"
      onKeyDown={(e) => {
        if (e.key === 'Escape') setOpen(false)
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false)
      }}
    >
      <button type="button" className="btn btn-sm btn-ol" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        {current ? `View: ${current.name}` : 'Views'}
      </button>
      {open && (
        <div className="views-pop" role="group" aria-label="Saved views">
          {write.error != null && <p className="text-error text-sm font-bold">{errorMessage(write.error)}</p>}
          {shared.length > 0 && (
            <>
              <p className="label-up">Shared</p>
              <ul aria-label="Shared views">{shared.map(row)}</ul>
            </>
          )}
          {mine.length > 0 && (
            <>
              <p className="label-up">Yours</p>
              <ul aria-label="Your views">{mine.map(row)}</ul>
            </>
          )}
          {views.data?.length === 0 && <p className="text-muted text-sm font-medium">No saved views yet.</p>}
          {isActive(filter) && !current ? (
            <form
              className="views-save"
              onSubmit={(e) => {
                e.preventDefault()
                save()
              }}
            >
              <input
                className="input input-sm w-full"
                aria-label="Name for this view"
                placeholder="Save this filter as…"
                maxLength={100}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
              {canShare && (
                <label className="flex items-center gap-2 text-sm font-bold">
                  <input type="checkbox" className="filter-check" checked={share} onChange={(e) => setShare(e.target.checked)} />
                  Share with the workspace
                </label>
              )}
              <button className="btn btn-xs btn-primary" disabled={!name.trim() || write.isPending}>
                Save view
              </button>
            </form>
          ) : (
            !isActive(filter) && <p className="text-muted text-sm font-medium">Set a filter to save it as a view.</p>
          )}
        </div>
      )}
    </span>
  )
}
