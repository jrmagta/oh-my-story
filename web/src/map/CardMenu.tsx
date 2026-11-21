import { useState, type ReactNode } from 'react'

/**
 * A ⋯ button in a card's corner that opens a small menu. It sits over the
 * card rather than beside the title, so the title keeps the card's width.
 * Presses inside never start a drag. children gets close(), and is mounted
 * only while open, so a sub-panel's state resets each time.
 */
export function CardMenu({ label, children }: { label: string; children: (close: () => void) => ReactNode }) {
  const [open, setOpen] = useState(false)
  const close = () => setOpen(false)
  return (
    <span
      className={`card-menu ${open ? 'open' : ''}`}
      data-no-drag
      onKeyDown={(e) => {
        if (e.key === 'Escape') close()
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) close()
      }}
    >
      <button type="button" className="menu-btn" aria-label={`Actions for ${label}`} aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        ⋯
      </button>
      {open && (
        <div className="menu-pop" role="group" aria-label={`Actions for ${label}`}>
          {children(close)}
        </div>
      )}
    </span>
  )
}

/** One action in a CardMenu. */
export function MenuItem({ onClick, danger, children }: { onClick: () => void; danger?: boolean; children: ReactNode }) {
  return (
    <button type="button" className={`menu-item ${danger ? 'danger' : ''}`} onClick={onClick}>
      {children}
    </button>
  )
}
