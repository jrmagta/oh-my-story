import { useEffect, useRef, useState } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import type { MapNode, MapView, Placement, Release, Story } from '../api'

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

type Message = { type: 'event'; event: LiveEvent } | { type: 'presence'; users: PresentUser[] } | { type: 'resync' }

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
    case 'node:create':
      return { ...view, nodes: add(view.nodes, { version: 0, ...after } as unknown as MapNode) }
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
    case 'story:create':
      return { ...view, stories: add(view.stories, after as unknown as Story) }
    case 'story:update':
      return { ...view, stories: patch(view.stories, ev.entityId, after) }
    case 'story:delete':
      return { ...view, stories: view.stories.filter((s) => s.id !== ev.entityId) }
    case 'placement:create':
      return { ...view, placements: add(view.placements, after as unknown as Placement) }
    case 'placement:move': {
      // A move also bumps its story's version, which travels as storyVersion.
      const { storyVersion, ...fields } = after
      const placements = patch(view.placements, ev.entityId, fields)
      const storyId = placements.find((p) => p.id === ev.entityId)?.storyId
      const stories =
        typeof storyVersion === 'number' && storyId ? patch(view.stories, storyId, { version: storyVersion }) : view.stories
      return { ...view, placements, stories }
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
          setUsers(msg.users)
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
