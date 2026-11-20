import { useState } from 'react'
import type { Label } from '../api'
import { palette } from './colours'

/** A colour for a new label, the same one each time for the same name. */
export function colourFor(name: string): string {
  let h = 0
  for (const ch of name.toLowerCase()) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return palette[h % palette.length]?.hex ?? '#8A8A93'
}

/**
 * Adds and removes a step's labels: type to filter, click or Enter to toggle,
 * and a name no label has yet offers to create it. Escape or leaving closes it.
 */
export function LabelPicker(props: {
  labels: Label[]
  selected: string[]
  onToggle: (id: string) => void
  onCreate: (name: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const q = query.trim()
  const matches = [...props.labels]
    .filter((l) => l.name.toLowerCase().includes(q.toLowerCase()))
    .sort((a, b) => a.name.localeCompare(b.name))
  const exact = props.labels.find((l) => l.name.toLowerCase() === q.toLowerCase())
  const close = () => {
    setOpen(false)
    setQuery('')
  }
  return (
    <span
      className="relative"
      onKeyDown={(e) => {
        if (e.key === 'Escape') close()
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) close()
      }}
    >
      <button type="button" className="btn btn-xs btn-ol" aria-expanded={open} onClick={() => (open ? close() : setOpen(true))}>
        + Label
      </button>
      {open && (
        <div className="label-pop" role="group" aria-label="Labels">
          <input
            autoFocus
            className="input input-sm w-full"
            aria-label="Find or create a label"
            placeholder="Find or create"
            maxLength={50}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key !== 'Enter' || !q) return
              e.preventDefault()
              if (exact) props.onToggle(exact.id)
              else props.onCreate(q)
              setQuery('')
            }}
          />
          <ul>
            {matches.map((l) => (
              <li key={l.id}>
                <button type="button" aria-pressed={props.selected.includes(l.id)} onClick={() => props.onToggle(l.id)}>
                  <i style={{ background: l.colour }} aria-hidden />
                  {l.name}
                  {props.selected.includes(l.id) && <span className="ml-auto">✓</span>}
                </button>
              </li>
            ))}
          </ul>
          {q && !exact && (
            <button
              type="button"
              className="btn btn-xs btn-primary w-full"
              onClick={() => {
                props.onCreate(q)
                setQuery('')
              }}
            >
              Create “{q}”
            </button>
          )}
          {!q && props.labels.length === 0 && <p className="text-muted text-sm font-medium">No labels yet. Type a name to create one.</p>}
        </div>
      )}
    </span>
  )
}
