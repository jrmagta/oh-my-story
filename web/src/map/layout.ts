import type { MapNode, MapView, Placement, Release, Story } from '../api'

/**
 * One grid column under the backbone. Each activity is a column; an objective
 * with no activities still takes one, which holds the button for its first.
 */
export interface Column {
  key: string
  activity: MapNode | null
  objective: MapNode
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

export function backbone(view: MapView): Backbone {
  const kids = childrenOf(view.nodes)
  const out: Backbone = { objectives: [], columns: [] }
  for (const obj of kids.get('') ?? []) {
    const start = out.columns.length
    const acts = kids.get(obj.id) ?? []
    if (acts.length === 0) out.columns.push({ key: `o:${obj.id}`, activity: null, objective: obj })
    for (const act of acts) out.columns.push({ key: act.id, activity: act, objective: obj })
    out.objectives.push({ node: obj, start, span: out.columns.length - start })
  }
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
  story: Story
  placement: Placement
}

export const cellKey = (activityId: string, releaseId: string | null) => `${activityId}|${releaseId ?? ''}`

/** Stories by cell, each cell in rank order. */
export function cells(view: MapView): Map<string, Card[]> {
  const stories = new Map(view.stories.map((s) => [s.id, s]))
  const out = new Map<string, Card[]>()
  for (const p of [...view.placements].sort(byRank)) {
    const story = stories.get(p.storyId)
    if (!story) continue
    const k = cellKey(p.activityId, p.releaseId)
    const list = out.get(k)
    if (list) list.push({ story, placement: p })
    else out.set(k, [{ story, placement: p }])
  }
  return out
}
