import type { MapNode, MapView, Placement, Position, Release } from '../api'

/** What is being dragged. */
export type DragItem =
  | { kind: 'step'; stepId: string; placement: Placement; label: string }
  | { kind: 'node'; node: MapNode; label: string }
  | { kind: 'release'; release: Release; label: string }

/** Where it can land. A step, node or release target means "next to this one". */
export type DropTarget =
  | { kind: 'step'; stepId: string; placement: Placement; label: string }
  | { kind: 'cell'; activityId: string; releaseId: string | null; label: string }
  | { kind: 'node'; node: MapNode; label: string }
  | { kind: 'slot'; level: MapNode['level']; parentId: string; label: string }
  | { kind: 'release'; release: Release; label: string }
  | { kind: 'unscheduled'; label: string }

/** Whether the target is the dragged item itself. */
export function isSelf(target: DropTarget, item: DragItem | undefined): boolean {
  if (!item) return false
  return (
    (item.kind === 'step' && target.kind === 'step' && target.stepId === item.stepId) ||
    (item.kind === 'node' && target.kind === 'node' && target.node.id === item.node.id) ||
    (item.kind === 'release' && target.kind === 'release' && target.release.id === item.release.id)
  )
}

export function accepts(target: DropTarget, item: DragItem | undefined): boolean {
  if (!item) return false
  switch (item.kind) {
    case 'step':
      return (target.kind === 'step' && target.stepId !== item.stepId) || target.kind === 'cell'
    case 'node':
      return (
        (target.kind === 'node' && target.node.level === item.node.level && target.node.id !== item.node.id) ||
        (target.kind === 'slot' && target.level === item.node.level)
      )
    case 'release':
      return (target.kind === 'release' && target.release.id !== item.release.id) || target.kind === 'unscheduled'
  }
}

/** A move, as the API takes it. */
export type Move =
  | { kind: 'step'; stepId: string; activityId: string; releaseId: string | null; position: Position }
  | { kind: 'node'; nodeId: string; parentId: string | null; position: Position }
  | { kind: 'release'; releaseId: string; position: Position }

const byRank = (a: { rank: string; id: string }, b: { rank: string; id: string }) =>
  a.rank < b.rank ? -1 : a.rank > b.rank ? 1 : a.id < b.id ? -1 : 1

/**
 * Same list: moving down lands after the target, moving up before it, like
 * reordering a list. Different list: before the target.
 */
function nextTo<T extends { id: string; rank: string }>(list: T[], moving: T, target: T, anchor: (t: T) => string): Position {
  const sorted = [...list].sort(byRank)
  const from = sorted.findIndex((x) => x.id === moving.id)
  const to = sorted.findIndex((x) => x.id === target.id)
  if (from >= 0 && from < to) return { afterId: anchor(target) }
  return { beforeId: anchor(target) }
}

/** The move a drop asks for, or null when it would change nothing the user meant. */
export function moveFor(view: MapView, item: DragItem, target: DropTarget): Move | null {
  if (!accepts(target, item)) return null
  if (item.kind === 'step') {
    if (target.kind === 'cell') {
      return { kind: 'step', stepId: item.stepId, activityId: target.activityId, releaseId: target.releaseId, position: {} }
    }
    if (target.kind !== 'step') return null
    const t = target.placement
    const cell = view.placements.filter((p) => p.activityId === t.activityId && p.releaseId === t.releaseId)
    return {
      kind: 'step',
      stepId: item.stepId,
      activityId: t.activityId,
      releaseId: t.releaseId,
      position: nextTo(cell, item.placement, t, (p) => p.stepId),
    }
  }
  if (item.kind === 'node') {
    if (target.kind === 'slot') return { kind: 'node', nodeId: item.node.id, parentId: target.parentId, position: {} }
    if (target.kind !== 'node') return null
    const siblings = view.nodes.filter((n) => n.parentId === target.node.parentId)
    return {
      kind: 'node',
      nodeId: item.node.id,
      parentId: target.node.parentId,
      position: nextTo(siblings, item.node, target.node, (n) => n.id),
    }
  }
  if (target.kind === 'unscheduled') return { kind: 'release', releaseId: item.release.id, position: {} }
  if (target.kind !== 'release') return null
  return { kind: 'release', releaseId: item.release.id, position: nextTo(view.releases, item.release, target.release, (r) => r.id) }
}

/**
 * A sort key for showing a move before the server answers. Ranks are only
 * computed on the server; this one just has to sort between the neighbours
 * until the reply replaces it. Server keys are ASCII, so appending U+0000 to a
 * key gives one that sorts right after it and before anything that followed it.
 */
function interimRank<T extends { id: string; rank: string }>(siblings: T[], position: Position, anchor: (t: T) => string): string {
  const sorted = [...siblings].sort(byRank)
  if (position.afterId) {
    const a = sorted.find((s) => anchor(s) === position.afterId)
    return a ? a.rank + '\u0000' : 'a0'
  }
  if (position.beforeId) {
    const i = sorted.findIndex((s) => anchor(s) === position.beforeId)
    const prev = i > 0 ? sorted[i - 1] : undefined
    return prev ? prev.rank + '\u0000' : '\u0000'
  }
  const last = sorted[sorted.length - 1]
  return last ? last.rank + '\u0000' : 'a0'
}

/** The map as it will look once the move is saved. */
export function applyMove(view: MapView, m: Move): MapView {
  switch (m.kind) {
    case 'step': {
      const moving = view.placements.find((p) => p.stepId === m.stepId)
      if (!moving) return view
      const cell = view.placements.filter((p) => p.activityId === m.activityId && p.releaseId === m.releaseId && p.id !== moving.id)
      const rank = interimRank(cell, m.position, (p) => p.stepId)
      return {
        ...view,
        placements: view.placements.map((p) => (p.id === moving.id ? { ...p, activityId: m.activityId, releaseId: m.releaseId, rank } : p)),
      }
    }
    case 'node': {
      const siblings = view.nodes.filter((n) => n.parentId === m.parentId && n.id !== m.nodeId)
      const rank = interimRank(siblings, m.position, (n) => n.id)
      return { ...view, nodes: view.nodes.map((n) => (n.id === m.nodeId ? { ...n, parentId: m.parentId, rank } : n)) }
    }
    case 'release': {
      const rank = interimRank(
        view.releases.filter((r) => r.id !== m.releaseId),
        m.position,
        (r) => r.id,
      )
      return { ...view, releases: view.releases.map((r) => (r.id === m.releaseId ? { ...r, rank } : r)) }
    }
  }
}
