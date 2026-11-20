import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useOutletContext } from 'react-router'
import { api, atLeast, type Category, type Membership, type Status, type Workflow } from '../api'
import { ErrorAlert, Field, InlineText, Loading } from '../ui'
import { PageBand } from './app'

export const workflowsKey = (slug: string) => ['ws', slug, 'workflows'] as const

export const categories: { value: Category; label: string }[] = [
  { value: 'todo', label: 'To do' },
  { value: 'doing', label: 'In progress' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'done', label: 'Done' },
]

const defaultColour: Record<Category, string> = { todo: '#8A8A93', doing: '#FCB131', blocked: '#BA3636', done: '#549334' }

/**
 * Workflows and their statuses. Everyone can read them; admins change them.
 * Categories, not names, drive status colours on the map and roll-ups, so a
 * status can be called anything.
 */
export function WorkflowsPage() {
  const ws = useOutletContext<Membership>()
  const workflows = useQuery({ queryKey: workflowsKey(ws.slug), queryFn: () => api.workflows(ws.slug) })
  const canEdit = atLeast(ws.role, 'admin')
  return (
    <>
      <PageBand kicker={`${ws.name} › Workflows`} title="Workflows" />
      <main className="mx-auto grid max-w-5xl gap-12 px-7 py-10 lg:grid-cols-[1fr_340px]">
        <section aria-label="Workflows">
          <p className="text-muted mb-8 font-medium">
            Each map uses one workflow, chosen when the map is created. A status's category decides how it counts: to do,
            in progress, blocked or done.
            {!canEdit && ' Admins can change workflows.'}
          </p>
          {workflows.isPending ? (
            <Loading />
          ) : workflows.isError ? (
            <ErrorAlert error={workflows.error} />
          ) : (
            workflows.data.map((w) => <WorkflowCard key={w.id} ws={ws} wf={w} canEdit={canEdit} />)
          )}
        </section>
        {canEdit && <CreateWorkflow ws={ws} />}
      </main>
    </>
  )
}

/** Runs a workflow change and reloads the list; reports the error if it fails. */
function useWorkflowWrite(ws: Membership) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (write: () => Promise<unknown>) => write(),
    onSettled: () => qc.invalidateQueries({ queryKey: workflowsKey(ws.slug) }),
  })
}

function WorkflowCard({ ws, wf, canEdit }: { ws: Membership; wf: Workflow; canEdit: boolean }) {
  const write = useWorkflowWrite(ws)
  return (
    <section className="mb-12" aria-label={`Workflow ${wf.name}`}>
      <h2 className="up mb-3 flex items-baseline gap-3 text-2xl" aria-label={wf.name}>
        <InlineText
          value={wf.name}
          label={`workflow ${wf.name}`}
          editable={canEdit}
          maxLength={100}
          onSave={(name) => write.mutate(() => api.renameWorkflow(ws.slug, wf.id, { name }))}
        />
        {wf.isDefault && <span className="label-up">Default</span>}
      </h2>
      <ErrorAlert error={write.error} />
      <ul className="rows" aria-label={`Statuses in ${wf.name}`}>
        {wf.statuses.map((st, i) => (
          <StatusRow key={st.id} ws={ws} wf={wf} st={st} index={i} canEdit={canEdit} write={write.mutate} />
        ))}
      </ul>
      {canEdit && <AddStatus ws={ws} wf={wf} write={write.mutate} />}
    </section>
  )
}

