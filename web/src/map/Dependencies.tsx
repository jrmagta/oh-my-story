import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { api, type Dependency, type MapView, type SearchHit, type Status } from '../api'
import { useMap } from '../pages/map'

/** One end of a link, as the other step sees it. mapId is null for a step on this map. */
export interface Linked {
  dep: Dependency
  id: string
  key: string
  title: string
  done: boolean
  mapId: string | null
  mapName: string
}

/** What blocks a step and what it blocks, with each other step's name and whether it is done. */
export function links(view: MapView, statuses: Map<string, Status>, stepId: string): { blockedBy: Linked[]; blocks: Linked[] } {
  const end = (dep: Dependency, id: string): Linked | null => {
    const s = view.steps.find((x) => x.id === id)
    if (s) return { dep, id, key: s.key, title: s.title, done: statuses.get(s.statusId)?.category === 'done', mapId: null, mapName: view.map.name }
    const r = view.otherSteps.find((x) => x.id === id)
    return r ? { dep, id, key: r.key, title: r.title, done: r.done, mapId: r.mapId, mapName: r.mapName } : null
  }
  const byKey = (a: Linked, b: Linked) => a.key.localeCompare(b.key, undefined, { numeric: true })
  const blockedBy = view.dependencies.filter((d) => d.blockedId === stepId).map((d) => end(d, d.blockerId))
  const blocks = view.dependencies.filter((d) => d.blockerId === stepId).map((d) => end(d, d.blockedId))
  return {
    blockedBy: blockedBy.filter((x): x is Linked => x !== null).sort(byKey),
    blocks: blocks.filter((x): x is Linked => x !== null).sort(byKey),
  }
}

/** "Blocked by CHC-7" on a card while any of its blockers is unfinished. */
export function BlockedBadge({ stepId }: { stepId: string }) {
  const { view, statuses } = useMap()
  const open = links(view, statuses, stepId).blockedBy.filter((l) => !l.done)
  const [first] = open
  if (!first) return null
  return (
    <span className="blocked-badge" title={`Blocked by ${open.map((l) => `${l.key} ${l.title}`).join(', ')}`}>
      Blocked by {first.key}
      {open.length > 1 && ` +${open.length - 1}`}
    </span>
  )
}

/** The step panel's Blocked by and Blocks lists. Editors add links by finding a step, and remove them. */
export function DependencySection({ stepId, stepKey }: { stepId: string; stepKey: string }) {
  const { ws, view, canEdit, statuses, run } = useMap()
  const { blockedBy, blocks } = links(view, statuses, stepId)
  if (!canEdit && blockedBy.length === 0 && blocks.length === 0) return null
  const linked = new Set([stepId, ...blockedBy.map((l) => l.id), ...blocks.map((l) => l.id)])
  const create = (b: { blockerId: string; blockedId: string }) => run(() => api.createDependency(ws.slug, view.map.id, b))
  return (
    <section className="border-line border-b px-5 py-4" aria-label="Dependencies">
      <h3 className="label-up mb-2">Dependencies</h3>
      <DependencyRow
        title="Blocked by"
        items={blockedBy}
        stepKey={stepKey}
        exclude={linked}
        onAdd={canEdit ? (id) => create({ blockerId: id, blockedId: stepId }) : undefined}
      />
      <DependencyRow
        title="Blocks"
        items={blocks}
        stepKey={stepKey}
        exclude={linked}
        onAdd={canEdit ? (id) => create({ blockerId: stepId, blockedId: id }) : undefined}
      />
    </section>
  )
}

