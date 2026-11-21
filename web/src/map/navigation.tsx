import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useMap } from '../pages/map'

/** Zoom levels the controls, Ctrl+scroll and Ctrl+plus/minus step through. */
export const zoomLevels = [0.5, 0.6, 0.75, 0.9, 1, 1.1, 1.25, 1.5]
const minZoom = zoomLevels[0]!
const maxZoom = zoomLevels[zoomLevels.length - 1]!

export function clampZoom(z: number): number {
  return Math.min(maxZoom, Math.max(minZoom, Math.round(z * 100) / 100))
}

/** The next level in a direction from z (z may sit between levels after Fit). */
function stepZoom(z: number, dir: 1 | -1): number {
  const next = dir > 0 ? zoomLevels.find((l) => l > z + 0.001) : [...zoomLevels].reverse().find((l) => l < z - 0.001)
  return next ?? z
}

/** Where a press on the map is for something else, not for panning. */
const notPannable =
  'button, a, input, textarea, select, label, [role="button"], [contenteditable], .step-card, .map-obj, .map-act, .band-row, .add-slot, .side-add, [data-no-drag]'

/**
 * The map's scroll area. Drag empty space to pan; Ctrl+scroll (or a pinch)
 * and Ctrl+plus/minus/0 zoom; zoom keeps the point under the pointer (or the
 * middle of the view) in place. Zoom controls and the minimap sit in the
 * bottom-right corner.
 */
export function MapViewport({ panelOpen, children }: { panelOpen: boolean; children: ReactNode }) {
  const { prefs, setPrefs } = useMap()
  const ref = useRef<HTMLDivElement>(null)
  const zoom = prefs.zoom
  const prevZoom = useRef(zoom)
  // The point to hold still through the next zoom, relative to the scroll area's top left.
  const anchor = useRef<{ x: number; y: number } | null>(null)
  const prefsRef = useRef(prefs)
  prefsRef.current = prefs

  const setZoom = useCallback(
    (z: number, at?: { x: number; y: number }) => {
      anchor.current = at ?? null
      const p = prefsRef.current
      const next = clampZoom(z)
      if (next !== p.zoom) setPrefs({ ...p, zoom: next })
    },
    [setPrefs],
  )

  // Release bands are as wide as the part of the map in view (see .band-row), in the map's own
  // (zoomed) units, so the whole band, with its Focus and menu, is always on screen.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const set = () => {
      const style = getComputedStyle(el)
      const room = el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
      el.style.setProperty('--pane-w', `${Math.max(0, room / zoom)}px`)
    }
    set()
    const ro = new ResizeObserver(set)
    ro.observe(el)
    return () => ro.disconnect()
  }, [zoom])

  useLayoutEffect(() => {
    const el = ref.current
    const before = prevZoom.current
    prevZoom.current = zoom
    if (!el || before === zoom) return
    const a = anchor.current ?? { x: el.clientWidth / 2, y: el.clientHeight / 2 }
    anchor.current = null
    const k = zoom / before
    el.scrollLeft = (el.scrollLeft + a.x) * k - a.x
    el.scrollTop = (el.scrollTop + a.y) * k - a.y
  }, [zoom])

  // Ctrl+scroll and pinches zoom the map instead of the page; this needs a non-passive listener.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return
      e.preventDefault()
      const r = el.getBoundingClientRect()
      setZoom(stepZoom(prefsRef.current.zoom, e.deltaY < 0 ? 1 : -1), { x: e.clientX - r.left, y: e.clientY - r.top })
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [setZoom])

  // Ctrl+plus/minus/0 zoom the map while it is open, unless typing.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return
      const t = e.target as HTMLElement | null
      if (t?.closest('input, textarea, select, [contenteditable]')) return
      const z = prefsRef.current.zoom
      if (e.key === '=' || e.key === '+') setZoom(stepZoom(z, 1))
      else if (e.key === '-') setZoom(stepZoom(z, -1))
      else if (e.key === '0') setZoom(1)
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setZoom])

  // Drag on empty space pans.
  const pan = useRef<{ x: number; y: number; left: number; top: number; id: number } | null>(null)
  const [panning, setPanning] = useState(false)

  const fit = () => {
    const el = ref.current
    const content = el?.querySelector<HTMLElement>('.map-zoom')
    if (!el || !content) return
    // The map's size at 100%, against the room the scroll area has (less its padding).
    const w = content.scrollWidth
    const h = content.scrollHeight
    const style = getComputedStyle(el)
    const roomW = el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
    const roomH = el.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom)
    setZoom(Math.min(1, roomW / w, roomH / h))
    el.scrollTo({ left: 0, top: 0 })
  }

  return (
    <>
      <div
        ref={ref}
        className={`map-scroll ${panelOpen ? 'with-panel' : ''} ${panning ? 'panning' : ''}`}
        onPointerDown={(e) => {
          const el = ref.current
          if (!el || e.button !== 0 || (e.target as HTMLElement).closest(notPannable)) return
          pan.current = { x: e.clientX, y: e.clientY, left: el.scrollLeft, top: el.scrollTop, id: e.pointerId }
          el.setPointerCapture(e.pointerId)
          setPanning(true)
        }}
        onPointerMove={(e) => {
          const el = ref.current
          const p = pan.current
          if (!el || !p || p.id !== e.pointerId) return
          el.scrollLeft = p.left - (e.clientX - p.x)
          el.scrollTop = p.top - (e.clientY - p.y)
        }}
        onPointerUp={() => {
          pan.current = null
          setPanning(false)
        }}
        onPointerCancel={() => {
          pan.current = null
          setPanning(false)
        }}
      >
        {children}
      </div>
      <div className={`map-nav ${panelOpen ? 'with-panel' : ''}`}>
        {prefs.minimap && <Minimap scroller={ref} />}
        <div className="map-nav-bar" role="group" aria-label="Zoom and overview">
          <button type="button" aria-label="Zoom out" disabled={zoom <= minZoom} onClick={() => setZoom(stepZoom(zoom, -1))}>
            −
          </button>
          <button type="button" className="pct" aria-label={`Zoom ${Math.round(zoom * 100)}%, reset to 100%`} title="Reset to 100%" onClick={() => setZoom(1)}>
            {Math.round(zoom * 100)}%
          </button>
          <button type="button" aria-label="Zoom in" disabled={zoom >= maxZoom} onClick={() => setZoom(stepZoom(zoom, 1))}>
            +
          </button>
          <button type="button" title="Zoom to fit the whole map" onClick={fit}>
            Fit
          </button>
          <button
            type="button"
            aria-pressed={prefs.minimap}
            title="Show an overview of the whole map"
            onClick={() => setPrefs({ ...prefs, minimap: !prefs.minimap })}
          >
            Minimap
          </button>
        </div>
      </div>
    </>
  )
}

