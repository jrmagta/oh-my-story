import { createContext, useCallback, useContext, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useOutletContext, useParams, useSearchParams } from 'react-router'
import { api, ApiError, atLeast, type Category, type Level, type MapNode, type MapView, type Membership, type Release, type Status } from '../api'
import { MapDnd, refs, useDrag, useDrop } from '../map/dnd'
import { HistoryList } from '../map/HistoryList'
import { useLiveMap, type LiveState, type PresentUser } from '../map/live'
import { backbone, bands, cellKey, cells, type Band, type Card, type Column } from '../map/layout'
import { amount, formatEstimate, totals, type Total, type Totals } from '../map/estimates'
import { nodeStyle, palette } from '../map/colours'
import { colourFor, TagChooser } from '../map/TagPicker'
import { CardMenu, MenuItem } from '../map/CardMenu'
import { matching, readFilter, writeFilter, type Filter } from '../map/filter'
import { FilterBar } from '../map/FilterBar'
import { toggled, UNSCHEDULED_BAND, useViewPrefs, type ViewPrefs } from '../map/viewPrefs'
import { applyMove, type Move } from '../map/moves'
import { Avatar, ErrorAlert, errorMessage, InlineText, Loading, TextInput, Toasts, type ToastMessage } from '../ui'
import { PageBand } from './app'
import { StepPanel } from './step-panel'
import { ShareButton } from './share'
import { Roadmap } from './roadmap'

export const mapKey = (slug: string, mapId: string) => ['ws', slug, 'map', mapId] as const

export type ViewMode = 'map' | 'roadmap'

export interface MapCtx {
  ws: Membership
  view: MapView
  canEdit: boolean
  /** Read-only through a share link: no people, comments, history, presence or saved views. */
  shared: boolean
  /** The map laid out as backbone over release bands, or as a roadmap of releases across. */
  mode: ViewMode
  setMode: (m: ViewMode) => void
  statuses: Map<string, Status>
  /** Step counts and estimate sums for the map, each node and each release; only matching steps while a filter is on. */
  totals: Totals
  /** The filter in the URL, and the ids of the steps it matches (null when no filter is on). */
  filter: Filter
  match: Set<string> | null
  setFilter: (f: Filter) => void
  /** This browser's view of the map: card size, collapsed activities and bands, the release in focus. */
  prefs: ViewPrefs
  setPrefs: (p: ViewPrefs) => void
  /** Runs a write, then reloads the map. undo, when given, is offered in a notice. */
  run: (write: () => Promise<unknown>, opts?: { done?: string; undo?: () => Promise<unknown> }) => Promise<boolean>
  selectedStep: string | null
  openStep: (id: string | null) => void
  /** Shows a move at once, saves it, and puts things back if saving fails. */
  move: (m: Move) => void
  /** The open add-node input, as "level:parentId"; see useAddNode. */
  adding: string | null
  setAdding: (key: string | null) => void
}

const Ctx = createContext<MapCtx | null>(null)
export const MapProvider = Ctx.Provider

/**
 * The map's step selection (?step=) and filter, both in the URL. The filter is state mirrored into
 * the URL so a reload or a shared link keeps it; reading it back from the URL on every change would
 * lag, because the router updates the URL asynchronously (a checkbox flicked back, fast typing
 * dropped characters).
 */
export function useMapSearch() {
  const [search, setSearch] = useSearchParams()
  const selectedStep = search.get('step')
  const openStep = useCallback(
    (id: string | null) =>
      setSearch(
        (s) => {
          const n = new URLSearchParams(s)
          if (id) n.set('step', id)
          else n.delete('step')
          return n
        },
        { replace: true },
      ),
    [setSearch],
  )
  const [filter, setFilterState] = useState(() => readFilter(search))
  const setFilter = useCallback(
    (f: Filter) => {
      setFilterState(f)
      setSearch((s) => writeFilter(s, f), { replace: true })
    },
    [setSearch],
  )
  // Map or Roadmap, in the URL so a link or reload keeps it.
  const mode: ViewMode = search.get('view') === 'roadmap' ? 'roadmap' : 'map'
  const setMode = useCallback(
    (m: ViewMode) =>
      setSearch((s) => {
        const n = new URLSearchParams(s)
        if (m === 'roadmap') n.set('view', 'roadmap')
        else n.delete('view')
        return n
      }),
    [setSearch],
  )
  return { search, setSearch, selectedStep, openStep, filter, setFilter, mode, setMode }
}

/** What the map shows that follows from the payload and the filter. */
export function deriveMap(view: MapView, filter: Filter) {
  const statuses = new Map(view.workflow.statuses.map((s) => [s.id, s]))
  const match = matching(view, filter)
  return { statuses, match, totals: totals(view, statuses, match) }
}

export function useMap(): MapCtx {
  const c = useContext(Ctx)
  if (!c) throw new Error('useMap outside MapPage')
  return c
}

