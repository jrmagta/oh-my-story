import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Markdown from 'react-markdown'
import { api } from '../api'
import { ConfirmButton, InlineText } from '../ui'
import { HistoryList } from '../map/HistoryList'
import { mapKey, StatusLabel, useMap } from './map'

/** The open story, beside the map. Editors change it in place; everyone else reads. */
export function StoryPanel({ storyId }: { storyId: string }) {
  const { ws, view, canEdit, statuses, run, openStory } = useMap()
  const [tab, setTab] = useState<'details' | 'history'>('details')
  const members = useQuery({ queryKey: ['ws', ws.slug, 'members'], queryFn: () => api.members(ws.slug) })
  const story = view.stories.find((s) => s.id === storyId)
  if (!story) return null
  const status = statuses.get(story.statusId)
  const placement = view.placements.find((p) => p.storyId === story.id)
  const step = view.nodes.find((n) => n.id === placement?.stepId)
  const release = view.releases.find((r) => r.id === placement?.releaseId)
  const assignee = members.data?.find((m) => m.userId === story.assigneeId)
  const patch = (b: Parameters<typeof api.patchStory>[3]) => run(() => api.patchStory(ws.slug, view.map.id, story.id, b))

  return (
    <aside className="story-panel" aria-label={`Story ${story.key}`}>
      <div className={`hd ${status?.category ?? 'todo'}`}>
        <div className="flex items-center gap-2">
          <span className="pill">{story.key}</span>
          <span className="pill">{status?.name ?? 'No status'}</span>
          <span className="flex-1" />
          <button type="button" className="icon-btn" aria-label="Close story" onClick={() => openStory(null)}>
            <span>✕</span>
          </button>
        </div>
        <h2 aria-label={story.title}>
          <InlineText value={story.title} label="story" editable={canEdit} onSave={(title) => patch({ title })} />
        </h2>
      </div>
      <div role="tablist" aria-label="Story" className="story-tabs">
        {(['details', 'history'] as const).map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            id={`story-tab-${t}`}
            aria-selected={tab === t}
            aria-controls={`story-pane-${t}`}
            onClick={() => setTab(t)}
          >
            {t === 'details' ? 'Details' : 'History'}
          </button>
        ))}
      </div>
      {tab === 'details' ? (
        <div role="tabpanel" id="story-pane-details" aria-labelledby="story-tab-details">
          <dl>
            <div className="row">
              <dt>Status</dt>
              <dd>
                {canEdit ? (
                  <select
                    className="select select-sm w-44 font-bold"
                    aria-label="Status"
                    value={story.statusId}
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
            <div className="row">
              <dt>Assignee</dt>
              <dd>
                {canEdit ? (
                  <select
                    className="select select-sm w-44 font-bold"
                    aria-label="Assignee"
                    value={story.assigneeId ?? ''}
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
            <div className="row">
              <dt>Placed in</dt>
              <dd className="text-right">
                {release?.name ?? 'Unscheduled'} · {step?.title}
              </dd>
            </div>
          </dl>
          <Description key={story.id} text={story.descriptionMd} editable={canEdit} onSave={(descriptionMd) => patch({ descriptionMd })} />
          {canEdit && (
            <div className="p-5">
              <ConfirmButton
                label="Delete story"
                confirm="Confirm delete"
                name={story.key}
                busy={false}
                onConfirm={() => {
                  openStory(null)
                  run(() => api.deleteStory(ws.slug, view.map.id, story.id), {
                    done: `Deleted ${story.key}`,
                    undo: () => api.restoreStory(ws.slug, view.map.id, story.id),
                  })
                }}
              />
            </div>
          )}
        </div>
      ) : (
        <div role="tabpanel" id="story-pane-history" aria-labelledby="story-tab-history">
          <HistoryList
            slug={ws.slug}
            view={view}
            queryKey={[...mapKey(ws.slug, view.map.id), 'history', story.id]}
            page={(before) => api.storyHistory(ws.slug, view.map.id, story.id, before)}
            label={`History of ${story.key}`}
          />
        </div>
      )}
    </aside>
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
