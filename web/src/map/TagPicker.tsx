import { useState } from 'react'
import { palette } from './colours'

/** Something a picker offers: a label or a persona. */
export interface Tag {
  id: string
  name: string
  colour: string
}

/** A colour for a new label or persona, the same one each time for the same name. */
export function colourFor(name: string): string {
  let h = 0
  for (const ch of name.toLowerCase()) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return palette[h % palette.length]?.hex ?? '#8A8A93'
}

/**
 * Adds and removes tags (a step's labels, an objective's personas): type to
 * filter, click or Enter to toggle, and a name no tag has yet offers to create
 * it. Escape or leaving closes it. With iconLabel the trigger is a small icon
 * button with that accessible name; otherwise it reads "+ Label".
 */
export function TagPicker(props: {
  noun: 'label' | 'persona'
  tags: Tag[]
  selected: string[]
  onToggle: (id: string) => void
  onCreate: (name: string) => void
  iconLabel?: string
}) {
  const Noun = props.noun === 'label' ? 'Label' : 'Persona'
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const q = query.trim()
  const matches = [...props.tags]
    .filter((l) => l.name.toLowerCase().includes(q.toLowerCase()))
    .sort((a, b) => a.name.localeCompare(b.name))
  const exact = props.tags.find((l) => l.name.toLowerCase() === q.toLowerCase())
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
      {props.iconLabel ? (
        <button
          type="button"
          className="icon-btn"
          aria-label={props.iconLabel}
          title={`${Noun}s`}
          aria-expanded={open}
          onClick={() => (open ? close() : setOpen(true))}
        >
          <span>☺</span>
        </button>
      ) : (
        <button type="button" className="btn btn-xs btn-ol" aria-expanded={open} onClick={() => (open ? close() : setOpen(true))}>
          + {Noun}
        </button>
      )}
      {open && (
        <div className="label-pop" role="group" aria-label={`${Noun}s`}>
          <input
            autoFocus
            className="input input-sm w-full"
            aria-label={`Find or create a ${props.noun}`}
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
          {!q && props.tags.length === 0 && <p className="text-muted text-sm font-medium">No {props.noun}s yet. Type a name to create one.</p>}
        </div>
      )}
    </span>
  )
}
