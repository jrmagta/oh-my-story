import type { MapNode, MapView, Placement, Release, Step } from '../api'

/**
 * One grid column under the backbone. Each activity is a column; an objective
 * with no activities still takes one, which holds the button for its first.
 */
export interface Column {
  key: string
  activity: MapNode | null
  /** null only for the column of an objective being typed in (entry 'objective'). */
  objective: MapNode | null
  /** The column holding the name field of an objective or activity being added here. */
  entry?: MapNode['level']
}

/** A node being added: its level, its parent, and where among its siblings (the end when neither id is set). */
export interface Adding {
  level: MapNode['level']
  parentId: string | null
  afterId?: string
  beforeId?: string
}

/** Where a new sibling goes among siblings (in rank order) for an Adding. */
function insertAt(siblings: MapNode[], a: Adding): number {
  if (a.beforeId) {
    const i = siblings.findIndex((n) => n.id === a.beforeId)
    if (i >= 0) return i
  }
  if (a.afterId) {
    const i = siblings.findIndex((n) => n.id === a.afterId)
    if (i >= 0) return i + 1
  }
  return siblings.length
}

/** A backbone node drawn across `span` columns starting at column `start` (0-based). */
export interface Span {
  node: MapNode
  start: number
  span: number
}

export interface Backbone {
  objectives: Span[]
  columns: Column[]
}

const byRank = (a: { rank: string; id: string }, b: { rank: string; id: string }) =>
  a.rank < b.rank ? -1 : a.rank > b.rank ? 1 : a.id < b.id ? -1 : 1

/** Children of each parent id ('' for the top level), in rank order. */
function childrenOf(nodes: MapNode[]): Map<string, MapNode[]> {
  const out = new Map<string, MapNode[]>()
  for (const n of nodes) {
    const k = n.parentId ?? ''
    const list = out.get(k)
    if (list) list.push(n)
    else out.set(k, [n])
  }
  for (const list of out.values()) list.sort(byRank)
  return out
}

/**
 * The backbone's columns: each objective's activities in order, and an empty
 * column for an objective with none. adding (a node being typed) gets a column
 * of its own at its place: an activity's within its objective, an objective's
 * between objectives. An objective's add column keeps one key (o:id) whether
 * it is empty or holding the field, so the field stays mounted and focused as
 * new activities appear just before it.
 */
export function backbone(view: MapView, adding: Adding | null = null): Backbone {
  const kids = childrenOf(view.nodes)
  const out: Backbone = { objectives: [], columns: [] }
  const objs = kids.get('') ?? []
  const newObjectiveAt = adding?.level === 'objective' ? insertAt(objs, adding) : -1
  const newObjective = () => out.columns.push({ key: 'new:objective', activity: null, objective: null, entry: 'objective' })
  objs.forEach((obj, i) => {
    if (i === newObjectiveAt) newObjective()
    const start = out.columns.length
    const acts = kids.get(obj.id) ?? []
    const at = adding?.level === 'activity' && adding.parentId === obj.id ? insertAt(acts, adding) : -1
    acts.forEach((act, j) => {
      if (j === at) out.columns.push({ key: `o:${obj.id}`, activity: null, objective: obj, entry: 'activity' })
      out.columns.push({ key: act.id, activity: act, objective: obj })
    })
    if (at === acts.length) out.columns.push({ key: `o:${obj.id}`, activity: null, objective: obj, entry: 'activity' })
    else if (acts.length === 0) out.columns.push({ key: `o:${obj.id}`, activity: null, objective: obj })
    out.objectives.push({ node: obj, start, span: out.columns.length - start })
  })
  if (newObjectiveAt === objs.length) newObjective()
  return out
}

/** A release band; `release` is null for Unscheduled, which always comes last. */
export interface Band {
  key: string
  release: Release | null
}

export function bands(view: MapView): Band[] {
  return [
    ...[...view.releases].sort(byRank).map((r) => ({ key: r.id, release: r })),
    { key: 'unscheduled', release: null },
  ]
}

export interface Card {
  step: Step
  placement: Placement
}

export const cellKey = (activityId: string, releaseId: string | null) => `${activityId}|${releaseId ?? ''}`

/** Steps by cell, each cell in rank order. */
export function cells(view: MapView): Map<string, Card[]> {
  const steps = new Map(view.steps.map((s) => [s.id, s]))
  const out = new Map<string, Card[]>()
  for (const p of [...view.placements].sort(byRank)) {
    const step = steps.get(p.stepId)
    if (!step) continue
    const k = cellKey(p.activityId, p.releaseId)
    const list = out.get(k)
    if (list) list.push({ step, placement: p })
    else out.set(k, [{ step, placement: p }])
  }
  return out
}

/**
 * The colour a new node starts with: its left neighbour's where it is being
 * inserted. An objective takes the objective before it; an activity, the
 * activity before it in its objective, or for an objective's first activity,
 * the last activity of the objectives before it.
 */
export function inheritedColour(view: MapView, a: Adding): string | null {
  const kids = childrenOf(view.nodes)
  const objs = kids.get('') ?? []
  if (a.level === 'objective') return objs[insertAt(objs, a) - 1]?.colour ?? null
  const own = kids.get(a.parentId ?? '') ?? []
  const at = insertAt(own, a)
  if (at > 0) return own[at - 1]!.colour
  const before = objs.slice(0, Math.max(0, objs.findIndex((o) => o.id === a.parentId))).reverse()
  for (const o of before) {
    const acts = kids.get(o.id) ?? []
    if (acts.length) return acts[acts.length - 1]!.colour
  }
  return null
}
