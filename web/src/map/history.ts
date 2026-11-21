import type { Member, MapView } from '../api'

/** One history entry, as GET .../history returns it (docs/API.md, History). */
export interface HistoryEntry {
  id: number
  entity: string
  entityId: string
  action: string
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  actorId: string | null
  actorName: string
  at: string
}

export interface HistoryPage {
  entries: HistoryEntry[]
  next: number | null
}

/** What the sentences can look up: the map as shown, and the workspace's people. */
export interface Names {
  view: MapView
  members: Member[]
}

const q = (s: unknown) => `“${String(s)}”`

function stepName(n: Names, id: string, fields?: Record<string, unknown> | null): string {
  const key = fields?.key ?? n.view.steps.find((s) => s.id === id)?.key
  return typeof key === 'string' ? key : 'a step'
}

function status(n: Names, id: unknown): string {
  return n.view.workflow.statuses.find((s) => s.id === id)?.name ?? 'a removed status'
}

function activity(n: Names, id: unknown): string {
  const s = n.view.nodes.find((x) => x.id === id)
  return s ? q(s.title) : 'a deleted activity'
}

function release(n: Names, id: unknown): string {
  if (id === null) return 'Unscheduled'
  return n.view.releases.find((r) => r.id === id)?.name ?? 'a deleted release'
}

const ids = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])

function labelNames(n: Names, labelIds: string[]): string {
  return labelIds.map((id) => q(n.view.labels.find((l) => l.id === id)?.name ?? 'a deleted label')).join(', ')
}

function personaNames(n: Names, personaIds: string[]): string {
  return personaIds.map((id) => q(n.view.personas.find((p) => p.id === id)?.name ?? 'a deleted persona')).join(', ')
}

function person(n: Names, id: unknown): string {
  return n.members.find((m) => m.userId === id)?.displayName ?? 'a former member'
}

function nodeLabel(n: Names, id: string, fields?: Record<string, unknown> | null): string {
  const node = n.view.nodes.find((x) => x.id === id)
  const level = fields?.level ?? node?.level ?? 'item'
  const title = fields?.title ?? node?.title
  return title === undefined ? `a ${String(level)}` : `${String(level)} ${q(title)}`
}

/**
 * What an entry did, as the rest of a sentence that starts with the actor's
 * name: "set ACME-4 to In progress". Changes the reader cannot act on (a
 * placement created with its step, a reorder's rank) are folded in or left
 * out; returns null for entries that add nothing on their own.
 */
