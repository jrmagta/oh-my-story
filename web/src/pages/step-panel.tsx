import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Markdown from 'react-markdown'
import { api } from '../api'
import { ConfirmButton, InlineText } from '../ui'
import { HistoryList } from '../map/HistoryList'
import { Comments } from '../map/Comments'
import { amount, formatEstimate } from '../map/estimates'
import { LabelChips, mapKey, StatusLabel, useMap } from './map'
import { colourFor, TagPicker } from '../map/TagPicker'
import { DependencySection } from '../map/Dependencies'
import { LinkedMaps } from '../map/LinkedMaps'

/** The open step, beside the map. Editors change it in place; everyone else reads. */
export function StepPanel({ stepId }: { stepId: string }) {
  const { ws, view, canEdit, statuses, run, openStep, shared } = useMap()
  const [tab, setTab] = useState<'details' | 'comments' | 'history'>('details')
  const members = useQuery({ queryKey: ['ws', ws.slug, 'members'], queryFn: () => api.members(ws.slug), enabled: !shared })
  const step = view.steps.find((s) => s.id === stepId)
  if (!step) return null
  const status = statuses.get(step.statusId)
  const placement = view.placements.find((p) => p.stepId === step.id)
  const activity = view.nodes.find((n) => n.id === placement?.activityId)
  const release = view.releases.find((r) => r.id === placement?.releaseId)
  const assignee = members.data?.find((m) => m.userId === step.assigneeId)
  // Delete takes a step off every map it is on, so the confirmation says how many.
  const onMaps = 1 + view.elsewhere.filter((e) => e.stepId === step.id).length
  const patch = (b: Parameters<typeof api.patchStep>[3]) => run(() => api.patchStep(ws.slug, view.map.id, step.id, b))

  return (
    <aside className="step-panel" aria-label={`Step ${step.key}`}>
      <div className={`hd ${status?.category ?? 'todo'}`}>
        <div className="flex items-center gap-2">
          <span className="pill">{step.key}</span>
          <span className="pill">{status?.name ?? 'No status'}</span>
          <span className="flex-1" />
          <button type="button" className="icon-btn" aria-label="Close step" onClick={() => openStep(null)}>
            <span>✕</span>
          </button>
        </div>
        <h2 aria-label={step.title}>
          <InlineText value={step.title} label="step" editable={canEdit} onSave={(title) => patch({ title })} />
        </h2>
      </div>
      <div role="tablist" aria-label="Step" className="step-tabs">
        {(shared ? (['details'] as const) : (['details', 'comments', 'history'] as const)).map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            id={`step-tab-${t}`}
            aria-selected={tab === t}
            aria-controls={`step-pane-${t}`}
            onClick={() => setTab(t)}
          >
            {t === 'details' ? 'Details' : t === 'comments' ? `Comments${step.commentCount ? ` (${step.commentCount})` : ''}` : 'History'}
          </button>
        ))}
      </div>
      {tab === 'details' ? (
        <div role="tabpanel" id="step-pane-details" aria-labelledby="step-tab-details">
          <dl>
            <div className="row">
              <dt>Status</dt>
              <dd>
                {canEdit ? (
                  <select
                    className="select select-sm w-44 font-bold"
                    aria-label="Status"
                    value={step.statusId}
                    onChange={(e) => patch({ statusId: e.target.value })}
                  >
                    {view.workflow.statuses.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <StatusLabel status={status} />
                )}
              </dd>
            </div>
            {!shared && (
            <div className="row">
              <dt>Assignee</dt>
              <dd>
                {canEdit ? (
                  <select
                    className="select select-sm w-44 font-bold"
                    aria-label="Assignee"
                    value={step.assigneeId ?? ''}
                    onChange={(e) => patch({ assigneeId: e.target.value || null })}
                  >
                    <option value="">Unassigned</option>
                    {members.data?.map((m) => (
                      <option key={m.userId} value={m.userId}>
                        {m.displayName}
                      </option>
                    ))}
                  </select>
                ) : (
                  (assignee?.displayName ?? 'Unassigned')
                )}
              </dd>
            </div>
            )}
            <div className="row">
              <dt>Estimate</dt>
              <dd>
                {canEdit ? (
                  <EstimateInput
                    key={`${step.id}:${step.estimate}`}
                    value={step.estimate}
                    unit={ws.estimateUnit}
                    onSave={(estimate) => patch({ estimate })}
                  />
                ) : step.estimate === null ? (
                  'None'
                ) : (
                  amount(step.estimate * 100, ws.estimateUnit)
                )}
              </dd>
            </div>
            <div className="row">
              <dt>Labels</dt>
              <dd className="labels-dd">
                <LabelChips
                  ids={step.labelIds}
                  onRemove={canEdit ? (id) => patch({ labelIds: step.labelIds.filter((x) => x !== id) }) : undefined}
                />
                {canEdit && (
                  <TagPicker
                    noun="label"
                    tags={view.labels}
                    selected={step.labelIds}
                    onToggle={(id) =>
                      patch({ labelIds: step.labelIds.includes(id) ? step.labelIds.filter((x) => x !== id) : [...step.labelIds, id] })
                    }
                    onCreate={(name) =>
                      run(async () => {
                        const l = await api.createLabel(ws.slug, { name, colour: colourFor(name) })
                        await api.patchStep(ws.slug, view.map.id, step.id, { labelIds: [...step.labelIds, l.id] })
                      })
                    }
                  />
                )}
                {!canEdit && step.labelIds.length === 0 && 'None'}
              </dd>
            </div>
            <div className="row">
              <dt>Placed in</dt>
              <dd className="text-right">
                {release?.name ?? 'Unscheduled'} · {activity?.title}
              </dd>
            </div>
          </dl>
          <Description key={step.id} text={step.descriptionMd} editable={canEdit} onSave={(descriptionMd) => patch({ descriptionMd })} />
          <DependencySection key={`deps:${step.id}`} stepId={step.id} stepKey={step.key} />
          <LinkedMaps key={`maps:${step.id}`} step={step} />
          {canEdit && (
            <div className="p-5">
              <ConfirmButton
                label="Delete step"
                confirm={onMaps > 1 ? `Delete from all ${onMaps} maps` : 'Confirm delete'}
                name={step.key}
                busy={false}
                onConfirm={() => {
                  openStep(null)
                  run(() => api.deleteStep(ws.slug, view.map.id, step.id), {
                    done: `Deleted ${step.key}`,
                    undo: () => api.restoreStep(ws.slug, view.map.id, step.id),
                  })
                }}
              />
            </div>
          )}
        </div>
      ) : tab === 'comments' ? (
        <div role="tabpanel" id="step-pane-comments" aria-labelledby="step-tab-comments">
          <Comments key={step.id} stepId={step.id} />
        </div>
      ) : (
        <div role="tabpanel" id="step-pane-history" aria-labelledby="step-tab-history">
          <HistoryList
            slug={ws.slug}
            view={view}
            queryKey={[...mapKey(ws.slug, view.map.id), 'history', step.id]}
            page={(before) => api.stepHistory(ws.slug, view.map.id, step.id, before)}
            label={`History of ${step.key}`}
          />
        </div>
      )}
    </aside>
  )
}

