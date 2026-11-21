import { useEffect, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { api, type SearchHit } from '../api'

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
export const searchShortcut = isMac ? '⌘K' : 'Ctrl+K'

/**
 * Search across the workspace's steps. Opens with Ctrl+K (⌘K on a Mac) or the
 * nav button; arrows choose a result and Enter opens it on its map, with its
 * panel open. Escape or clicking outside closes it.
 */
export function SearchPalette({ slug }: { slug: string }) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  return (
    <>
      <button type="button" className="search-btn" aria-label="Search steps" onClick={() => setOpen(true)}>
        Search <kbd>{searchShortcut}</kbd>
      </button>
      {open && <Palette slug={slug} close={() => setOpen(false)} />}
    </>
  )
}

function Palette({ slug, close }: { slug: string; close: () => void }) {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [settled, setSettled] = useState('')
  const [index, setIndex] = useState(0)
  useEffect(() => {
    const t = setTimeout(() => setSettled(q.trim()), 150)
    return () => clearTimeout(t)
  }, [q])
  const hits = useQuery({
    queryKey: ['ws', slug, 'search', settled],
    queryFn: () => api.search(slug, settled),
    enabled: settled !== '',
    placeholderData: keepPreviousData,
  })
  const list = settled ? (hits.data ?? []) : []
  const go = (h: SearchHit | undefined) => {
    if (!h) return
    close()
    navigate(`/w/${slug}/maps/${h.mapId}?step=${h.stepId}`)
  }
  return (
    <div className="palette-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="palette" role="dialog" aria-modal="true" aria-label="Search steps">
        <input
          autoFocus
          className="palette-input"
          role="combobox"
          aria-expanded={list.length > 0}
          aria-controls="search-results"
          aria-activedescendant={list[index] ? `hit-${list[index].stepId}` : undefined}
          aria-label="Search steps by key or words"
          placeholder="Search steps by key or words"
          maxLength={200}
          value={q}
          onChange={(e) => {
            setQ(e.target.value)
            setIndex(0)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') close()
            else if (e.key === 'ArrowDown') {
              e.preventDefault()
              setIndex((i) => Math.min(i + 1, list.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setIndex((i) => Math.max(i - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              go(list[index])
            }
          }}
        />
        <ul id="search-results" role="listbox" aria-label="Results" className="palette-list">
          {list.map((h, i) => (
            <li
              key={h.stepId}
              id={`hit-${h.stepId}`}
              role="option"
              aria-selected={i === index}
              onMouseEnter={() => setIndex(i)}
              onClick={() => go(h)}
            >
              <span className="palette-key">{h.key}</span>
              <span className="palette-title">{h.title}</span>
              <span className="palette-where">
                {h.mapName} › {h.activity}
              </span>
            </li>
          ))}
        </ul>
        {settled && hits.isSuccess && list.length === 0 && <p className="palette-empty">No steps match “{settled}”.</p>}
        {!settled && <p className="palette-empty">Type a key such as CHC-12, or words from a step's title or description.</p>}
      </div>
    </div>
  )
}
