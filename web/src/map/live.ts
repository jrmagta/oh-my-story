import { useEffect, useRef, useState } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import type { Dependency, Label, MapNode, MapView, Persona, Placement, Release, Step } from '../api'

/** One row of the event log, as the live socket sends it (docs/API.md, Live). */
export interface LiveEvent {
  id: number
  mapId: string | null
  actorId: string | null
  entity: string
  entityId: string
  action: string
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
}

export interface PresentUser {
  id: string
  name: string
}

/** A presence message leaves users out when nobody is on the map (the server omits an empty list). */
type Message = { type: 'event'; event: LiveEvent } | { type: 'presence'; users?: PresentUser[] } | { type: 'resync' }

function patch<T extends { id: string }>(list: T[], id: string, fields: Record<string, unknown> | null): T[] {
  return list.map((x) => (x.id === id ? { ...x, ...fields } : x))
}

function add<T extends { id: string }>(list: T[], item: T): T[] {
  return list.some((x) => x.id === item.id) ? list : [...list, item]
}

/**
 * The map after one event, or null when the event cannot be applied from its
 * own fields (a restore brings back rows the client no longer holds; a
 * workflow change can touch any status) and the map should be reloaded.
 */
export function applyEvent(view: MapView, ev: LiveEvent): MapView | null {
  const after = ev.after ?? {}
  switch (`${ev.entity}:${ev.action}`) {
    case 'map:update':
      return { ...view, map: { ...view.map, ...after } }
    case 'persona:create':
      return { ...view, personas: add(view.personas, after as unknown as Persona) }
    case 'persona:update':
      return { ...view, personas: patch(view.personas, ev.entityId, after) }
    case 'persona:delete':
      // Deleting a persona unlinks it from every objective, with no node events of its own.
      return {
        ...view,
        personas: view.personas.filter((p) => p.id !== ev.entityId),
        nodes: view.nodes.map((n) => (n.personaIds.includes(ev.entityId) ? { ...n, personaIds: n.personaIds.filter((id) => id !== ev.entityId) } : n)),
      }
    case 'label:create':
      return { ...view, labels: add(view.labels, after as unknown as Label) }
    case 'label:update':
      return { ...view, labels: patch(view.labels, ev.entityId, after) }
    case 'label:delete':
      // Deleting a label takes it off every step, with no step events of its own.
      return {
        ...view,
        labels: view.labels.filter((l) => l.id !== ev.entityId),
        steps: view.steps.map((s) => (s.labelIds.includes(ev.entityId) ? { ...s, labelIds: s.labelIds.filter((id) => id !== ev.entityId) } : s)),
      }
    case 'node:create':
      return { ...view, nodes: add(view.nodes, { version: 0, personaIds: [], ...after } as unknown as MapNode) }
    case 'node:update':
    case 'node:move':
      return { ...view, nodes: patch(view.nodes, ev.entityId, after) }
    case 'node:delete':
      return { ...view, nodes: view.nodes.filter((n) => n.id !== ev.entityId) }
    case 'release:create':
      return { ...view, releases: add(view.releases, after as unknown as Release) }
    case 'release:update':
    case 'release:move':
      return { ...view, releases: patch(view.releases, ev.entityId, after) }
    case 'release:delete':
      return { ...view, releases: view.releases.filter((r) => r.id !== ev.entityId) }
    case 'step:create':
      return { ...view, steps: add(view.steps, after as unknown as Step) }
    case 'step:update':
      return { ...view, steps: patch(view.steps, ev.entityId, after) }
    case 'comment:create':
    case 'comment:delete': {
      // A comment changes its step's or node's count; open comment lists reload (see apply).
      const fields = ev.action === 'create' ? after : (ev.before ?? {})
      const stepId = typeof fields.stepId === 'string' ? fields.stepId : null
      const nodeId = typeof fields.nodeId === 'string' ? fields.nodeId : null
      const by = ev.action === 'create' ? 1 : -1
      const count = <T extends { id: string; commentCount: number }>(x: T, id: string | null) =>
        x.id === id ? { ...x, commentCount: Math.max(0, x.commentCount + by) } : x
      return { ...view, steps: view.steps.map((s) => count(s, stepId)), nodes: view.nodes.map((n) => count(n, nodeId)) }
    }
    case 'comment:update':
      return view
    case 'attachment:create':
    case 'attachment:delete':
    case 'attachment:restore': {
      // Only the step's or node's count lives in the map; open file lists reload (see apply).
      const fields = ev.action === 'delete' ? (ev.before ?? {}) : after
      const stepId = typeof fields.stepId === 'string' ? fields.stepId : null
      const nodeId = typeof fields.nodeId === 'string' ? fields.nodeId : null
      const by = ev.action === 'delete' ? -1 : 1
      const count = <T extends { id: string; attachmentCount: number }>(x: T, id: string | null) =>
        x.id === id ? { ...x, attachmentCount: Math.max(0, x.attachmentCount + by) } : x
      return { ...view, steps: view.steps.map((s) => count(s, stepId)), nodes: view.nodes.map((n) => count(n, nodeId)) }
    }
    case 'step:delete':
      return { ...view, steps: view.steps.filter((s) => s.id !== ev.entityId) }
    case 'step:place': {
      // Shown on another map too: here (the step arrives; its placement follows) or elsewhere.
      const { mapId, mapName, ...step } = after as { mapId: string; mapName: string } & Record<string, unknown>
      if (mapId === view.map.id) return { ...view, steps: add(view.steps, step as unknown as Step) }
      if (view.elsewhere.some((e) => e.stepId === ev.entityId && e.mapId === mapId)) return view
      return { ...view, elsewhere: [...view.elsewhere, { stepId: ev.entityId, mapId, mapName }] }
    }
    case 'step:unplace': {
      const mapId = (ev.before ?? {}).mapId
      if (mapId === view.map.id) {
        return {
          ...view,
          steps: view.steps.filter((s) => s.id !== ev.entityId),
          placements: view.placements.filter((p) => p.stepId !== ev.entityId),
          elsewhere: view.elsewhere.filter((e) => e.stepId !== ev.entityId),
        }
      }
      return { ...view, elsewhere: view.elsewhere.filter((e) => !(e.stepId === ev.entityId && e.mapId === mapId)) }
    }
    case 'dependency:create': {
      // A link to a step this map has never seen needs its name: reload.
      const d = after as unknown as Dependency
      const known = (id: string) => view.steps.some((s) => s.id === id) || view.otherSteps.some((s) => s.id === id)
      if (!known(d.blockerId) || !known(d.blockedId)) return null
      return { ...view, dependencies: add(view.dependencies, { id: d.id, blockerId: d.blockerId, blockedId: d.blockedId }) }
    }
    case 'dependency:delete':
      return { ...view, dependencies: view.dependencies.filter((d) => d.id !== ev.entityId) }
    case 'stepref:update':
      return { ...view, otherSteps: patch(view.otherSteps, ev.entityId, { done: after.done }) }
    case 'placement:create':
      return { ...view, placements: add(view.placements, after as unknown as Placement) }
    case 'placement:move': {
      // A move also bumps its step's version, which travels as stepVersion.
      const { stepVersion, ...fields } = after
      const placements = patch(view.placements, ev.entityId, fields)
      const stepId = placements.find((p) => p.id === ev.entityId)?.stepId
      const steps =
        typeof stepVersion === 'number' && stepId ? patch(view.steps, stepId, { version: stepVersion }) : view.steps
      return { ...view, placements, steps }
    }
    default:
      return null
  }
}

