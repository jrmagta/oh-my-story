import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { api, type Step } from '../api'
import { useMap } from '../pages/map'

/**
 * The maps a step is on. Editors put it on another map of the same workflow
 * (picking where it sits there) or take it off this one while it stays on others.
 */
export function LinkedMaps({ step }: { step: Step }) {
  const { ws, view, canEdit, run, openStep, shared } = useMap()
  const [adding, setAdding] = useState(false)
  const others = view.elsewhere.filter((e) => e.stepId === step.id)
  if (shared) return null
  const placement = view.placements.find((p) => p.stepId === step.id)
  return (
    <section className="border-line border-b px-5 py-4" aria-label="Maps">
      <div className="mb-2 flex items-center gap-2">
        <h3 className="label-up flex-1">On maps</h3>
        {canEdit && !adding && (
          <button type="button" className="btn btn-xs btn-ol" onClick={() => setAdding(true)}>
            + Add to a map
          </button>
        )}
      </div>
      <ul className="dep-list">
        <li>
          <span className="flex-1 font-semibold">{view.map.name}</span>
          {canEdit && others.length > 0 && placement && (
            <button
              type="button"
              className="btn btn-xs btn-ol"
              onClick={() => {
                openStep(null)
                void run(() => api.unplaceStep(ws.slug, view.map.id, step.id), {
                  done: `Took ${step.key} off this map`,
                  undo: () =>
                    api.placeStep(ws.slug, view.map.id, { stepId: step.id, activityId: placement.activityId, releaseId: placement.releaseId }),
                })
              }}
            >
              Remove from this map
            </button>
          )}
        </li>
        {others.map((e) => (
          <li key={e.mapId}>
            <Link className="dep-step" to={`/w/${ws.slug}/maps/${e.mapId}?step=${step.id}`}>
              {e.mapName}
            </Link>
          </li>
        ))}
      </ul>
      {adding && <PlaceOnMap step={step} exclude={[view.map.id, ...others.map((e) => e.mapId)]} onClose={() => setAdding(false)} />}
    </section>
  )
}

/** Picks a map, then the activity and release there, and puts the step in that cell. */
function PlaceOnMap({ step, exclude, onClose }: { step: Step; exclude: string[]; onClose: () => void }) {
  const { ws, view, run } = useMap()
  const maps = useQuery({ queryKey: ['ws', ws.slug, 'maps'], queryFn: () => api.maps(ws.slug) })
  const [mapId, setMapId] = useState('')
  const target = useQuery({ queryKey: ['ws', ws.slug, 'map', mapId], queryFn: () => api.map(ws.slug, mapId), enabled: mapId !== '' })
  const [activityId, setActivityId] = useState('')
  const [releaseId, setReleaseId] = useState('')
  // A step keeps its status, so it can only go on maps that use the same workflow.
  const choices = maps.data?.filter((m) => !exclude.includes(m.id) && m.workflowId === view.map.workflowId) ?? []
  const objectives = new Map(target.data?.nodes.filter((n) => n.level === 'objective').map((n) => [n.id, n.title]))
  const activities = target.data?.nodes.filter((n) => n.level === 'activity') ?? []
  const name = choices.find((m) => m.id === mapId)?.name ?? ''
  return (
    <div className="dep-finder flex flex-col gap-2" role="group" aria-label={`Add ${step.key} to a map`}>
      {maps.data && choices.length === 0 ? (
        <p className="text-muted text-sm font-medium">It is on every map that uses this workflow already.</p>
      ) : (
        <select
          className="select select-sm w-full font-bold"
          aria-label="Map"
          value={mapId}
          onChange={(e) => {
            setMapId(e.target.value)
            setActivityId('')
            setReleaseId('')
          }}
        >
          <option value="">Choose a map…</option>
          {choices.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
      )}
      {target.data &&
        (activities.length === 0 ? (
          <p className="text-muted text-sm font-medium">That map has no activities yet.</p>
        ) : (
          <>
            <select
              className="select select-sm w-full font-bold"
              aria-label="Activity"
              value={activityId}
              onChange={(e) => setActivityId(e.target.value)}
            >
              <option value="">Choose an activity…</option>
              {activities.map((a) => (
                <option key={a.id} value={a.id}>
                  {objectives.get(a.parentId ?? '')} › {a.title}
                </option>
              ))}
            </select>
            <select className="select select-sm w-full font-bold" aria-label="Release" value={releaseId} onChange={(e) => setReleaseId(e.target.value)}>
              <option value="">Unscheduled</option>
              {target.data.releases.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name}
                </option>
              ))}
            </select>
          </>
        ))}
      <div className="flex gap-2">
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={!mapId || !activityId}
          onClick={() => {
            onClose()
            void run(() => api.placeStep(ws.slug, mapId, { stepId: step.id, activityId, releaseId: releaseId || null }), {
              done: `Added ${step.key} to ${name}`,
            })
          }}
        >
          Add
        </button>
        <button type="button" className="btn btn-sm btn-ol" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>
  )
}
