import { useState } from 'react'
import { palette } from './colours'

/** Something a picker offers: a label, or a persona with its colour. */
export interface Tag {
  id: string
  name: string
  colour?: string
}

/** A colour for a new persona, the same one each time for the same name. */
export function colourFor(name: string): string {
  let h = 0
  for (const ch of name.toLowerCase()) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return palette[h % palette.length]?.hex ?? '#8A8A93'
}

interface ChooserProps {
  noun: 'label' | 'persona'
  tags: Tag[]
  selected: string[]
  onToggle: (id: string) => void
  onCreate: (name: string) => void
}

/**
 * Adds and removes tags (a step's labels, an objective's personas): type to
 * filter, click or Enter to toggle, and a name no tag has yet offers to create
 * it. Used inside a popover: TagPicker's, or a card menu's.
 */
export function TagChooser(props: ChooserProps) {
  const Noun = props.noun === 'label' ? 'Label' : 'Persona'
  const [query, setQuery] = useState('')
  const q = query.trim()
  const matches = [...props.tags]
    .filter((l) => l.name.toLowerCase().includes(q.toLowerCase()))
    .sort((a, b) => a.name.localeCompare(b.name))
  const exact = props.tags.find((l) => l.name.toLowerCase() === q.toLowerCase())
  return (
    <div className="tag-chooser" role="group" aria-label={`${Noun}s`}>
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
              {l.colour && <i style={{ background: l.colour }} aria-hidden />}
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
  )
}

/** A "+ Label" button that opens a TagChooser. Escape or leaving closes it. */
export function TagPicker(props: ChooserProps) {
  const Noun = props.noun === 'label' ? 'Label' : 'Persona'
  const [open, setOpen] = useState(false)
  return (
    <span
      className="relative"
      onKeyDown={(e) => {
        if (e.key === 'Escape') setOpen(false)
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false)
      }}
    >
      <button type="button" className="btn btn-xs btn-ol" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        + {Noun}
      </button>
      {open && (
        <div className="label-pop">
          <TagChooser {...props} />
        </div>
      )}
    </span>
  )
}
