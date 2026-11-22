import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useOutletContext } from 'react-router'
import { api, atLeast, type Label, type Membership } from '../api'
import { ConfirmButton, ErrorAlert, Field, InlineText, Loading } from '../ui'
import { PageBand } from './app'

const labelsKey = (slug: string) => ['ws', slug, 'labels'] as const

/**
 * The workspace's labels. Everyone reads them; editors add them (here or from
 * a step); admins rename and delete them.
 */
export function LabelsPage() {
  const ws = useOutletContext<Membership>()
  const qc = useQueryClient()
  const labels = useQuery({ queryKey: labelsKey(ws.slug), queryFn: () => api.labels(ws.slug) })
  const canCreate = atLeast(ws.role, 'editor')
  const canManage = atLeast(ws.role, 'admin')
  const write = useMutation({
    mutationFn: (w: () => Promise<unknown>) => w(),
    onSettled: () => qc.invalidateQueries({ queryKey: labelsKey(ws.slug) }),
  })
  return (
    <>
      <PageBand kicker={`${ws.name} › Labels`} title="Labels" />
      <main className="mx-auto grid max-w-5xl gap-12 px-7 py-10 lg:grid-cols-[1fr_340px]">
        <section aria-labelledby="labels-h">
          <h2 id="labels-h" className="label-up mb-3">
            Labels
          </h2>
          <p className="text-muted mb-6 font-medium">
            Labels are shared by every map in this workspace.
            {!canManage && ' Admins can rename and delete them.'}
          </p>
          <ErrorAlert error={write.error} />
          {labels.isPending ? (
            <Loading />
          ) : labels.isError ? (
            <ErrorAlert error={labels.error} />
          ) : labels.data.length === 0 ? (
            <p className="text-muted font-medium">No labels yet.</p>
          ) : (
            <ul className="rows" aria-label="Labels">
              {labels.data.map((l) => (
                <LabelRow key={l.id} ws={ws} label={l} canManage={canManage} busy={write.isPending} write={write.mutate} />
              ))}
            </ul>
          )}
        </section>
        {canCreate && <CreateLabel ws={ws} write={write.mutateAsync} />}
      </main>
    </>
  )
}

function LabelRow(props: { ws: Membership; label: Label; canManage: boolean; busy: boolean; write: (w: () => Promise<unknown>) => void }) {
  const { ws, label: l, canManage, write } = props
  return (
    <li>
      <span className="min-w-0 flex-1 font-extrabold">
        <InlineText
          value={l.name}
          label={`label ${l.name}`}
          editable={canManage}
          maxLength={50}
          onSave={(name) => write(() => api.patchLabel(ws.slug, l.id, { name }))}
        />
      </span>
      {canManage && (
        <ConfirmButton
          label="Delete"
          confirm="Delete from every step"
          name={l.name}
          busy={props.busy}
          onConfirm={() => write(() => api.deleteLabel(ws.slug, l.id))}
        />
      )}
    </li>
  )
}

function CreateLabel({ ws, write }: { ws: Membership; write: (w: () => Promise<unknown>) => Promise<unknown> }) {
  const [name, setName] = useState('')
  return (
    <section aria-labelledby="label-create" className="bg-base-200 border-line self-start border p-6">
      <h2 id="label-create" className="up mb-5 text-xl">
        New label
      </h2>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const n = name.trim()
          if (!n) return
          void write(() => api.createLabel(ws.slug, { name: n })).then(() => setName(''))
        }}
      >
        <Field label="Label name">
          <input className="input w-full" required maxLength={50} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <button className="btn btn-primary mt-2 w-full">Create label</button>
      </form>
    </section>
  )
}
