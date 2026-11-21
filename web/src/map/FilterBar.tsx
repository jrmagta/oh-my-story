import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { useMap } from '../pages/map'
import { SavedViews } from './SavedViews'
import { UNSCHEDULED_BAND, type ViewPrefs } from './viewPrefs'
import { emptyFilter, isActive, listFields, UNASSIGNED, UNSCHEDULED, type Filter } from './filter'

type Field = (typeof listFields)[number]
interface Option {
  id: string
  name: string
  colour?: string | null
}

const fieldName: Record<Field, string> = {
  statuses: 'Status',
  labels: 'Label',
  assignees: 'Assignee',
  personas: 'Persona',
  releases: 'Release',
}

/**
 * The map's filter: text over key and title, a menu of statuses, labels,
 * assignees, personas and releases, a chip per chosen value, and whether
 * steps that do not match are dimmed or hidden. It all lives in the URL.
 */
export function FilterBar({ panelOpen }: { panelOpen: boolean }) {
  const { ws, view, filter, setFilter, match, prefs, setPrefs, shared } = useMap()
  const focusName =
    prefs.focus === UNSCHEDULED_BAND ? 'Unscheduled' : view.releases.find((r) => r.id === prefs.focus)?.name
  const members = useQuery({ queryKey: ['ws', ws.slug, 'members'], queryFn: () => api.members(ws.slug), enabled: !shared })
  const [open, setOpen] = useState(false)

  const options: Record<Field, Option[]> = {
    statuses: view.workflow.statuses,
    labels: [...view.labels].sort((a, b) => a.name.localeCompare(b.name)),
    // A share link shows no people, so there is nothing to filter by.
    assignees: shared
      ? []
      : [{ id: UNASSIGNED, name: 'Unassigned' }, ...(members.data ?? []).map((m) => ({ id: m.userId, name: m.displayName }))],
    personas: [...view.personas].sort((a, b) => a.name.localeCompare(b.name)),
    releases: [
      ...[...view.releases].sort((a, b) => (a.rank < b.rank ? -1 : 1)),
      { id: UNSCHEDULED, name: 'Unscheduled' },
    ],
  }
  const nameOf = (k: Field, id: string) => options[k].find((o) => o.id === id)?.name ?? 'unknown'
  const toggle = (k: Field, id: string) =>
    setFilter({ ...filter, [k]: filter[k].includes(id) ? filter[k].filter((x) => x !== id) : [...filter[k], id] })
  const set = (f: Partial<Filter>) => setFilter({ ...filter, ...f })
  const active = isActive(filter)
  const chosen = listFields.reduce((n, k) => n + filter[k].length, 0)

  return (
    <section className={`filter-bar ${panelOpen ? 'with-panel' : ''}`} aria-label="Filter">
      {!shared && <SavedViews />}
      <input
        type="search"
        className="input input-sm w-52"
        aria-label="Filter steps by key or title"
        placeholder="Filter steps by key or title"
        value={filter.text}
        onChange={(e) => set({ text: e.target.value })}
      />
      <span
        className="relative"
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false)
        }}
        onBlur={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false)
        }}
      >
        <button type="button" className="btn btn-sm btn-ol" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
          Filter{chosen > 0 && ` (${chosen})`}
        </button>
        {open && (
          <div className="filter-pop" role="group" aria-label="Filter by">
            {listFields
              .filter((k) => options[k].length > 0)
              .map((k) => (
                <fieldset key={k}>
                  <legend className="label-up">{fieldName[k]}</legend>
                  {options[k].map((o) => (
                    <label key={o.id}>
                      <input
                        type="checkbox"
                        className="filter-check"
                        checked={filter[k].includes(o.id)}
                        onChange={() => toggle(k, o.id)}
                      />
                      {o.colour && <i style={{ background: o.colour }} aria-hidden />}
                      {o.name}
                    </label>
                  ))}
                </fieldset>
              ))}
          </div>
        )}
      </span>
      {listFields.flatMap((k) =>
        filter[k].map((id) => (
          <span key={`${k}:${id}`} className="filter-chip">
            {fieldName[k]}: {nameOf(k, id)}
            <button type="button" aria-label={`Remove filter ${fieldName[k]}: ${nameOf(k, id)}`} onClick={() => toggle(k, id)}>
              ✕
            </button>
          </span>
        )),
      )}
      {focusName && (
        <span className="filter-chip">
          Focus: {focusName}
          <button type="button" aria-label="Show all releases" onClick={() => setPrefs({ ...prefs, focus: null })}>
            ✕
          </button>
        </span>
      )}
      {active && (
        <>
          <span className="seg" role="group" aria-label="Steps that do not match">
            <button type="button" aria-pressed={filter.mode === 'highlight'} onClick={() => set({ mode: 'highlight' })}>
              Dim
            </button>
            <button type="button" aria-pressed={filter.mode === 'hide'} onClick={() => set({ mode: 'hide' })}>
              Hide
            </button>
          </span>
          <span className="text-muted text-sm font-bold" role="status">
            {match?.size ?? 0} of {view.steps.length} steps match
          </span>
          <button type="button" className="btn btn-xs btn-ol" onClick={() => setFilter({ ...emptyFilter, mode: filter.mode })}>
            Clear
          </button>
        </>
      )}
      <span className="flex-1" />
      <span className="seg" role="group" aria-label="Card size">
        {sizes.map(([value, name]) => (
          <button key={value} type="button" aria-pressed={prefs.size === value} onClick={() => setPrefs({ ...prefs, size: value })}>
            {name}
          </button>
        ))}
      </span>
    </section>
  )
}

const sizes: [ViewPrefs['size'], string][] = [
  ['full', 'Full'],
  ['compact', 'Compact'],
  ['titles', 'Titles'],
]
