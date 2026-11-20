import { createContext, useCallback, useContext, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useOutletContext, useParams, useSearchParams } from 'react-router'
import { api, ApiError, atLeast, type Category, type Level, type MapNode, type MapView, type Membership, type Release, type Status } from '../api'
import { MapDnd, refs, useDrag, useDrop } from '../map/dnd'
import { HistoryList } from '../map/HistoryList'
import { useLiveMap, type LiveState, type PresentUser } from '../map/live'
import { backbone, bands, cellKey, cells, type Band, type Card, type Column } from '../map/layout'
import { applyMove, type Move } from '../map/moves'
import { Avatar, ErrorAlert, errorMessage, InlineText, Loading, TextInput, Toasts, type ToastMessage } from '../ui'
import { PageBand } from './app'
import { StoryPanel } from './story-panel'

export const mapKey = (slug: string, mapId: string) => ['ws', slug, 'map', mapId] as const

interface MapCtx {
  ws: Membership
  view: MapView
  canEdit: boolean
  statuses: Map<string, Status>
  /** Runs a write, then reloads the map. undo, when given, is offered in a notice. */
  run: (write: () => Promise<unknown>, opts?: { done?: string; undo?: () => Promise<unknown> }) => Promise<boolean>
  selectedStory: string | null
  openStory: (id: string | null) => void
  /** Shows a move at once, saves it, and puts things back if saving fails. */
  move: (m: Move) => void
  /** The open add-node input, as "level:parentId"; see useAddNode. */
  adding: string | null
  setAdding: (key: string | null) => void
}

const Ctx = createContext<MapCtx | null>(null)

export function useMap(): MapCtx {
  const c = useContext(Ctx)
  if (!c) throw new Error('useMap outside MapPage')
  return c
}