export function MapPage() {
  const ws = useOutletContext<Membership>()
  const { mapId = '' } = useParams()
  const qc = useQueryClient()
  const { search, setSearch, selectedStep, openStep, filter, setFilter, mode, setMode } = useMapSearch()
  const key = useMemo(() => mapKey(ws.slug, mapId), [ws.slug, mapId])
  const query = useQuery({ queryKey: key, queryFn: () => api.map(ws.slug, mapId), retry: false })
  const live = useLiveMap(ws.slug, mapId, key)

  const [toasts, setToasts] = useState<ToastMessage[]>([])
  const nextToast = useRef(1)
  const toast = useCallback((t: Omit<ToastMessage, 'id'>) => setToasts((ts) => [...ts, { ...t, id: nextToast.current++ }]), [])
  const dismiss = useCallback((id: number) => setToasts((ts) => ts.filter((t) => t.id !== id)), [])

  const run = useCallback<MapCtx['run']>(
    async (write, opts) => {
      try {
        await write()
        if (opts?.done) {
          const undo = opts.undo
          toast({
            text: opts.done,
            tone: 'info',
            action: undo && {
              label: 'Undo',
              run: () => {
                undo()
                  .catch((e: unknown) => toast({ text: errorMessage(e), tone: 'error' }))
                  .finally(() => qc.invalidateQueries({ queryKey: key }))
              },
            },
          })
        }
        return true
      } catch (e) {
        toast({ text: errorMessage(e), tone: 'error' })
        return false
      } finally {
        await qc.invalidateQueries({ queryKey: key })
      }
    },
    [qc, toast, key],
  )

  const move = useCallback(
    (m: Move) => {
      const before = qc.getQueryData<MapView>(key)
      if (!before) return
      void qc.cancelQueries({ queryKey: key })
      qc.setQueryData<MapView>(key, applyMove(before, m))
      const save = async () => {
        switch (m.kind) {
          case 'step': {
            const step = before.steps.find((s) => s.id === m.stepId)
            const r = await api.moveStep(ws.slug, mapId, m.stepId, {
              activityId: m.activityId,
              releaseId: m.releaseId,
              version: step?.version ?? 0,
              ...m.position,
            })
            if (r.stale && step) {
              toast({ text: `${step.key} was changed by someone else while you moved it. It is in its new place.`, tone: 'info' })
            }
            return
          }
          case 'node':
            await api.moveNode(ws.slug, mapId, m.nodeId, { parentId: m.parentId, ...m.position })
            return
          case 'release':
            await api.moveRelease(ws.slug, mapId, m.releaseId, m.position)
        }
      }
      void run(async () => {
        try {
          await save()
        } catch (e) {
          qc.setQueryData(key, before)
          throw e
        }
      })
    },
    [qc, key, run, toast, ws.slug, mapId],
  )

  const [adding, setAdding] = useState<string | null>(null)
  const [prefs, setPrefs] = useViewPrefs(mapId)

  const ctx = useMemo<MapCtx | null>(() => {
    if (!query.data) return null
    const { statuses, match, totals } = deriveMap(query.data, filter)
    return {
      ws,
      shared: false,
      mode,
      setMode,
      view: query.data,
      canEdit: atLeast(ws.role, 'editor'),
      statuses,
      totals,
      filter,
      match,
      setFilter,
      prefs,
      setPrefs,
      run,
      selectedStep,
      openStep,
      move,
      adding,
      setAdding,
    }
  }, [ws, query.data, filter, setFilter, prefs, setPrefs, mode, setMode, run, selectedStep, openStep, move, adding])

  if (query.isPending) return <Loading />
  if (!ctx) {
    const notFound = query.error instanceof ApiError && query.error.status === 404
    return (
      <>
        <PageBand kicker={`${ws.name} › Story maps`} title={notFound ? 'Not found' : 'Error'} />
        <main className="mx-auto max-w-5xl px-7 py-10">
          {notFound ? <p className="text-muted mb-6 font-medium">This map doesn't exist or was deleted.</p> : <ErrorAlert error={query.error} />}
          <Link className="btn btn-primary" to={`/w/${ws.slug}/maps`}>
            All maps
          </Link>
        </main>
      </>
    )
  }
  // One side panel at a time: the open step, else the activity feed.
  const stepOpen = !!selectedStep && ctx.view.steps.some((s) => s.id === selectedStep)
  const activityOpen = !stepOpen && search.has('activity')
  const panelOpen = stepOpen || activityOpen
  const toggleActivity = () =>
    setSearch(
      (s) => {
        const n = new URLSearchParams(s)
        if (activityOpen) n.delete('activity')
        else {
          n.set('activity', '')
          n.delete('step')
        }
        return n
      },
      { replace: true },
    )
  return (
    <Ctx.Provider value={ctx}>
      <MapHeader panelOpen={panelOpen} live={{ users: live.users, state: live.state, activityOpen, toggleActivity }} />
      <FilterBar panelOpen={panelOpen} />
      <div className={`map-scroll ${panelOpen ? 'with-panel' : ''}`}>
        <MapDnd view={ctx.view} onMove={move}>
          {ctx.mode === 'roadmap' ? <Roadmap /> : <MapGrid />}
        </MapDnd>
      </div>
      {stepOpen && <StepPanel stepId={selectedStep} />}
      {activityOpen && <ActivityPanel onClose={toggleActivity} />}
      <Toasts items={toasts} dismiss={dismiss} />
    </Ctx.Provider>
  )
}

const categoryOrder: { category: Category; label: string; ring: string }[] = [
  { category: 'done', label: 'Done', ring: 'green' },
  { category: 'doing', label: 'In progress', ring: 'amber' },
  { category: 'blocked', label: 'Blocked', ring: 'red' },
  { category: 'todo', label: 'To do', ring: 'todo' },
]

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

/** "8 points" plus how many steps lack an estimate; empty when nothing is estimated yet. */
export function estimated(t: Total, unit: string): string {
  if (t.steps === t.unestimated) return ''
  return t.unestimated ? `${amount(t.hundredths, unit)} (${t.unestimated} unestimated)` : amount(t.hundredths, unit)
}

const liveLabel: Record<LiveState, string> = { connecting: 'Connecting…', live: 'Live', offline: 'Offline, reconnecting…' }

/** Who is on the map now, and whether this page is receiving their changes. */
function Presence({ users, state }: { users: PresentUser[]; state: LiveState }) {
  return (
    <div className="presence" aria-label="On this map now">
      <span className={`live-dot ${state}`} role="status">
        {liveLabel[state]}
      </span>
      <ul className="flex -space-x-1">
        {users.map((u) => (
          <li key={u.id} title={u.name}>
            <Avatar id={u.id} name={u.name} size={30} />
            <span className="sr-only">{u.name}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

/** Map or Roadmap: two layouts of the same steps. */
function ModeSwitch() {
  const { mode, setMode } = useMap()
  return (
    <span className="seg mt-3" role="group" aria-label="View">
      {(['map', 'roadmap'] as const).map((m) => (
        <button key={m} type="button" aria-pressed={mode === m} onClick={() => setMode(m)}>
          {m === 'map' ? 'Map' : 'Roadmap'}
        </button>
      ))}
    </span>
  )
}

/** The map's name and counts. live is the signed-in map's presence and Activity; a shared page offers Refresh instead. */
export function MapHeader(props: {
  panelOpen: boolean
  live?: { users: PresentUser[]; state: LiveState; activityOpen: boolean; toggleActivity: () => void }
  onRefresh?: () => void
}) {
  const { panelOpen, live } = props
  const { ws, view, canEdit, totals, run, shared } = useMap()
  const count = (level: Level) => view.nodes.filter((n) => n.level === level).length
  const facts = [
    plural(count('objective'), 'objective'),
    plural(count('activity'), 'activity').replace('activitys', 'activities'),
    plural(view.steps.length, 'step'),
    estimated(totals.map, ws.estimateUnit),
  ].filter(Boolean)
  return (
    <section className={`mhead ${panelOpen ? 'with-panel' : ''}`}>
      <div className="min-w-0">
        {shared ? (
          <span className="label-up">{ws.name} › Read-only link</span>
        ) : (
          <Link to={`/w/${ws.slug}/maps`} className="label-up hover:underline">
            {ws.name} › Story maps
          </Link>
        )}
        {/* The title can be a rename button; the heading still reads as the map's name. */}
        <h1 className="mt-1" aria-label={view.map.name}>
          <InlineText
            value={view.map.name}
            label="map"
            editable={canEdit}
            maxLength={100}
            onSave={(name) => run(() => api.patchMap(ws.slug, view.map.id, { name }))}
          />
        </h1>
        <p className="text-muted mt-1.5 text-sm font-semibold">{facts.join(' · ')}</p>
        <ModeSwitch />
      </div>
      <div className="flex flex-col items-end gap-3" style={{ marginLeft: 'auto' }}>
        {live && (
          <>
            <Presence users={live.users} state={live.state} />
            <ShareButton />
            <button type="button" className="btn btn-xs btn-ol" aria-pressed={live.activityOpen} onClick={live.toggleActivity}>
              Activity
            </button>
          </>
        )}
        {props.onRefresh && (
          <button type="button" className="btn btn-xs btn-ol" onClick={props.onRefresh}>
            Refresh
          </button>
        )}
      </div>
      <div className="kpis" aria-label="Steps by status">
        {categoryOrder.map((c) => (
          <div className="kpi" key={c.category}>
            <span className={`ring-disc ${c.ring}`}>{totals.map.byCategory[c.category]}</span>
            {c.label}
          </div>
        ))}
      </div>
    </section>
  )
}

const levelLabel: Record<Level, string> = { objective: 'Objective', activity: 'Activity' }

export function MapGrid() {
  const { view, canEdit, prefs } = useMap()
  const bb = useMemo(() => backbone(view), [view])
  const all = useMemo(() => bands(view), [view])
  // A focused release shows alone; a focus on a release that no longer exists shows everything.
  const focused = all.filter((b) => (b.release?.id ?? UNSCHEDULED_BAND) === prefs.focus)
  const bs = focused.length ? focused : all
  const collapsed = (c: Column) => !!c.activity && prefs.collapsedActivities.includes(c.activity.id)
  const byCell = useMemo(() => cells(view), [view])

  if (bb.columns.length === 0) {
    return (
      <div className="max-w-xl py-10">
        <p className="up mb-2 text-2xl">An empty map</p>
        <p className="text-muted mb-6 font-medium">Start the backbone with an objective, then add its activities and the steps under them.</p>
        {canEdit && <AddNode level="objective" parent={null} label="Add objective" />}
      </div>
    )
  }

  // Columns: row labels, one per backbone column, and one for adding at the end.
  const widths = bb.columns.map((c) => (collapsed(c) ? '52px' : '184px')).join(' ')
  // The last, flexible column takes up whatever width a release band needs beyond the others (on a
  // narrow map, or with activities collapsed), so bands never stretch the label column.
  const style = { gridTemplateColumns: `max-content ${widths}${canEdit ? ' 150px' : ''} minmax(0, 1fr)` }
  const col = (start: number, span = 1) => ({ gridColumn: `${start + 2} / span ${span}` })
  const trailing = { gridColumn: `${bb.columns.length + 2}` }

  return (
    <div className="map-grid" style={style}>
      <div className="map-lbl">Objective</div>
      {bb.objectives.map((o) => (
        <div
          key={o.node.id}
          className={`map-obj up ${o.node.colour ? 'coloured' : ''}`}
          style={{ ...col(o.start, o.span), ...nodeStyle(o.node.colour) }}
        >
          <NodeHead node={o.node} child="activity" />
        </div>
      ))}
      {canEdit && (
        <div style={trailing}>
          <AddNode level="objective" parent={null} label="Add objective" />
        </div>
      )}

      <div className="map-lbl" style={{ gridColumn: 1 }}>
        Activity
      </div>
      {bb.columns.map((c, i) =>
        c.activity ? (
          <div
            key={c.key}
            className={`map-act ${c.activity.colour ? 'coloured' : ''} ${collapsed(c) ? 'collapsed' : ''}`}
            style={{ ...col(i), ...nodeStyle(c.activity.colour) }}
          >
            {collapsed(c) ? <CollapsedActivity node={c.activity} /> : <NodeHead node={c.activity} bold />}
          </div>
        ) : (
          <Slot key={c.key} style={col(i)} level="activity" parent={c.objective}>
            {canEdit && <AddNode level="activity" parent={c.objective} label={`Add activity to ${c.objective.title}`} short="Add activity" />}
          </Slot>
        ),
      )}

      {bs.map((b, i) => (
        <BandRows
          key={b.key}
          band={b}
          index={all.indexOf(b)}
          columns={bb.columns}
          byCell={byCell}
          isLast={!focused.length && i === bs.length - 1}
          collapsedColumn={collapsed}
        />
      ))}
    </div>
  )
}

/**
 * A backbone card's content. The whole card drags (with the pointer, or Space
 * once focused); its ⋯ menu renames, recolours, links personas (objectives),
 * adds an activity (objectives) and deletes.
 */
function NodeHead({ node, child, bold }: { node: MapNode; child?: Level; bold?: boolean }) {
  const { ws, view, canEdit, totals, run } = useMap()
  const label = `${node.level} ${node.title}`
  const total = totals.nodes.get(node.id)
  const sum = total && estimated(total, ws.estimateUnit)
  const [renaming, setRenaming] = useState(false)
  const add = useAddNode(child ?? 'activity', node)
  // Until a node has children, the empty column under it holds the add field; showing it here
  // too would put two fields on one key, and the second would close the first by taking focus.
  const hasChildren = view.nodes.some((n) => n.parentId === node.id)
  const drag = useDrag(`node:${node.id}`, { kind: 'node', node, label }, !canEdit || renaming)
  const drop = useDrop(`node:${node.id}`, { kind: 'node', node, label })
  return (
    // The pointer drags from anywhere on the card; the keyboard handle is the title alone, so the
    // rename and add fields and the menu are not inside a button (whose content is presentational).
    <div
      ref={refs(drag.ref, drop.ref)}
      {...drag.id}
      className={`node-head ${drop.over ? 'drop' : ''} ${drag.dragging ? 'dragging' : ''} ${canEdit ? 'grab-area' : ''}`}
      {...drag.pointer}
    >
      {/* First, so it floats at the right of the title's first line and the title wraps around it. */}
      {(canEdit || node.level === 'activity') && !renaming && (
        <CardMenu label={label}>
          {(close) => (
            <NodeMenu node={node} label={label} close={close} rename={() => setRenaming(true)} addChild={child ? () => add.setOpen(true) : undefined} />
          )}
        </CardMenu>
      )}
      {renaming ? (
        <TextInput
          label={`New name for ${label}`}
          initial={node.title}
          className={`title-input ${bold ? 'text-[15px]' : ''}`}
          onDone={(title) => {
            setRenaming(false)
            if (title && title !== node.title) void run(() => api.patchNode(ws.slug, view.map.id, node.id, { title }))
          }}
        />
      ) : (
        <span
          ref={drag.keysRef}
          {...drag.keys}
          aria-label={canEdit ? `Move ${label}` : undefined}
          className={`node-title ${bold ? 'up text-[15px]' : ''} ${canEdit ? 'grab' : ''}`}
        >
          {node.title}
        </span>
      )}
      {sum && (
        <span className="node-total" aria-label={`Total for ${label}: ${sum}`}>
          {sum}
        </span>
      )}
      {node.level === 'objective' && <PersonaBadges node={node} />}
      {child && hasChildren && add.open && (
        <span className="node-popup" data-no-drag>
          {add.input}
        </span>
      )}
    </div>
  )
}

/** A collapsed activity: a narrow strip with its name, and a button to open it out again. */
function CollapsedActivity({ node }: { node: MapNode }) {
  const { prefs, setPrefs } = useMap()
  return (
    <button
      type="button"
      className="collapsed-act"
      aria-label={`Expand activity ${node.title}`}
      title={`Expand ${node.title}`}
      onClick={() => setPrefs({ ...prefs, collapsedActivities: toggled(prefs.collapsedActivities, node.id) })}
    >
      <span>{node.title}</span>
    </button>
  )
}

function NodeMenu(props: { node: MapNode; label: string; close: () => void; rename: () => void; addChild?: () => void }) {
  const { node, label, close } = props
  const { ws, view, run, canEdit, prefs, setPrefs } = useMap()
  const [panel, setPanel] = useState<'main' | 'colour' | 'personas'>('main')
  if (panel === 'colour') {
    return (
      <ColourChooser
        node={node}
        label={label}
        onPick={(colour) => {
          close()
          if (colour !== node.colour) void run(() => api.patchNode(ws.slug, view.map.id, node.id, { colour }))
        }}
      />
    )
  }
  if (panel === 'personas') {
    return (
      <TagChooser
        noun="persona"
        tags={view.personas}
        selected={node.personaIds}
        onToggle={(id) =>
          run(() =>
            api.patchNode(ws.slug, view.map.id, node.id, {
              personaIds: node.personaIds.includes(id) ? node.personaIds.filter((x) => x !== id) : [...node.personaIds, id],
            }),
          )
        }
        onCreate={(name) =>
          run(async () => {
            const p = await api.createPersona(ws.slug, { name, colour: colourFor(name) })
            await api.patchNode(ws.slug, view.map.id, node.id, { personaIds: [...node.personaIds, p.id] })
          })
        }
      />
    )
  }
  const then = (f: () => void) => () => {
    close()
    f()
  }
  // Collapsing is a personal view setting, so everyone gets it; the rest edits the map.
  const collapse = node.level === 'activity' && (
    <MenuItem onClick={then(() => setPrefs({ ...prefs, collapsedActivities: toggled(prefs.collapsedActivities, node.id) }))}>
      Collapse
    </MenuItem>
  )
  if (!canEdit) return collapse
  return (
    <>
      {collapse}
      <MenuItem onClick={then(props.rename)}>Rename</MenuItem>
      {props.addChild && <MenuItem onClick={then(props.addChild)}>Add activity</MenuItem>}
      {node.level === 'objective' && <MenuItem onClick={() => setPanel('personas')}>Personas…</MenuItem>}
      <MenuItem onClick={() => setPanel('colour')}>Colour…</MenuItem>
      <MenuItem
        danger
        onClick={then(() =>
          run(() => api.deleteNode(ws.slug, view.map.id, node.id), {
            done: `Deleted ${node.level} “${node.title}”`,
            undo: () => api.restoreNode(ws.slug, view.map.id, node.id),
          }),
        )}
      >
        Delete
      </MenuItem>
    </>
  )
}

/** A backbone card's colour: a swatch, any colour, or the level's default look. */
function ColourChooser({ node, label, onPick }: { node: MapNode; label: string; onPick: (colour: string | null) => void }) {
  return (
    <div className="colour-grid" role="group" aria-label={`Colours for ${label}`}>
      {palette.map((c) => (
        <button
          key={c.hex}
          type="button"
          className="swatch"
          style={{ background: c.hex }}
          aria-label={c.name}
          aria-pressed={node.colour?.toUpperCase() === c.hex}
          title={c.name}
          onClick={() => onPick(c.hex)}
        />
      ))}
      <label className="swatch custom" title="Any colour">
        {/* React's onChange fires on every move in the picker; the native change event fires once, when it closes. */}
        <input
          type="color"
          aria-label="Any colour"
          defaultValue={(node.colour ?? '#8A8A93').toLowerCase()}
          ref={(el) => {
            if (el) el.onchange = () => onPick(el.value.toUpperCase())
          }}
        />
      </label>
      <button type="button" className="btn btn-xs btn-ol col-span-full" aria-pressed={!node.colour} onClick={() => onPick(null)}>
        Default
      </button>
    </div>
  )
}

/**
 * Adding backbone nodes. Which add input is open lives in the map context, not
 * in the button, because the button that opened it can unmount mid-entry: the
 * empty column that offers a first activity becomes that activity. The input then
 * reappears at the parent's own add button, still open.
 */
function useAddNode(level: Level, parent: MapNode | null) {
  const { ws, view, run, adding, setAdding } = useMap()
  const key = `${level}:${parent?.id ?? ''}`
  return {
    open: adding === key,
    setOpen: (on: boolean) => setAdding(on ? key : null),
    input: (
      <TextInput
        label={`New ${level}${parent ? ` in ${parent.title}` : ''}`}
        placeholder={`${levelLabel[level]} name`}
        keepOpen
        onDone={(title) => {
          if (!title) setAdding(null)
          else run(() => api.createNode(ws.slug, view.map.id, { level, title, ...(parent ? { parentId: parent.id } : {}) }))
        }}
      />
    ),
  }
}

function AddNode({ level, parent, label, short }: { level: Level; parent: MapNode | null; label: string; short?: string }) {
  const add = useAddNode(level, parent)
  if (add.open) return add.input
  return (
    <button type="button" className="add-slot w-full" aria-label={label} onClick={() => add.setOpen(true)}>
      + {short ?? label}
    </button>
  )
}

/** An empty column under a node: a place to add, or drop, its first child. */
function Slot(props: { style: CSSProperties; level: Level; parent: MapNode; children: ReactNode }) {
  const drop = useDrop(`slot:${props.level}:${props.parent.id}`, {
    kind: 'slot',
    level: props.level,
    parentId: props.parent.id,
    label: `the empty ${props.level} column of ${props.parent.title}`,
  })
  return (
    <div ref={drop.ref} style={props.style} className={drop.over ? 'drop' : undefined}>
      {props.children}
    </div>
  )
}

function BandRows(props: {
  band: Band
  index: number
  columns: Column[]
  byCell: Map<string, Card[]>
  isLast: boolean
  collapsedColumn: (c: Column) => boolean
}) {
  const { canEdit, totals, prefs } = useMap()
  const { band, columns, byCell } = props
  const name = band.release?.name ?? 'Unscheduled'
  const bandCollapsed = prefs.collapsedReleases.includes(band.release?.id ?? UNSCHEDULED_BAND)
  return (
    <>
      {props.isLast && canEdit && <AddRelease />}
      <BandHeader release={band.release} index={props.index} total={totals.releases.get(band.release?.id ?? '')} />
      {!bandCollapsed && <div className="map-lbl" style={{ gridColumn: 1 }} />}
      {!bandCollapsed && columns.map((c, i) =>
        c.activity ? (
          <Cell
            key={c.key}
            column={i + 2}
            activity={c.activity}
            release={band.release}
            bandName={name}
            narrow={props.collapsedColumn(c)}
            cards={byCell.get(cellKey(c.activity.id, band.release?.id ?? null)) ?? []}
          />
        ) : (
          <div key={c.key} style={{ gridColumn: i + 2 }} />
        ),
      )}
    </>
  )
}

export function formatDate(d: string): string {
  return new Date(`${d}T00:00:00`).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

function BandHeader({ release, index, total }: { release: Release | null; index: number; total: Total | undefined }) {
  const { ws, view, canEdit, run, prefs, setPrefs } = useMap()
  const tone = release ? (index % 2 === 0 ? 'green' : 'amber') : ''
  const bandId = release?.id ?? UNSCHEDULED_BAND
  const collapsed = prefs.collapsedReleases.includes(bandId)
  const focused = prefs.focus === bandId
  const name = release?.name ?? 'Unscheduled'
  const label = `release ${name}`
  // Unscheduled stays last: a release dropped on it moves to the end of the releases.
  const placeholder = { id: 'unscheduled', name, description: '', targetDate: null, rank: '' }
  const drag = useDrag(`release:${release?.id ?? 'unscheduled'}`, { kind: 'release', release: release ?? placeholder, label }, !canEdit || !release)
  const drop = useDrop(
    `release:${release?.id ?? 'unscheduled'}`,
    release ? { kind: 'release', release, label } : { kind: 'unscheduled', label: 'the end of the releases' },
  )
  const [renaming, setRenaming] = useState(false)
  const movable = release && canEdit && !renaming
  return (
    <div
      ref={refs(drag.ref, drop.ref)}
      {...drag.id}
      className={`band-row ${tone} ${drop.over ? 'drop' : ''} ${drag.dragging ? 'dragging' : ''}`}
      role="heading"
      aria-level={2}
      aria-label={`Release ${name}`}
      {...(movable ? drag.pointer : {})}
    >
      <button
        type="button"
        className="band-toggle"
        data-no-drag
        aria-expanded={!collapsed}
        aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${label}`}
        onClick={() => setPrefs({ ...prefs, collapsedReleases: toggled(prefs.collapsedReleases, bandId) })}
      >
        {collapsed ? '▸' : '▾'}
      </button>
      {release && renaming ? (
        <TextInput
          label={`New name for ${label}`}
          initial={release.name}
          maxLength={100}
          className="title-input band-input"
          onDone={(n) => {
            setRenaming(false)
            if (n && n !== release.name) void run(() => api.patchRelease(ws.slug, view.map.id, release.id, { name: n }))
          }}
        />
      ) : (
        // The band stays a heading; its name takes focus for moving it with the keyboard.
        <span
          className={`n ${movable ? 'grab' : ''}`}
          ref={movable ? drag.keysRef : undefined}
          {...(movable ? { ...drag.keys, 'aria-label': `Move ${label}` } : {})}
        >
          {name}
        </span>
      )}
      {release && (
        <span data-no-drag>
          <TargetDate release={release} />
        </span>
      )}
      <span className="r">
        <span>{[plural(total?.steps ?? 0, 'step'), total && estimated(total, ws.estimateUnit)].filter(Boolean).join(' · ')}</span>
        {total && <Meter total={total} />}
        <button
          type="button"
          className="btn btn-xs btn-ol"
          data-no-drag
          aria-pressed={focused}
          aria-label={focused ? 'Show all releases' : `Focus ${label}`}
          onClick={() => setPrefs({ ...prefs, focus: focused ? null : bandId })}
        >
          {focused ? 'Show all' : 'Focus'}
        </button>
        {release && canEdit && !renaming && (
          <CardMenu label={label}>
            {(close) => (
              <>
                <MenuItem
                  onClick={() => {
                    close()
                    setRenaming(true)
                  }}
                >
                  Rename
                </MenuItem>
                <MenuItem
                  danger
                  onClick={() => {
                    close()
                    void run(() => api.deleteRelease(ws.slug, view.map.id, release.id), {
                      done: `Deleted release “${release.name}”. Its steps moved to Unscheduled.`,
                    })
                  }}
                >
                  Delete
                </MenuItem>
              </>
            )}
          </CardMenu>
        )}
      </span>
    </div>
  )
}

/** Share of the release's steps done, in progress and blocked. */
export function Meter({ total }: { total: Total }) {
  const pct = (n: number) => `${(100 * n) / total.steps}%`
  const done = total.byCategory.done
  return (
    <span
      className="meter"
      role="img"
      aria-label={`${done} of ${total.steps} done, ${total.byCategory.doing} in progress, ${total.byCategory.blocked} blocked`}
    >
      <b className="done" style={{ width: pct(done) }} />
      <b className="doing" style={{ width: pct(total.byCategory.doing) }} />
      <b className="blocked" style={{ width: pct(total.byCategory.blocked) }} />
    </span>
  )
}

function TargetDate({ release }: { release: Release }) {
  const { ws, view, canEdit, run } = useMap()
  const [editing, setEditing] = useState(false)
  const text = release.targetDate ? formatDate(release.targetDate) : 'No date'
  if (!canEdit) return <span className="d">{text}</span>
  if (!editing) {
    return (
      <button type="button" className="date-btn" aria-label={`Target date for ${release.name}: ${text}`} onClick={() => setEditing(true)}>
        {text}
      </button>
    )
  }
  return (
    <input
      type="date"
      autoFocus
      aria-label={`Target date for ${release.name}`}
      defaultValue={release.targetDate ?? ''}
      onBlur={() => setEditing(false)}
      onKeyDown={(e) => e.key === 'Escape' && setEditing(false)}
      onChange={(e) => {
        // Typing fires a change per field; only save a complete date or a cleared one.
        const v = e.target.value
        if (v === '' || /^\d{4}-\d{2}-\d{2}$/.test(v)) {
          run(() => api.patchRelease(ws.slug, view.map.id, release.id, { targetDate: v || null }))
        }
      }}
    />
  )
}

function AddRelease() {
  const { ws, view, run } = useMap()
  const [open, setOpen] = useState(false)
  return (
    <div style={{ gridColumn: '2 / span 2', marginTop: 22 }}>
      {open ? (
        <TextInput
          label="New release name"
          placeholder="Release name"
          onDone={(name) => {
            setOpen(false)
            if (name) run(() => api.createRelease(ws.slug, view.map.id, { name }))
          }}
        />
      ) : (
        <button type="button" className="add-slot w-full" onClick={() => setOpen(true)}>
          + Add release
        </button>
      )}
    </div>
  )
}

/** narrow: the activity is collapsed, so the cell shows its count and still takes drops. */
export function Cell(props: { column: number; activity: MapNode; release: Release | null; bandName: string; cards: Card[]; narrow: boolean }) {
  const { activity, release, bandName, cards } = props
  const { ws, view, canEdit, run, filter, match } = useMap()
  const [adding, setAdding] = useState(false)
  const where = `${activity.title} / ${bandName}`
  const drop = useDrop(`cell:${cellKey(activity.id, release?.id ?? null)}`, {
    kind: 'cell',
    activityId: activity.id,
    releaseId: release?.id ?? null,
    label: `the end of ${where}`,
  })
  return (
    <div
      ref={drop.ref}
      role="group"
      aria-label={`Steps in ${where}`}
      className={`map-cell ${drop.over ? 'drop' : ''} ${props.narrow ? 'narrow' : ''}`}
      style={{ gridColumn: props.column }}
    >
      {props.narrow ? (
        <span className="narrow-count" title={`${cards.length} step${cards.length === 1 ? '' : 's'}`}>
          {cards.length || ''}
        </span>
      ) : (
        cards
        .filter((c) => !match || filter.mode === 'highlight' || match.has(c.step.id))
        .map((c) => (
          <StepCard key={c.step.id} card={c} dimmed={!!match && !match.has(c.step.id)} />
        ))
      )}
      {canEdit &&
        !props.narrow &&
        (adding ? (
          <TextInput
            label={`New step in ${where}`}
            placeholder="Step title"
            keepOpen
            onDone={async (title) => {
              if (!title) {
                setAdding(false)
                return
              }
              await run(() => api.createStep(ws.slug, view.map.id, { title, activityId: activity.id, releaseId: release?.id ?? null }))
            }}
          />
        ) : (
          <button type="button" className="add-step" aria-label={`Add step to ${where}`} onClick={() => setAdding(true)}>
            + Step
          </button>
        ))}
    </div>
  )
}

export function StatusLabel({ status }: { status: Status | undefined }) {
  return <span className={`st ${status?.category ?? 'todo'}`}>{status?.name ?? 'No status'}</span>
}

/** The personas an objective serves, as pills under its title. */
function PersonaBadges({ node }: { node: MapNode }) {
  const { view } = useMap()
  const ps = view.personas.filter((p) => node.personaIds.includes(p.id)).sort((a, b) => a.name.localeCompare(b.name))
  if (ps.length === 0) return null
  return (
    <span className="personas" aria-label={`Personas: ${ps.map((p) => p.name).join(', ')}`}>
      {ps.map((p) => (
        <span key={p.id} className="persona" title={p.description || undefined}>
          <i style={{ background: p.colour }} aria-hidden />
          {p.name}
        </span>
      ))}
    </span>
  )
}

/** A step's labels as small chips, in name order; ids of deleted labels are skipped. */
export function LabelChips({ ids, onRemove }: { ids: string[]; onRemove?: (id: string) => void }) {
  const { view } = useMap()
  const chips = view.labels.filter((l) => ids.includes(l.id)).sort((a, b) => a.name.localeCompare(b.name))
  if (chips.length === 0) return null
  return (
    <span className="chips">
      {chips.map((l) => (
        <span key={l.id} className="tag">
          <i style={{ background: l.colour }} aria-hidden />
          {l.name}
          {onRemove && (
            <button type="button" className="tag-x" aria-label={`Remove label ${l.name}`} onClick={() => onRemove(l.id)}>
              ✕
            </button>
          )}
        </span>
      ))}
    </span>
  )
}

/** dimmed: a highlight filter is on and this step does not match. */
function StepCard({ card, dimmed }: { card: Card; dimmed: boolean }) {
  const { ws, canEdit, statuses, selectedStep, openStep, prefs } = useMap()
  const { step, placement } = card
  const status = statuses.get(step.statusId)
  const selected = selectedStep === step.id
  const label = `${step.key} ${step.title}`
  const drag = useDrag(`step:${step.id}`, { kind: 'step', stepId: step.id, placement, label }, !canEdit)
  const drop = useDrop(`step:${step.id}`, { kind: 'step', stepId: step.id, placement, label })
  return (
    <div
      ref={refs(drag.ref, drop.ref)}
      {...drag.id}
      className={`step-card size-${prefs.size} ${drop.over ? 'drop' : ''} ${drag.dragging ? 'dragging' : ''} ${dimmed ? 'dimmed' : ''}`}
      {...drag.pointer}
    >
      {/* Click or Enter opens the step; Space picks it up to move it. */}
      <button
        type="button"
        ref={drag.keysRef}
        {...drag.keys}
        className={`card-open ${canEdit ? 'grab' : ''}`}
        aria-current={selected}
        aria-label={label}
        onClick={() => openStep(selected ? null : step.id)}
      >
        <span>{step.title}</span>
        <LabelChips ids={step.labelIds} />
        <span className="meta">
          <StatusLabel status={status} />
          {step.commentCount > 0 && (
            <span className="comment-count" title={`${step.commentCount} comment${step.commentCount === 1 ? '' : 's'}`}>
              <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden>
                <path d="M2 3h12v8H7l-3 3v-3H2z" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
              </svg>
              {step.commentCount}
            </span>
          )}
          <span className="text-muted ml-auto text-[11px] font-extrabold">{step.key}</span>
          {step.estimate !== null && (
            <span className={`ring-disc est ${status?.category ?? 'todo'}`} title={amount(step.estimate * 100, ws.estimateUnit)}>
              {formatEstimate(step.estimate)}
            </span>
          )}
        </span>
      </button>
    </div>
  )
}

/** Everything that happened on the map, newest first. */
function ActivityPanel({ onClose }: { onClose: () => void }) {
  const { ws, view, openStep } = useMap()
  return (
    <aside className="step-panel" aria-label="Map activity">
      <div className="hd flex items-center">
        <h2 className="flex-1" style={{ marginTop: 0 }}>
          Activity
        </h2>
        <button type="button" className="icon-btn" aria-label="Close activity" onClick={onClose}>
          <span>✕</span>
        </button>
      </div>
      <HistoryList
        slug={ws.slug}
        view={view}
        queryKey={[...mapKey(ws.slug, view.map.id), 'history']}
        page={(before) => api.mapHistory(ws.slug, view.map.id, before)}
        label="Map activity"
        onStep={openStep}
      />
    </aside>
  )
}