/** Saves on Enter or when focus leaves; empty clears the estimate, Escape puts it back. */
function EstimateInput({ value, unit, onSave }: { value: number | null; unit: string; onSave: (v: number | null) => void }) {
  const shown = value === null ? '' : formatEstimate(value)
  const [draft, setDraft] = useState(shown)
  const save = () => {
    const v = draft.trim() === '' ? null : Number(draft)
    if (v !== value && !Number.isNaN(v)) onSave(v)
  }
  return (
    <span className="flex items-center justify-end gap-2 font-bold">
      <input
        type="number"
        className="input input-sm w-24 text-right font-bold"
        aria-label={`Estimate (${unit})`}
        min={0}
        max={9999.99}
        step="any"
        placeholder="None"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur()
          if (e.key === 'Escape') setDraft(shown)
        }}
      />
      {unit}
    </span>
  )
}

function Description({ text, editable, onSave }: { text: string; editable: boolean; onSave: (v: string) => Promise<boolean> }) {
  const [draft, setDraft] = useState<string | null>(null)
  const [preview, setPreview] = useState(false)
  if (draft === null) {
    return (
      <section className="border-line border-b px-5 py-4" aria-label="Description">
        <div className="mb-2 flex items-center">
          <h3 className="label-up flex-1">Description</h3>
          {editable && (
            <button type="button" className="btn btn-xs btn-ol" onClick={() => setDraft(text)}>
              Edit
            </button>
          )}
        </div>
        {text ? (
          <div className="markdown">
            <Markdown>{text}</Markdown>
          </div>
        ) : (
          <p className="text-muted font-medium">No description.</p>
        )}
      </section>
    )
  }
  return (
    <section className="border-line border-b px-5 py-4" aria-label="Description">
      <div className="mb-2 flex items-center gap-2">
        <h3 className="label-up flex-1">Description</h3>
        <button type="button" className="btn btn-xs btn-ol" aria-pressed={preview} onClick={() => setPreview((p) => !p)}>
          {preview ? 'Write' : 'Preview'}
        </button>
      </div>
      {preview ? (
        <div className="markdown min-h-40">
          <Markdown>{draft}</Markdown>
        </div>
      ) : (
        <textarea
          autoFocus
          className="textarea min-h-40 w-full font-medium"
          aria-label="Description (Markdown)"
          maxLength={50_000}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      )}
      <div className="mt-3 flex gap-2">
        <button
          type="button"
          className="btn btn-sm btn-primary"
          onClick={async () => {
            if (await onSave(draft)) {
              setDraft(null)
              setPreview(false)
            }
          }}
        >
          Save
        </button>
        <button
          type="button"
          className="btn btn-sm btn-ol"
          onClick={() => {
            setDraft(null)
            setPreview(false)
          }}
        >
          Cancel
        </button>
      </div>
    </section>
  )
}
