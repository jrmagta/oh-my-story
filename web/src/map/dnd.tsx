import { useState, type KeyboardEventHandler, type PointerEventHandler, type ReactNode } from 'react'
import {
  closestCorners,
  DndContext,
  getFirstCollision,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  pointerWithin,
  useDndContext,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type Announcements,
  type CollisionDetection,
  type DragEndEvent,
  type KeyboardCoordinateGetter,
} from '@dnd-kit/core'
import { accepts, isSelf, moveFor, type DragItem, type DropTarget, type Move } from './moves'
import type { MapView } from '../api'

const itemOf = (d: unknown) => (d as { item?: DragItem } | undefined)?.item
const targetOf = (d: unknown) => (d as { target?: DropTarget } | undefined)?.target

/**
 * With a pointer, the smallest target under it wins, so a card beats the cell
 * around it. The keyboard has no pointer; it moves between enabled targets.
 */
const collisions: CollisionDetection = (args) => {
  const under = pointerWithin(args)
  if (under.length > 0) {
    const area = (id: string | number) => {
      const r = args.droppableRects.get(id)
      return r ? r.width * r.height : Infinity
    }
    return [...under].sort((a, b) => area(a.id) - area(b.id))
  }
  return closestCorners(args)
}

/**
 * Arrow keys move to the nearest target in that direction, skipping any target
 * the dragged item is inside. Without that, Up from the second card in a cell
 * lands on the cell (meaning "the end of it") rather than on the first card.
 */
const keyboardCoordinates: KeyboardCoordinateGetter = (event, { context }) => {
  const { active, collisionRect, droppableRects, droppableContainers, over } = context
  const dir = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' }[event.code]
  if (!dir) return undefined
  event.preventDefault()
  if (!active || !collisionRect) return undefined
  const cx = collisionRect.left + collisionRect.width / 2
  const cy = collisionRect.top + collisionRect.height / 2
  const candidates = droppableContainers.getEnabled().filter((entry) => {
    const r = droppableRects.get(entry.id)
    if (!r) return false
    const around = r.left <= cx && cx <= r.right && r.top <= cy && cy <= r.bottom
    if (around) return false
    switch (dir) {
      case 'up':
        return r.top < collisionRect.top
      case 'down':
        return r.top > collisionRect.top
      case 'left':
        return r.left < collisionRect.left
      default:
        return r.left > collisionRect.left
    }
  })
  const hits = closestCorners({ active, collisionRect, droppableRects, droppableContainers: candidates, pointerCoordinates: null })
  let id = getFirstCollision(hits, 'id')
  if (id === over?.id && hits.length > 1) id = hits[1]?.id ?? null
  const r = id == null ? undefined : droppableRects.get(id)
  return r ? { x: r.left, y: r.top } : undefined
}

const announcements: Announcements = {
  onDragStart: ({ active }) => `Picked up ${itemOf(active.data.current)?.label ?? 'item'}.`,
  onDragOver: ({ active, over }) =>
    over ? `${itemOf(active.data.current)?.label} is over ${targetOf(over.data.current)?.label}.` : undefined,
  onDragEnd: ({ active, over }) =>
    over ? `Dropped ${itemOf(active.data.current)?.label} on ${targetOf(over.data.current)?.label}.` : 'Dropped. Nothing moved.',
  onDragCancel: ({ active }) => `Cancelled. ${itemOf(active.data.current)?.label} did not move.`,
}

/** Drag and drop for the map. onMove gets each drop that changes something. */
export function MapDnd({ view, onMove, children }: { view: MapView; onMove: (m: Move) => void; children: ReactNode }) {
  const sensors = useSensors(
    // A short distance before a drag starts keeps clicks working on cards and titles.
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: keyboardCoordinates }),
  )
  const [dragging, setDragging] = useState<DragItem | null>(null)
  const end = (e: DragEndEvent) => {
    setDragging(null)
    const item = itemOf(e.active.data.current)
    const target = targetOf(e.over?.data.current)
    if (!item || !target) return
    const m = moveFor(view, item, target)
    if (m) onMove(m)
  }
  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisions}
      accessibility={{
        announcements,
        screenReaderInstructions: {
          draggable: 'To move this, press space or enter. Use the arrow keys to choose where, then space or enter to drop it there, or escape to cancel.',
        },
      }}
      onDragStart={(e) => setDragging(itemOf(e.active.data.current) ?? null)}
      onDragEnd={end}
      onDragCancel={() => setDragging(null)}
    >
      {children}
      <DragOverlay dropAnimation={null}>{dragging && <div className="drag-ghost">{dragging.label}</div>}</DragOverlay>
    </DndContext>
  )
}

/**
 * A draggable item. Spread `pointer` on the element people grab and `handle`
 * on a keyboard-focusable button, so the element's own click and Enter keep
 * their meaning.
 */
export function useDrag(id: string, item: DragItem, disabled: boolean) {
  const d = useDraggable({ id: `drag:${id}`, data: { item }, disabled })
  const { onKeyDown, onPointerDown } = (d.listeners ?? {}) as {
    onKeyDown?: KeyboardEventHandler
    onPointerDown?: PointerEventHandler
  }
  return {
    ref: d.setNodeRef,
    dragging: d.isDragging,
    pointer: { onPointerDown },
    handle: { ...d.attributes, onKeyDown, ref: d.setActivatorNodeRef, 'aria-label': `Move ${item.label}` },
  }
}

/**
 * A place to drop. It turns itself off for drags it cannot take, except the
 * dragged item's own place: a drag starts over itself, so picking something up
 * and dropping it straight away moves nothing.
 */
export function useDrop(id: string, target: DropTarget) {
  const { active } = useDndContext()
  const item = itemOf(active?.data.current)
  const ok = accepts(target, item)
  const d = useDroppable({ id: `drop:${id}`, data: { target }, disabled: !ok && !isSelf(target, item) })
  return { ref: d.setNodeRef, over: d.isOver && ok }
}

/** One ref callback that sets several refs, for an element that is dragged and dropped on. */
export function refs<T>(...fns: ((el: T | null) => void)[]) {
  return (el: T | null) => fns.forEach((f) => f(el))
}