export function describe(e: HistoryEntry, n: Names): string | null {
  const b = e.before ?? {}
  const a = e.after ?? {}
  const changed = (k: string) => k in a
  switch (`${e.entity}:${e.action}`) {
    case 'step:create':
      return `added ${stepName(n, e.entityId, a)} ${q(a.title)}`
    case 'step:update': {
      const key = stepName(n, e.entityId)
      const parts: string[] = []
      if (changed('title')) parts.push(`renamed ${key} from ${q(b.title)} to ${q(a.title)}`)
      if (changed('statusId')) parts.push(`set ${key} to ${status(n, a.statusId)}`)
      if (changed('assigneeId')) parts.push(a.assigneeId ? `assigned ${key} to ${person(n, a.assigneeId)}` : `unassigned ${key}`)
      if (changed('estimate')) parts.push(a.estimate === null ? `cleared the estimate of ${key}` : `estimated ${key} at ${String(a.estimate)}`)
      if (changed('descriptionMd')) parts.push(`edited the description of ${key}`)
      if (changed('labelIds')) {
        const was = ids(b.labelIds)
        const now = ids(a.labelIds)
        const added = now.filter((id) => !was.includes(id))
        const removed = was.filter((id) => !now.includes(id))
        if (added.length) parts.push(`labelled ${key} ${labelNames(n, added)}`)
        if (removed.length) parts.push(`removed ${labelNames(n, removed)} from ${key}`)
      }
      return parts.length ? parts.join(', ') : null
    }
    case 'comment:create': {
      const key = stepName(n, typeof a.stepId === 'string' ? a.stepId : '')
      return a.parentId ? `replied to a comment on ${key}` : `commented on ${key}`
    }
    case 'comment:update': {
      const key = stepName(n, typeof a.stepId === 'string' ? a.stepId : '')
      if (changed('resolved')) return a.resolved ? `resolved a comment on ${key}` : `reopened a comment on ${key}`
      return changed('bodyMd') ? `edited a comment on ${key}` : null
    }
    case 'comment:delete':
      return `deleted a comment on ${stepName(n, typeof b.stepId === 'string' ? b.stepId : '')}`
    case 'step:delete':
      return `deleted ${stepName(n, e.entityId, b)} ${b.title !== undefined ? q(b.title) : ''}`.trim()
    case 'step:restore':
      return `restored ${stepName(n, e.entityId, a)}`
    case 'step:place':
      return `put ${stepName(n, e.entityId, a)} on ${q(a.mapName)}`
    case 'step:unplace':
      return `took ${stepName(n, e.entityId, b)} off ${q(b.mapName)}`
    case 'dependency:create':
      return `linked ${String(a.blockerKey)} as blocking ${String(a.blockedKey)}`
    case 'dependency:delete':
      return `unlinked ${String(b.blockerKey)} from blocking ${String(b.blockedKey)}`
    case 'stepref:update':
      // Told this map a linked step on another map changed; its own map's history says how.
      return null
    case 'placement:create':
      return null
    case 'placement:move': {
      const p = n.view.placements.find((x) => x.id === e.entityId)
      const key = p ? stepName(n, p.stepId) : 'a step'
      if (changed('activityId') || changed('releaseId')) {
        const activityId = changed('activityId') ? a.activityId : p?.activityId
        const releaseId = changed('releaseId') ? a.releaseId : (p?.releaseId ?? null)
        return `moved ${key} to ${activity(n, activityId)} in ${release(n, releaseId)}`
      }
      return `reordered ${key}`
    }
    case 'node:create':
      return `added ${nodeLabel(n, e.entityId, a)}`
    case 'node:update': {
      const parts: string[] = []
      if (changed('title')) parts.push(`renamed ${nodeLabel(n, e.entityId, { title: b.title })} to ${q(a.title)}`)
      if (changed('description')) parts.push(`edited the description of ${nodeLabel(n, e.entityId)}`)
      if (changed('colour')) parts.push(`recoloured ${nodeLabel(n, e.entityId)}`)
      if (changed('personaIds')) {
        const was = ids(b.personaIds)
        const now = ids(a.personaIds)
        const added = now.filter((id) => !was.includes(id))
        const removed = was.filter((id) => !now.includes(id))
        if (added.length) parts.push(`linked ${personaNames(n, added)} to ${nodeLabel(n, e.entityId)}`)
        if (removed.length) parts.push(`unlinked ${personaNames(n, removed)} from ${nodeLabel(n, e.entityId)}`)
      }
      return parts.length ? parts.join(', ') : null
    }
    case 'node:move':
      if (changed('parentId')) {
        const parent = n.view.nodes.find((x) => x.id === a.parentId)
        return `moved ${nodeLabel(n, e.entityId)} to ${parent ? `${parent.level} ${q(parent.title)}` : 'the top level'}`
      }
      return `reordered ${nodeLabel(n, e.entityId)}`
    case 'node:delete':
      return `deleted ${nodeLabel(n, e.entityId, b)}`
    case 'node:restore':
      return `restored ${nodeLabel(n, e.entityId, a)}`
    case 'release:create':
      return `added release ${q(a.name)}`
    case 'release:update': {
      const name = n.view.releases.find((r) => r.id === e.entityId)?.name ?? b.name
      const parts: string[] = []
      if (changed('name')) parts.push(`renamed release ${q(b.name)} to ${q(a.name)}`)
      if (changed('targetDate')) parts.push(a.targetDate ? `set the date of ${q(name)} to ${String(a.targetDate)}` : `cleared the date of ${q(name)}`)
      if (changed('description')) parts.push(`edited the description of ${q(name)}`)
      return parts.length ? parts.join(', ') : null
    }
    case 'release:move':
      return `reordered release ${q(n.view.releases.find((r) => r.id === e.entityId)?.name ?? '')}`
    case 'release:delete':
      return `deleted release ${q(b.name)}`
    case 'map:create':
      return 'created the map'
    case 'map:update': {
      const parts: string[] = []
      if (changed('name')) parts.push(`renamed the map from ${q(b.name)} to ${q(a.name)}`)
      if (changed('description')) parts.push('edited the map description')
      return parts.length ? parts.join(', ') : null
    }
    default:
      return null
  }
}

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
]

/** "3 minutes ago", "just now". */
export function ago(at: string, now = Date.now()): string {
  const secs = Math.round((now - new Date(at).getTime()) / 1000)
  if (secs < 45) return 'just now'
  const fmt = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  for (const [unit, size] of units) {
    if (secs >= size) return fmt.format(-Math.floor(secs / size), unit)
  }
  return fmt.format(-1, 'minute')
}