function DependencyRow(props: {
  title: 'Blocked by' | 'Blocks'
  items: Linked[]
  stepKey: string
  exclude: Set<string>
  onAdd?: (stepId: string) => void
}) {
  const { title, items, stepKey, exclude, onAdd } = props
  const { ws, view, canEdit, run, openStep } = useMap()
  const [adding, setAdding] = useState(false)
  if (!onAdd && items.length === 0) return null
  return (
    <div className="dep-row" role="group" aria-label={`${title} for ${stepKey}`}>
      <div className="flex items-center">
        <span className="text-muted flex-1 text-xs font-extrabold uppercase">{title}</span>
        {onAdd && !adding && (
          <button type="button" className="btn btn-xs btn-ol" onClick={() => setAdding(true)}>
            + Add
          </button>
        )}
      </div>
      {items.length > 0 && (
        <ul className="dep-list">
          {items.map((l) => (
            <li key={l.dep.id} className={l.done ? 'done' : ''}>
              {l.mapId === null ? (
                <button type="button" className="dep-step" onClick={() => openStep(l.id)}>
                  <b>{l.key}</b> {l.title}
                </button>
              ) : (
                <Link className="dep-step" to={`/w/${ws.slug}/maps/${l.mapId}?step=${l.id}`}>
                  <b>{l.key}</b> {l.title} <span className="text-muted">· {l.mapName}</span>
                </Link>
              )}
              {l.done && <span className="sr-only"> (done)</span>}
              {canEdit && (
                <button
                  type="button"
                  className="tag-x"
                  aria-label={`Remove link: ${title === 'Blocks' ? `${stepKey} blocks ${l.key}` : `${l.key} blocks ${stepKey}`}`}
                  onClick={() => run(() => api.deleteDependency(ws.slug, view.map.id, l.dep.id))}
                >
                  ✕
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {adding && onAdd && (
        <StepFinder
          label={title === 'Blocks' ? `Step that ${stepKey} blocks` : `Step that blocks ${stepKey}`}
          exclude={exclude}
          onPick={(id) => {
            setAdding(false)
            onAdd(id)
          }}
          onClose={() => setAdding(false)}
        />
      )}
    </div>
  )
}

/** Finds a step of the workspace by key or title words. */
function StepFinder(props: { label: string; exclude: Set<string>; onPick: (stepId: string) => void; onClose: () => void }) {
  const { label, exclude, onPick, onClose } = props
  const { ws } = useMap()
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  useEffect(() => {
    if (!q.trim()) {
      setHits(null)
      return
    }
    let live = true
    const t = setTimeout(() => {
      api.search(ws.slug, q).then(
        (h) => live && setHits(h),
        () => live && setHits([]),
      )
    }, 200)
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [q, ws.slug])
  const shown = hits?.filter((h) => !exclude.has(h.stepId)) ?? []
  return (
    <div className="dep-finder">
      <input
        autoFocus
        className="input input-sm w-full"
        aria-label={label}
        placeholder="Find a step by key or title"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onClose()
          if (e.key === 'Enter' && shown[0]) onPick(shown[0].stepId)
        }}
      />
      {hits !== null && (
        <ul className="dep-hits" aria-label="Matching steps">
          {shown.length === 0 && <li className="text-muted px-2 py-1 text-sm">No other steps match.</li>}
          {shown.map((h) => (
            <li key={h.stepId}>
              <button type="button" onClick={() => onPick(h.stepId)}>
                <b>{h.key}</b> {h.title} <span className="text-muted">· {h.mapName}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

interface Line {
  id: string
  d: string
  open: boolean
}

/**
 * Arrows from each blocker to the step it blocks, for links with both ends
 * shown on the map. Drawn over the grid from the cards' positions, and
 * redrawn when the map or its size changes.
 */
export function DependencyLines() {
  const { view, statuses, prefs, match, filter } = useMap()
  const ref = useRef<SVGSVGElement>(null)
  const [lines, setLines] = useState<Line[]>([])
  const [tick, setTick] = useState(0)

  useEffect(() => {
    const grid = ref.current?.parentElement
    if (!grid || !prefs.lines) return
    const ro = new ResizeObserver(() => setTick((t) => t + 1))
    ro.observe(grid)
    return () => ro.disconnect()
  }, [prefs.lines])

  useLayoutEffect(() => {
    const grid = ref.current?.parentElement
    if (!grid || !prefs.lines) return
    const box = grid.getBoundingClientRect()
    const at = (id: string) => grid.querySelector<HTMLElement>(`[data-step="${id}"]`)?.getBoundingClientRect()
    const next: Line[] = []
    for (const d of view.dependencies) {
      const a = at(d.blockerId)
      const b = at(d.blockedId)
      if (!a || !b) continue
      const blocker = view.steps.find((s) => s.id === d.blockerId)
      const open = blocker ? statuses.get(blocker.statusId)?.category !== 'done' : false
      // Leave from the side facing the other card; same column: bottom to top.
      let x1, y1, x2, y2, c1x, c1y, c2x, c2y
      if (Math.abs(a.left - b.left) < 4) {
        const down = a.top < b.top
        x1 = a.left + a.width / 2 - box.left
        x2 = b.left + b.width / 2 - box.left
        y1 = (down ? a.bottom : a.top) - box.top
        y2 = (down ? b.top : b.bottom) - box.top
        c1x = x1 + 30
        c2x = x2 + 30
        c1y = y1 + (down ? 20 : -20)
        c2y = y2 + (down ? -20 : 20)
      } else {
        const right = a.left < b.left
        x1 = (right ? a.right : a.left) - box.left
        x2 = (right ? b.left : b.right) - box.left
        y1 = a.top + a.height / 2 - box.top
        y2 = b.top + b.height / 2 - box.top
        const bend = Math.max(30, Math.abs(x2 - x1) / 2)
        c1x = x1 + (right ? bend : -bend)
        c2x = x2 + (right ? -bend : bend)
        c1y = y1
        c2y = y2
      }
      next.push({ id: d.id, d: `M${x1},${y1} C${c1x},${c1y} ${c2x},${c2y} ${x2},${y2}`, open })
    }
    setLines(next)
  }, [view, statuses, prefs, match, filter, tick])

  if (!prefs.lines) return null
  return (
    <svg ref={ref} className="dep-lines" aria-hidden>
      <defs>
        <marker id="dep-arrow-open" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M0,0 L10,5 L0,10 z" className="open" />
        </marker>
        <marker id="dep-arrow-done" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M0,0 L10,5 L0,10 z" className="done" />
        </marker>
      </defs>
      {lines.map((l) => (
        <path key={l.id} d={l.d} className={l.open ? 'open' : 'done'} markerEnd={`url(#dep-arrow-${l.open ? 'open' : 'done'})`} />
      ))}
    </svg>
  )
}
