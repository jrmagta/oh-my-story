import { useMemo, type CSSProperties } from 'react'
import { nodeStyle } from '../map/colours'
import { backbone, bands, cellKey, cells } from '../map/layout'
import { UNSCHEDULED_BAND } from '../map/viewPrefs'
import type { MapNode, Release } from '../api'
import { Cell, estimated, formatDate, Meter, plural, useMap } from './map'

/** Today as YYYY-MM-DD in the viewer's time zone, to compare with target dates. */
function today(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/**
 * The map as a roadmap: releases across in map order, then Unscheduled;
 * objectives and their activities down. Each cell is the map's own cell, so
 * steps open, drag between releases (and activities) and are added the same
 * way, and the filter, focus and collapsed activities apply here too.
 */
export function Roadmap() {
  const { view, totals, prefs, ws } = useMap()
  const bb = useMemo(() => backbone(view), [view])
  const byCell = useMemo(() => cells(view), [view])
  const all = useMemo(() => bands(view), [view])
  const focused = all.filter((b) => (b.release?.id ?? UNSCHEDULED_BAND) === prefs.focus)
  const columns = focused.length ? focused : all
  const now = today()

  if (bb.columns.length === 0) {
    return <p className="text-muted py-10 font-medium">An empty map: add objectives and activities on the Map first.</p>
  }

  const style: CSSProperties = { gridTemplateColumns: `minmax(10rem, max-content) repeat(${columns.length}, 220px)` }
  return (
    <div className="roadmap" style={style} role="region" aria-label="Roadmap">
      <div className="rm-corner" />
      {columns.map((b, i) => {
        const total = totals.releases.get(b.release?.id ?? '')
        const late = isLate(b.release, now, total?.steps ?? 0, total?.byCategory.done ?? 0)
        const name = b.release?.name ?? 'Unscheduled'
        return (
          <div
            key={b.key}
            className={`rm-release ${late ? 'late' : ''} ${b.release ? '' : 'unscheduled'}`}
            style={{ gridColumn: i + 2 }}
            role="heading"
            aria-level={2}
            aria-label={`Release ${name}${late ? ', late' : ''}`}
          >
            <span className="rm-name">
              {name}
              {late && <span className="pill late-pill">Late</span>}
            </span>
            <span className="rm-date">{b.release?.targetDate ? formatDate(b.release.targetDate) : b.release ? 'No date' : 'Not planned'}</span>
            <span className="rm-sum">
              {[plural(total?.steps ?? 0, 'step'), total && estimated(total, ws.estimateUnit)].filter(Boolean).join(' · ')}
            </span>
            {total && <Meter total={total} />}
          </div>
        )
      })}

      {bb.objectives.map((o) => (
        <RoadmapObjective
          key={o.node.id}
          objective={o.node}
          activities={bb.columns.filter((c) => c.objective.id === o.node.id && c.activity).map((c) => c.activity!)}
          columns={columns.map((b) => b.release)}
          byCell={byCell}
        />
      ))}
    </div>
  )
}

/** One objective's row header and a row per activity, with a cell per release. */
function RoadmapObjective(props: { objective: MapNode; activities: MapNode[]; columns: (Release | null)[]; byCell: ReturnType<typeof cells> }) {
  const { prefs } = useMap()
  const obj = props.objective
  const acts = props.activities
  return (
      <>
        <div className={`rm-objective up ${obj.colour ? 'coloured' : ''}`} style={{ gridColumn: '1 / -1', ...nodeStyle(obj.colour) }}>
          {obj.title}
        </div>
        {acts.map((act) => {
          const narrow = prefs.collapsedActivities.includes(act.id)
          return (
            <div key={act.id} className="rm-row">
              <div className={`rm-activity ${act.colour ? 'coloured' : ''}`} style={nodeStyle(act.colour)}>
                {act.title}
              </div>
              {props.columns.map((release, i) => (
                <Cell
                  key={release?.id ?? 'unscheduled'}
                  column={i + 2}
                  activity={act}
                  release={release}
                  bandName={release?.name ?? 'Unscheduled'}
                  narrow={narrow}
                  cards={props.byCell.get(cellKey(act.id, release?.id ?? null)) ?? []}
                />
              ))}
            </div>
          )
        })}
      </>
  )
}

/** A release is late when its target date has passed and some of its steps are not done. */
export function isLate(release: Release | null, now: string, steps: number, done: number): boolean {
  return !!release?.targetDate && release.targetDate < now && done < steps
}
