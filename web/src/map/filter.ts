import type { MapView } from '../api'

/**
 * Which steps to show. Within one field any value matches; across fields all
 * must. An empty field does not filter. In highlight mode steps that do not
 * match are dimmed; in hide mode they are left out.
 */
export interface Filter {
  labels: string[]
  statuses: string[]
  /** User ids; UNASSIGNED stands for steps with no assignee. */
  assignees: string[]
  /** Persona ids, matched through the step's objective. */
  personas: string[]
  /** Release ids; UNSCHEDULED stands for the Unscheduled band. */
  releases: string[]
  /** Matches the key or title, ignoring case. */
  text: string
  mode: 'highlight' | 'hide'
}

export const UNASSIGNED = 'none'
export const UNSCHEDULED = 'none'

export const emptyFilter: Filter = { labels: [], statuses: [], assignees: [], personas: [], releases: [], text: '', mode: 'highlight' }

type ListField = 'labels' | 'statuses' | 'assignees' | 'personas' | 'releases'
export const listFields: ListField[] = ['labels', 'statuses', 'assignees', 'personas', 'releases']

/** URL query names, kept short so a filtered map's link stays readable. */
const param: Record<ListField, string> = { labels: 'l', statuses: 's', assignees: 'a', personas: 'p', releases: 'r' }

export function isActive(f: Filter): boolean {
  return f.text.trim() !== '' || listFields.some((k) => f[k].length > 0)
}

/** Reads a filter from the map's URL. Unknown ids are kept; they match nothing. */
export function readFilter(search: URLSearchParams): Filter {
  const list = (k: ListField) => (search.get(param[k]) ?? '').split(',').filter(Boolean)
  return {
    labels: list('labels'),
    statuses: list('statuses'),
    assignees: list('assignees'),
    personas: list('personas'),
    releases: list('releases'),
    text: search.get('q') ?? '',
    mode: search.get('hide') === '1' ? 'hide' : 'highlight',
  }
}

/** Writes a filter into the URL's query, leaving other parameters alone. */
export function writeFilter(search: URLSearchParams, f: Filter): URLSearchParams {
  const n = new URLSearchParams(search)
  for (const k of listFields) {
    if (f[k].length) n.set(param[k], f[k].join(','))
    else n.delete(param[k])
  }
  if (f.text) n.set('q', f.text)
  else n.delete('q')
  if (f.mode === 'hide') n.set('hide', '1')
  else n.delete('hide')
  return n
}

/** The ids of the steps that match, or null when the filter is not active. */
export function matching(view: MapView, f: Filter): Set<string> | null {
  if (!isActive(f)) return null
  const nodes = new Map(view.nodes.map((n) => [n.id, n]))
  const placements = new Map(view.placements.map((p) => [p.stepId, p]))
  const text = f.text.trim().toLowerCase()
  const any = (want: string[], has: (v: string) => boolean) => want.length === 0 || want.some(has)
  const out = new Set<string>()
  for (const s of view.steps) {
    const p = placements.get(s.id)
    const objective = p && nodes.get(nodes.get(p.activityId)?.parentId ?? '')
    const ok =
      any(f.labels, (id) => s.labelIds.includes(id)) &&
      any(f.statuses, (id) => s.statusId === id) &&
      any(f.assignees, (id) => (id === UNASSIGNED ? s.assigneeId === null : s.assigneeId === id)) &&
      any(f.personas, (id) => !!objective?.personaIds.includes(id)) &&
      any(f.releases, (id) => (id === UNSCHEDULED ? p?.releaseId === null : p?.releaseId === id)) &&
      (text === '' || s.key.toLowerCase().includes(text) || s.title.toLowerCase().includes(text))
    if (ok) out.add(s.id)
  }
  return out
}
