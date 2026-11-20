import type { Category, MapView, Status } from '../api'

/**
 * What a group of stories adds up to. Sums are kept in hundredths, the
 * precision the server stores, so adding never drifts (0.1 + 0.2).
 */
export interface Total {
  stories: number
  hundredths: number
  unestimated: number
  byCategory: Record<Category, number>
}

const empty = (): Total => ({ stories: 0, hundredths: 0, unestimated: 0, byCategory: { todo: 0, doing: 0, done: 0, blocked: 0 } })

export interface Totals {
  map: Total
  /** Per backbone node: an activity's own stories; an objective's, everything under it. */
  nodes: Map<string, Total>
  /** Per release id; Unscheduled is ''. */
  releases: Map<string, Total>
}

/** Adds up the stories on the map. Stories whose activity is not on the map are left out. */
export function totals(view: MapView, statuses: Map<string, Status>): Totals {
  const parent = new Map(view.nodes.map((n) => [n.id, n.parentId]))
  const stories = new Map(view.stories.map((s) => [s.id, s]))
  const out: Totals = { map: empty(), nodes: new Map(), releases: new Map() }
  const add = (m: Map<string, Total>, k: string) => {
    let t = m.get(k)
    if (!t) m.set(k, (t = empty()))
    return t
  }
  for (const p of view.placements) {
    const s = stories.get(p.storyId)
    if (!s || !parent.has(p.activityId)) continue
    const targets = [out.map, add(out.releases, p.releaseId ?? '')]
    for (let id: string | null | undefined = p.activityId; id; id = parent.get(id)) targets.push(add(out.nodes, id))
    const category = statuses.get(s.statusId)?.category ?? 'todo'
    for (const t of targets) {
      t.stories++
      t.byCategory[category]++
      if (s.estimate === null) t.unestimated++
      else t.hundredths += Math.round(s.estimate * 100)
    }
  }
  return out
}

/** 2.5, 3, 0.25: no trailing zeros. */
export function formatEstimate(n: number): string {
  return String(Math.round(n * 100) / 100)
}

/** "8 points", "1 day"; a unit ending in s loses it for one. */
export function amount(hundredths: number, unit: string): string {
  const n = hundredths / 100
  return `${formatEstimate(n)} ${n === 1 && unit.endsWith('s') ? unit.slice(0, -1) : unit}`
}