interface Box {
  x: number
  y: number
  w: number
  h: number
  fill: string
}

const width = 200
const maxHeight = 140

/**
 * The whole map in small: backbone cards, release bands and steps, with the
 * part in view outlined. Click or drag in it to move the view there. Shown
 * only when the map does not fit in view.
 */
function Minimap({ scroller }: { scroller: React.RefObject<HTMLDivElement | null> }) {
  const { view, prefs, match, filter, mode } = useMap()
  const [boxes, setBoxes] = useState<Box[]>([])
  const [size, setSize] = useState({ w: 0, h: 0 })
  const [port, setPort] = useState({ x: 0, y: 0, w: 0, h: 0, fits: true })
  const [tick, setTick] = useState(0)

  // Redraw when the map's size changes.
  useEffect(() => {
    const el = scroller.current
    const content = el?.querySelector('.map-zoom')
    if (!el || !content) return
    const ro = new ResizeObserver(() => setTick((t) => t + 1))
    ro.observe(el)
    ro.observe(content)
    return () => ro.disconnect()
  }, [scroller, mode])

  // Cards and bands, in the scroll area's content coordinates. The pinned backbone sits where it
  // would be unscrolled; bands run the full width.
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    const r = el.getBoundingClientRect()
    const toX = (x: number) => x - r.left + el.scrollLeft
    const toY = (y: number) => y - r.top + el.scrollTop
    const backbone = el.querySelector<HTMLElement>('.backbone')
    const grid = el.querySelector<HTMLElement>('.map-grid')
    const gridBox = grid?.getBoundingClientRect()
    const shift = backbone && gridBox ? backbone.getBoundingClientRect().top - gridBox.top : 0
    const out: Box[] = []
    el.querySelectorAll<HTMLElement>('.map-obj, .map-act, .band-row, .rm-release, .step-card').forEach((n) => {
      const b = n.getBoundingClientRect()
      const pinned = n.closest('.backbone') ? shift : 0
      let bg = getComputedStyle(n).backgroundColor
      if (n.matches('.step-card')) bg = 'var(--oms-muted)'
      else if (bg === 'rgba(0, 0, 0, 0)' || bg === 'transparent') bg = 'var(--color-base-content)'
      // A band is pinned to the left of the view and as wide as it; draw it across the whole map.
      const band = n.matches('.band-row') && gridBox
      out.push({ x: toX(band ? gridBox.left : b.left), y: toY(b.top) - pinned, w: band ? gridBox.width : b.width, h: b.height, fill: bg })
    })
    setBoxes(out)
    setSize({ w: el.scrollWidth, h: el.scrollHeight })
  }, [scroller, view, prefs, match, filter, mode, tick])

  // The part in view, kept current as the map scrolls.
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    let frame = 0
    const read = () => {
      frame = 0
      setPort({
        x: el.scrollLeft,
        y: el.scrollTop,
        w: el.clientWidth,
        h: el.clientHeight,
        fits: el.scrollWidth <= el.clientWidth + 1 && el.scrollHeight <= el.clientHeight + 1,
      })
    }
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(read)
    }
    read()
    el.addEventListener('scroll', onScroll)
    const ro = new ResizeObserver(onScroll)
    ro.observe(el)
    return () => {
      el.removeEventListener('scroll', onScroll)
      ro.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [scroller, tick])

  if (port.fits || size.w === 0) return null
  const scale = Math.min(width / size.w, maxHeight / size.h)
  const moveTo = (e: React.PointerEvent<SVGSVGElement>) => {
    const el = scroller.current
    if (!el) return
    const b = e.currentTarget.getBoundingClientRect()
    el.scrollLeft = (e.clientX - b.left) / scale - el.clientWidth / 2
    el.scrollTop = (e.clientY - b.top) / scale - el.clientHeight / 2
  }
  return (
    <svg
      className="minimap"
      role="img"
      aria-label="Map overview: click or drag to move the view"
      width={Math.ceil(size.w * scale)}
      height={Math.ceil(size.h * scale)}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        moveTo(e)
      }}
      onPointerMove={(e) => {
        if (e.currentTarget.hasPointerCapture(e.pointerId)) moveTo(e)
      }}
    >
      {boxes.map((b, i) => (
        <rect key={i} x={b.x * scale} y={b.y * scale} width={Math.max(1, b.w * scale)} height={Math.max(1, b.h * scale)} rx={1} style={{ fill: b.fill }} />
      ))}
      <rect className="port" x={port.x * scale} y={port.y * scale} width={port.w * scale} height={port.h * scale} />
    </svg>
  )
}