function StatusRow(props: {
  ws: Membership
  wf: Workflow
  st: Status
  index: number
  canEdit: boolean
  write: (w: () => Promise<unknown>) => void
}) {
  const { ws, wf, st, index, canEdit, write } = props
  const [deleting, setDeleting] = useState(false)
  const others = wf.statuses.filter((s) => s.id !== st.id)
  const [replacement, setReplacement] = useState('')
  const prev = wf.statuses[index - 1]
  const next = wf.statuses[index + 1]
  const colour = st.colour ?? defaultColour[st.category]
  return (
    <li className="flex-wrap">
      {canEdit ? (
        <input
          type="color"
          className="h-8 w-8 cursor-pointer"
          aria-label={`Colour for ${st.name}`}
          value={colour.toLowerCase()}
          onChange={(e) => write(() => api.patchStatus(ws.slug, st.id, { colour: e.target.value.toUpperCase() }))}
        />
      ) : (
        <span className="h-8 w-8" style={{ background: colour }} aria-hidden />
      )}
      <span className="min-w-0 flex-1 font-extrabold">
        <InlineText
          value={st.name}
          label={`status ${st.name}`}
          editable={canEdit}
          maxLength={100}
          onSave={(name) => write(() => api.patchStatus(ws.slug, st.id, { name }))}
        />
      </span>
      {canEdit ? (
        <select
          className="select select-sm w-36 font-bold"
          aria-label={`Category for ${st.name}`}
          value={st.category}
          onChange={(e) => write(() => api.patchStatus(ws.slug, st.id, { category: e.target.value as Category }))}
        >
          {categories.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      ) : (
        <span className={`st ${st.category} w-36 text-right`}>{categories.find((c) => c.value === st.category)?.label}</span>
      )}
      {canEdit && (
        <span className="flex gap-1">
          <button
            type="button"
            className="btn btn-xs btn-ol"
            aria-label={`Move ${st.name} up`}
            disabled={!prev}
            onClick={() => prev && write(() => api.moveStatus(ws.slug, st.id, { beforeId: prev.id }))}
          >
            ↑
          </button>
          <button
            type="button"
            className="btn btn-xs btn-ol"
            aria-label={`Move ${st.name} down`}
            disabled={!next}
            onClick={() => next && write(() => api.moveStatus(ws.slug, st.id, { afterId: next.id }))}
          >
            ↓
          </button>
          <button
            type="button"
            className="btn btn-xs btn-ol"
            aria-label={`Delete ${st.name}`}
            aria-expanded={deleting}
            disabled={others.length === 0}
            title={others.length === 0 ? 'A workflow keeps at least one status' : undefined}
            onClick={() => {
              setReplacement(others[0]?.id ?? '')
              setDeleting((d) => !d)
            }}
          >
            Delete
          </button>
        </span>
      )}
      {deleting && (
        <form
          className="bg-base-200 border-line flex w-full items-end gap-3 border p-3"
          aria-label={`Delete ${st.name}`}
          onSubmit={(e) => {
            e.preventDefault()
            setDeleting(false)
            write(() => api.deleteStatus(ws.slug, st.id, replacement))
          }}
        >
          <label className="flex-1">
            <span className="label-up mb-1 block">Move its stories to</span>
            <select className="select select-sm w-full font-bold" value={replacement} onChange={(e) => setReplacement(e.target.value)}>
              {others.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </select>
          </label>
          <button className="btn btn-sm btn-error">Delete {st.name}</button>
          <button type="button" className="btn btn-sm btn-ol" onClick={() => setDeleting(false)}>
            Cancel
          </button>
        </form>
      )}
    </li>
  )
}

function AddStatus({ ws, wf, write }: { ws: Membership; wf: Workflow; write: (w: () => Promise<unknown>) => void }) {
  const [name, setName] = useState('')
  const [category, setCategory] = useState<Category>('doing')
  return (
    <form
      className="mt-4 flex items-end gap-3"
      aria-label={`Add a status to ${wf.name}`}
      onSubmit={(e) => {
        e.preventDefault()
        const n = name.trim()
        if (!n) return
        write(() => api.createStatus(ws.slug, wf.id, { name: n, category, colour: defaultColour[category] }))
        setName('')
      }}
    >
      <label className="flex-1">
        <span className="label-up mb-1 block">New status</span>
        <input className="input input-sm w-full" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label>
        <span className="label-up mb-1 block">Category</span>
        <select className="select select-sm w-36 font-bold" value={category} onChange={(e) => setCategory(e.target.value as Category)}>
          {categories.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </label>
      <button className="btn btn-sm btn-primary">Add status</button>
    </form>
  )
}

function CreateWorkflow({ ws }: { ws: Membership }) {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: () => api.createWorkflow(ws.slug, { name }),
    onSuccess: async () => {
      setName('')
      await qc.invalidateQueries({ queryKey: workflowsKey(ws.slug) })
    },
  })
  return (
    <section aria-labelledby="wf-create" className="bg-base-200 border-line self-start border p-6">
      <h2 id="wf-create" className="up mb-5 text-xl">
        New workflow
      </h2>
      <ErrorAlert error={create.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Workflow name" hint="Starts with To do, In progress, Blocked and Done.">
          <input className="input w-full" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <button className="btn btn-primary mt-2 w-full" disabled={create.isPending}>
          Create workflow
        </button>
      </form>
    </section>
  )
}
