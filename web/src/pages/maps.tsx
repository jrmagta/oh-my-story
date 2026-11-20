import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useOutletContext } from 'react-router'
import { api, atLeast, type Membership, type StoryMap } from '../api'
import { ConfirmButton, ErrorAlert, Field, Loading } from '../ui'
import { PageBand } from './app'
import { workflowsKey } from './workflows'

export const mapsKey = (slug: string) => ['ws', slug, 'maps'] as const

export function MapsPage() {
  const ws = useOutletContext<Membership>()
  const maps = useQuery({ queryKey: mapsKey(ws.slug), queryFn: () => api.maps(ws.slug) })
  const canEdit = atLeast(ws.role, 'editor')
  return (
    <>
      <PageBand kicker={`${ws.name} › Story maps`} title="Story maps" />
      <main className="mx-auto grid max-w-5xl gap-12 px-7 py-10 lg:grid-cols-[1fr_340px]">
        <section aria-labelledby="maps-h">
          <h2 id="maps-h" className="label-up mb-3">
            Maps
          </h2>
          {maps.isPending ? (
            <Loading />
          ) : maps.isError ? (
            <ErrorAlert error={maps.error} />
          ) : maps.data.length === 0 ? (
            <p className="text-muted font-medium">
              No maps yet.{canEdit ? ' Create the first one.' : ' An editor can create the first one.'}
            </p>
          ) : (
            <ul className="rows" aria-label="Maps">
              {maps.data.map((m) => (
                <MapRow key={m.id} ws={ws} m={m} canEdit={canEdit} />
              ))}
            </ul>
          )}
        </section>
        {canEdit && <CreateMap ws={ws} />}
      </main>
    </>
  )
}

function MapRow({ ws, m, canEdit }: { ws: Membership; m: StoryMap; canEdit: boolean }) {
  const qc = useQueryClient()
  const remove = useMutation({
    mutationFn: () => api.deleteMap(ws.slug, m.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: mapsKey(ws.slug) }),
  })
  const to = `/w/${ws.slug}/maps/${m.id}`
  return (
    <li>
      <div className="min-w-0 flex-1">
        <Link to={to} className="up block truncate text-2xl hover:underline">
          {m.name}
        </Link>
        {m.description && <span className="text-muted block truncate text-sm font-medium">{m.description}</span>}
      </div>
      {canEdit && (
        <ConfirmButton
          label="Delete"
          confirm="Confirm delete"
          name={m.name}
          busy={remove.isPending}
          onConfirm={() => remove.mutate()}
        />
      )}
      <Link to={to} className="btn btn-sm btn-ol" aria-label={`Open ${m.name}`}>
        Open
      </Link>
    </li>
  )
}

function CreateMap({ ws }: { ws: Membership }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const workflows = useQuery({ queryKey: workflowsKey(ws.slug), queryFn: () => api.workflows(ws.slug) })
  const [name, setName] = useState('')
  const [workflowId, setWorkflowId] = useState('')
  const create = useMutation({
    mutationFn: () => api.createMap(ws.slug, { name, ...(workflowId ? { workflowId } : {}) }),
    onSuccess: async (m) => {
      await qc.invalidateQueries({ queryKey: mapsKey(ws.slug) })
      navigate(`/w/${ws.slug}/maps/${m.id}`)
    },
  })
  return (
    <section aria-labelledby="map-create" className="bg-base-200 border-line self-start border p-6">
      <h2 id="map-create" className="up mb-5 text-xl">
        New map
      </h2>
      <ErrorAlert error={create.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Map name">
          <input className="input w-full" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        {workflows.data && workflows.data.length > 1 && (
          <Field label="Workflow" hint="Statuses for this map's steps. Fixed after creation.">
            <select className="select w-full" value={workflowId} onChange={(e) => setWorkflowId(e.target.value)}>
              {workflows.data.map((w) => (
                <option key={w.id} value={w.isDefault ? '' : w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </Field>
        )}
        <button className="btn btn-primary mt-2 w-full" disabled={create.isPending}>
          Create map
        </button>
      </form>
    </section>
  )
}
