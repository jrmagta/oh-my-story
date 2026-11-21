import { useCallback, useState } from 'react'

/**
 * How one person looks at one map: card size, which activities and release
 * bands are collapsed, and the release in focus. Kept in this browser only,
 * per map; nothing else depends on it, so losing it costs nothing.
 */
export interface ViewPrefs {
  size: 'full' | 'compact' | 'titles'
  /** Activity ids. */
  collapsedActivities: string[]
  /** Release ids; UNSCHEDULED_BAND for the Unscheduled band. */
  collapsedReleases: string[]
  /** A release id (or UNSCHEDULED_BAND) to show alone, or null for all. */
  focus: string | null
}

export const UNSCHEDULED_BAND = 'unscheduled'

export const defaultPrefs: ViewPrefs = { size: 'full', collapsedActivities: [], collapsedReleases: [], focus: null }

const storageKey = (mapId: string) => `oms-view:${mapId}`

function load(mapId: string): ViewPrefs {
  try {
    const raw = localStorage.getItem(storageKey(mapId))
    if (!raw) return defaultPrefs
    const p = JSON.parse(raw) as Partial<ViewPrefs>
    return {
      size: p.size === 'compact' || p.size === 'titles' ? p.size : 'full',
      collapsedActivities: Array.isArray(p.collapsedActivities) ? p.collapsedActivities.filter((x) => typeof x === 'string') : [],
      collapsedReleases: Array.isArray(p.collapsedReleases) ? p.collapsedReleases.filter((x) => typeof x === 'string') : [],
      focus: typeof p.focus === 'string' ? p.focus : null,
    }
  } catch {
    return defaultPrefs
  }
}

/** The map's view settings and a setter that also saves them (when storage is available). */
export function useViewPrefs(mapId: string): [ViewPrefs, (p: ViewPrefs) => void] {
  const [prefs, setState] = useState(() => load(mapId))
  const set = useCallback(
    (p: ViewPrefs) => {
      setState(p)
      try {
        localStorage.setItem(storageKey(mapId), JSON.stringify(p))
      } catch {
        // Private windows and full storage keep the settings for this visit only.
      }
    },
    [mapId],
  )
  return [prefs, set]
}

/** A list with id added or removed. */
export function toggled(list: string[], id: string): string[] {
  return list.includes(id) ? list.filter((x) => x !== id) : [...list, id]
}