/** Applies events in id order, skipping any the map already includes. */
export function applyEvents(view: MapView, evs: LiveEvent[]): MapView | null {
  let v: MapView | null = view
  for (const ev of evs) {
    if (!v) return null
    if (ev.id <= v.lastEventId) continue
    v = applyEvent(v, ev)
    if (v) v = { ...v, lastEventId: ev.id }
  }
  return v
}

export type LiveState = 'connecting' | 'live' | 'offline'

export const liveLabel: Record<LiveState, string> = {
  connecting: 'Connecting to live updates…',
  live: 'Live: changes by others appear as they happen',
  offline: 'Offline: reconnecting…',
}

const keep = 500

/**
 * Keeps the cached map current from the live socket, and reports who else is
 * on the map. Reconnects with backoff, resuming after the newest event the
 * cache holds. A reload of the map can land with a snapshot older than events
 * already applied, so recent events are kept and re-applied on top of it.
 */
export function useLiveMap(slug: string, mapId: string, key: QueryKey): { users: PresentUser[]; state: LiveState } {
  const qc = useQueryClient()
  const [users, setUsers] = useState<PresentUser[]>([])
  const [state, setState] = useState<LiveState>('connecting')
  const recent = useRef<LiveEvent[]>([])

  useEffect(() => {
    let socket: WebSocket | null = null
    let retry: ReturnType<typeof setTimeout> | undefined
    let attempts = 0
    let stopped = false

    const reload = () => void qc.invalidateQueries({ queryKey: key })

    const apply = (evs: LiveEvent[]) => {
      const view = qc.getQueryData<MapView>(key)
      if (!view) return
      const next = applyEvents(view, evs)
      if (next) qc.setQueryData(key, next)
      else reload()
      // History and comment lists live under the map's key; refresh them for the new entries.
      void qc.invalidateQueries({ queryKey: [...key, 'history'] })
      if (evs.some((e) => e.entity === 'comment')) void qc.invalidateQueries({ queryKey: [...key, 'comments'] })
      if (evs.some((e) => e.entity === 'attachment')) void qc.invalidateQueries({ queryKey: [...key, 'attachments'] })
    }

    // When a fetched snapshot is older than events already applied, re-apply them.
    const unsubscribe = qc.getQueryCache().subscribe((e) => {
      if (e.type !== 'updated' || e.action.type !== 'success') return
      if (JSON.stringify(e.query.queryKey) !== JSON.stringify(key)) return
      const view = e.query.state.data as MapView | undefined
      const newer = recent.current.filter((ev) => view && ev.id > view.lastEventId)
      if (view && newer.length > 0) {
        const next = applyEvents(view, newer)
        if (next) qc.setQueryData(key, next)
        else reload()
      }
    })

    const connect = () => {
      // Wait for the map, and for any reload to land, so the socket resumes
      // from the newest snapshot rather than replaying what it replaces.
      const view = qc.getQueryData<MapView>(key)
      if (stopped) return
      if (!view || qc.isFetching({ queryKey: key }) > 0) {
        retry = setTimeout(connect, 300)
        return
      }
      setState('connecting')
      const scheme = location.protocol === 'https:' ? 'wss' : 'ws'
      socket = new WebSocket(
        `${scheme}://${location.host}/api/workspaces/${encodeURIComponent(slug)}/maps/${encodeURIComponent(mapId)}/live?after=${view.lastEventId}`,
      )
      socket.onopen = () => {
        attempts = 0
        setState('live')
      }
      socket.onmessage = (m) => {
        const msg = JSON.parse(m.data as string) as Message
        if (msg.type === 'event') {
          recent.current = [...recent.current.slice(-keep + 1), msg.event]
          apply([msg.event])
        } else if (msg.type === 'presence') {
          setUsers(msg.users ?? [])
        } else {
          reload()
        }
      }
      socket.onclose = () => {
        socket = null
        if (stopped) return
        setState('offline')
        setUsers([])
        // 0.5 s, 1 s, 2 s … up to 15 s, so a restart of the server is picked up soon.
        const delay = Math.min(15_000, 500 * 2 ** attempts++)
        retry = setTimeout(connect, delay)
      }
    }
    connect()
    return () => {
      stopped = true
      clearTimeout(retry)
      unsubscribe()
      socket?.close()
    }
  }, [qc, slug, mapId, key])

  return { users, state }
}