export function MapPage() {
  const ws = useOutletContext<Membership>()
  const { mapId = '' } = useParams()
  const qc = useQueryClient()
  const [search, setSearch] = useSearchParams()
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
          case 'story': {
            const story = before.stories.find((s) => s.id === m.storyId)
            const r = await api.moveStory(ws.slug, mapId, m.storyId, {
              stepId: m.stepId,
              releaseId: m.releaseId,
              version: story?.version ?? 0,
              ...m.position,
            })
            if (r.stale && story) {
              toast({ text: `${story.key} was changed by someone else while you moved it. It is in its new place.`, tone: 'info' })
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
  const selectedStory = search.get('story')
  const openStory = useCallback(
    (id: string | null) =>
      setSearch(
        (s) => {
          const n = new URLSearchParams(s)
          if (id) n.set('story', id)
          else n.delete('story')
          return n
        },
        { replace: true },
      ),
    [setSearch],
  )

  const ctx = useMemo<MapCtx | null>(
    () =>
      query.data
        ? {
            ws,
            view: query.data,
            canEdit: atLeast(ws.role, 'editor'),
            statuses: new Map(query.data.workflow.statuses.map((s) => [s.id, s])),
            run,
            selectedStory,
            openStory,
            move,
            adding,
            setAdding,
          }
        : null,
    [ws, query.data, run, selectedStory, openStory, move, adding],
  )

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
  // One side panel at a time: the open story, else the activity feed.
  const storyOpen = !!selectedStory && ctx.view.stories.some((s) => s.id === selectedStory)
  const activityOpen = !storyOpen && search.has('activity')
  const panelOpen = storyOpen || activityOpen
  const toggleActivity = () =>
    setSearch(
      (s) => {
        const n = new URLSearchParams(s)
        if (activityOpen) n.delete('activity')
        else {
          n.set('activity', '')
          n.delete('story')
        }
        return n
      },
      { replace: true },
    )
  return (
    <Ctx.Provider value={ctx}>
      <MapHeader panelOpen={panelOpen} users={live.users} state={live.state} activityOpen={activityOpen} toggleActivity={toggleActivity} />
      <div className={`map-scroll ${panelOpen ? 'with-panel' : ''}`}>
        <MapDnd view={ctx.view} onMove={move}>
          <MapGrid />
        </MapDnd>
      </div>
      {storyOpen && <StoryPanel storyId={selectedStory} />}
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

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
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

function MapHeader(props: {
  panelOpen: boolean
  users: PresentUser[]
  state: LiveState
  activityOpen: boolean
  toggleActivity: () => void
}) {
  const { panelOpen, users, state } = props
  const { ws, view, canEdit, statuses, run } = useMap()
  const count = (level: Level) => view.nodes.filter((n) => n.level === level).length
  const byCategory = new Map<Category, number>()
  for (const s of view.stories) {
    const c = statuses.get(s.statusId)?.category ?? 'todo'
    byCategory.set(c, (byCategory.get(c) ?? 0) + 1)
  }
  const facts = [
    `${view.map.levels}-level map`,
    ...(view.map.levels === 4 ? [plural(count('objective'), 'objective')] : []),
    plural(count('activity'), 'activity').replace('activitys', 'activities'),
    plural(count('step'), 'step'),
    plural(view.stories.length, 'story').replace('storys', 'stories'),
  ]
  return (
    <section className={`mhead ${panelOpen ? 'with-panel' : ''}`}>
      <div className="min-w-0">
        <Link to={`/w/${ws.slug}/maps`} className="label-up hover:underline">
          {ws.name} › Story maps
        </Link>
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
      </div>
      <div className="flex flex-col items-end gap-3" style={{ marginLeft: 'auto' }}>
        <Presence users={users} state={state} />
        <button type="button" className="btn btn-xs btn-ol" aria-pressed={props.activityOpen} onClick={props.toggleActivity}>
          Activity
        </button>
      </div>
      <div className="kpis" aria-label="Stories by status">
        {categoryOrder.map((c) => (
          <div className="kpi" key={c.category}>
            <span className={`ring-disc ${c.ring}`}>{byCategory.get(c.category) ?? 0}</span>
            {c.label}
          </div>
        ))}
      </div>
    </section>
  )
}

const levelLabel: Record<Level, string> = { objective: 'Objective', activity: 'Activity', step: 'Step' }

function MapGrid() {
  const { view, canEdit } = useMap()
  const bb = useMemo(() => backbone(view), [view])
  const bs = useMemo(() => bands(view), [view])
  const byCell = useMemo(() => cells(view), [view])
  const four = view.map.levels === 4
  const topLevel: Level = four ? 'objective' : 'activity'

  if (bb.columns.length === 0) {
    return (
      <div className="max-w-xl py-10">
        <p className="up mb-2 text-2xl">An empty map</p>
        <p className="text-muted mb-6 font-medium">
          {four
            ? 'Start the backbone with an objective, then add the activities and steps under it.'
            : 'Start the backbone with an activity, then add the steps under it.'}
        </p>
        {canEdit && <AddNode level={topLevel} parent={null} label={`Add ${topLevel}`} />}
      </div>
    )
  }

  // Columns: row labels, one per backbone column, and one for adding at the end.
  const style = { gridTemplateColumns: `132px repeat(${bb.columns.length}, 184px)${canEdit ? ' 150px' : ''}` }
  const col = (start: number, span = 1) => ({ gridColumn: `${start + 2} / span ${span}` })
  const trailing = { gridColumn: `${bb.columns.length + 2}` }

  return (
    <div className="map-grid" style={style}>
      {four && (
        <>
          <div className="map-lbl">Objective</div>
          {bb.objectives.map((o) => (
            <div key={o.node.id} className="map-obj up" style={col(o.start, o.span)}>
              <NodeHead node={o.node} child="activity" />
            </div>
          ))}
          {canEdit && (
            <div style={trailing}>
              <AddNode level="objective" parent={null} label="Add objective" />
            </div>
          )}
        </>
      )}

      <div className="map-lbl" style={{ gridColumn: 1 }}>
        Activity
      </div>
      {bb.activities.map((a) => (
        <div key={a.node.id} className="map-act" style={col(a.start, a.span)}>
          <NodeHead node={a.node} child="step" bold />
        </div>
      ))}
      {bb.columns.map((c, i) =>
        !c.activity && c.objective ? (
          <Slot key={c.key} style={col(i)} level="activity" parent={c.objective}>
            {canEdit && <AddNode level="activity" parent={c.objective} label={`Add activity to ${c.objective.title}`} short="Add activity" />}
          </Slot>
        ) : null,
      )}
      {canEdit && !four && (
        <div style={trailing}>
          <AddNode level="activity" parent={null} label="Add activity" />
        </div>
      )}

      <div className="map-lbl" style={{ gridColumn: 1 }}>
        Step
      </div>
      {bb.columns.map((c, i) =>
        c.step ? (
          <div key={c.key} style={col(i)} className="map-step">
            <NodeHead node={c.step} />
          </div>
        ) : c.activity ? (
          <Slot key={c.key} style={col(i)} level="step" parent={c.activity}>
            {canEdit && <AddNode level="step" parent={c.activity} label={`Add step to ${c.activity.title}`} short="Add step" />}
          </Slot>
        ) : (
          <div key={c.key} style={col(i)} />
        ),
      )}

      {bs.map((b, i) => (
        <BandRows key={b.key} band={b} index={i} columns={bb.columns} byCell={byCell} isLast={i === bs.length - 1} />
      ))}
    </div>
  )
}

/**
 * Title, rename and delete for a backbone node. child is the level below; its
 * add button shows once the node has children, since until then an empty
 * column below offers the first one.
 */
function NodeHead({ node, child, bold }: { node: MapNode; child?: Level; bold?: boolean }) {
  const { ws, view, canEdit, run } = useMap()
  const label = `${node.level} ${node.title}`
  const hasChildren = view.nodes.some((n) => n.parentId === node.id)
  const drag = useDrag(`node:${node.id}`, { kind: 'node', node, label }, !canEdit)
  const drop = useDrop(`node:${node.id}`, { kind: 'node', node, label })
  return (
    <div
      ref={refs(drag.ref, drop.ref)}
      className={`node-head ${drop.over ? 'drop' : ''} ${drag.dragging ? 'dragging' : ''}`}
      {...drag.pointer}
    >
      <InlineText
        value={node.title}
        label={label}
        editable={canEdit}
        className={bold ? 'up text-[15px]' : undefined}
        onSave={(title) => run(() => api.patchNode(ws.slug, view.map.id, node.id, { title }))}
      />
      {canEdit && (
        <span className="node-tools">
          {child && hasChildren && <AddNodeIcon level={child} parent={node} />}
          <button type="button" className="icon-btn drag-handle" title="Move" {...drag.handle}>
            <span>⠿</span>
          </button>
          <button
            type="button"
            className="icon-btn"
            aria-label={`Delete ${label}`}
            title="Delete"
            onClick={() =>
              run(() => api.deleteNode(ws.slug, view.map.id, node.id), {
                done: `Deleted ${node.level} “${node.title}”`,
                undo: () => api.restoreNode(ws.slug, view.map.id, node.id),
              })
            }
          >
            <span>✕</span>
          </button>
        </span>
      )}
    </div>
  )
}

/**
 * Adding backbone nodes. Which add input is open lives in the map context, not
 * in the button, because the button that opened it can unmount mid-entry: the
 * empty column that offers a first step becomes that step. The input then
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

function AddNodeIcon({ level, parent }: { level: Level; parent: MapNode }) {
  const add = useAddNode(level, parent)
  if (add.open) return <span className="node-popup">{add.input}</span>
  return (
    <button type="button" className="icon-btn" aria-label={`Add ${level} to ${parent.title}`} title={`Add ${level}`} onClick={() => add.setOpen(true)}>
      <span>+</span>
    </button>
  )
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

function BandRows(props: { band: Band; index: number; columns: Column[]; byCell: Map<string, Card[]>; isLast: boolean }) {
  const { canEdit } = useMap()
  const { band, columns, byCell } = props
  const stories = columns.reduce((n, c) => n + (c.step ? (byCell.get(cellKey(c.step.id, band.release?.id ?? null))?.length ?? 0) : 0), 0)
  const name = band.release?.name ?? 'Unscheduled'
  return (
    <>
      {props.isLast && canEdit && <AddRelease />}
      <BandHeader release={band.release} index={props.index} stories={stories} />
      <div className="map-lbl" style={{ gridColumn: 1 }} />
      {columns.map((c, i) =>
        c.step ? (
          <Cell
            key={c.key}
            column={i + 2}
            step={c.step}
            release={band.release}
            bandName={name}
            cards={byCell.get(cellKey(c.step.id, band.release?.id ?? null)) ?? []}
          />
        ) : (
          <div key={c.key} style={{ gridColumn: i + 2 }} />
        ),
      )}
    </>
  )
}

function formatDate(d: string): string {
  return new Date(`${d}T00:00:00`).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

function BandHeader({ release, index, stories }: { release: Release | null; index: number; stories: number }) {
  const { ws, view, canEdit, run } = useMap()
  const tone = release ? (index % 2 === 0 ? 'green' : 'amber') : ''
  const name = release?.name ?? 'Unscheduled'
  const label = `release ${name}`
  // Unscheduled stays last: a release dropped on it moves to the end of the releases.
  const placeholder = { id: 'unscheduled', name, description: '', targetDate: null, rank: '' }
  const drag = useDrag(`release:${release?.id ?? 'unscheduled'}`, { kind: 'release', release: release ?? placeholder, label }, !canEdit || !release)
  const drop = useDrop(
    `release:${release?.id ?? 'unscheduled'}`,
    release ? { kind: 'release', release, label } : { kind: 'unscheduled', label: 'the end of the releases' },
  )
  return (
    <div
      ref={refs(drag.ref, drop.ref)}
      className={`band-row ${tone} ${drop.over ? 'drop' : ''} ${drag.dragging ? 'dragging' : ''}`}
      role="heading"
      aria-level={2}
      aria-label={`Release ${name}`}
      {...(release ? drag.pointer : {})}
    >
      <span className="n">
        {release ? (
          <InlineText
            value={release.name}
            label={`release ${release.name}`}
            editable={canEdit}
            maxLength={100}
            onSave={(n) => run(() => api.patchRelease(ws.slug, view.map.id, release.id, { name: n }))}
          />
        ) : (
          name
        )}
      </span>
      {release && <TargetDate release={release} />}
      <span className="r">
        {plural(stories, 'story').replace('storys', 'stories')}
        {release && canEdit && (
          <button type="button" className="btn btn-xs btn-ol drag-handle" title="Move" {...drag.handle}>
            ⠿
          </button>
        )}
        {release && canEdit && (
          <button
            type="button"
            className="btn btn-xs btn-ol"
            aria-label={`Delete release ${release.name}`}
            onClick={() =>
              run(() => api.deleteRelease(ws.slug, view.map.id, release.id), {
                done: `Deleted release “${release.name}”. Its stories moved to Unscheduled.`,
              })
            }
          >
            Delete
          </button>
        )}
      </span>
    </div>
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

function Cell(props: { column: number; step: MapNode; release: Release | null; bandName: string; cards: Card[] }) {
  const { step, release, bandName, cards } = props
  const { ws, view, canEdit, run } = useMap()
  const [adding, setAdding] = useState(false)
  const where = `${step.title} / ${bandName}`
  const drop = useDrop(`cell:${cellKey(step.id, release?.id ?? null)}`, {
    kind: 'cell',
    stepId: step.id,
    releaseId: release?.id ?? null,
    label: `the end of ${where}`,
  })
  return (
    <div
      ref={drop.ref}
      role="group"
      aria-label={`Stories in ${where}`}
      className={`map-cell ${drop.over ? 'drop' : ''}`}
      style={{ gridColumn: props.column }}
    >
      {cards.map((c) => (
        <StoryCard key={c.story.id} card={c} />
      ))}
      {canEdit &&
        (adding ? (
          <TextInput
            label={`New story in ${where}`}
            placeholder="Story title"
            keepOpen
            onDone={async (title) => {
              if (!title) {
                setAdding(false)
                return
              }
              await run(() => api.createStory(ws.slug, view.map.id, { title, stepId: step.id, releaseId: release?.id ?? null }))
            }}
          />
        ) : (
          <button type="button" className="add-story" aria-label={`Add story to ${where}`} onClick={() => setAdding(true)}>
            + Story
          </button>
        ))}
    </div>
  )
}

export function StatusLabel({ status }: { status: Status | undefined }) {
  return <span className={`st ${status?.category ?? 'todo'}`}>{status?.name ?? 'No status'}</span>
}

function StoryCard({ card }: { card: Card }) {
  const { canEdit, statuses, selectedStory, openStory } = useMap()
  const { story, placement } = card
  const selected = selectedStory === story.id
  const label = `${story.key} ${story.title}`
  const drag = useDrag(`story:${story.id}`, { kind: 'story', storyId: story.id, placement, label }, !canEdit)
  const drop = useDrop(`story:${story.id}`, { kind: 'story', storyId: story.id, placement, label })
  return (
    <div
      ref={refs(drag.ref, drop.ref)}
      className={`story-card ${drop.over ? 'drop' : ''} ${drag.dragging ? 'dragging' : ''}`}
      {...drag.pointer}
    >
      <button
        type="button"
        className="card-open"
        aria-current={selected}
        aria-label={label}
        onClick={() => openStory(selected ? null : story.id)}
      >
        <span>{story.title}</span>
        <span className="meta">
          <StatusLabel status={statuses.get(story.statusId)} />
          <span className="text-muted ml-auto text-[11px] font-extrabold">{story.key}</span>
        </span>
      </button>
      {canEdit && (
        <button type="button" className="icon-btn drag-handle card-handle" title="Move" {...drag.handle}>
          <span>⠿</span>
        </button>
      )}
    </div>
  )
}

/** Everything that happened on the map, newest first. */
function ActivityPanel({ onClose }: { onClose: () => void }) {
  const { ws, view, openStory } = useMap()
  return (
    <aside className="story-panel" aria-label="Map activity">
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
        onStory={openStory}
      />
    </aside>
  )
}
