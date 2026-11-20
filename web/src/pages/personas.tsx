import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useOutletContext } from 'react-router'
import { api, atLeast, type Membership, type Persona } from '../api'
import { colourFor } from '../map/TagPicker'
import { ConfirmButton, ErrorAlert, Field, InlineText, Loading } from '../ui'
import { PageBand } from './app'

const personasKey = (slug: string) => ['ws', slug, 'personas'] as const

/** The workspace's personas. Everyone reads them; editors add, change and delete them. */
export function PersonasPage() {
  const ws = useOutletContext<Membership>()
  const qc = useQueryClient()
  const personas = useQuery({ queryKey: personasKey(ws.slug), queryFn: () => api.personas(ws.slug) })
  const canEdit = atLeast(ws.role, 'editor')
  const write = useMutation({
    mutationFn: (w: () => Promise<unknown>) => w(),
    onSettled: () => qc.invalidateQueries({ queryKey: personasKey(ws.slug) }),
  })
  return (
    <>
      <PageBand kicker={`${ws.name} › Personas`} title="Personas" />
      <main className="mx-auto grid max-w-5xl gap-12 px-7 py-10 lg:grid-cols-[1fr_340px]">
        <section aria-labelledby="personas-h">
          <h2 id="personas-h" className="label-up mb-3">
            Personas
          </h2>
          <p className="text-muted mb-6 font-medium">
            The people your maps serve. Link them to objectives from the objective's header on a map.
            {!canEdit && ' Editors can change them.'}
          </p>
          <ErrorAlert error={write.error} />
          {personas.isPending ? (
            <Loading />
          ) : personas.isError ? (
            <ErrorAlert error={personas.error} />
          ) : personas.data.length === 0 ? (
            <p className="text-muted font-medium">No personas yet.</p>
          ) : (
            <ul className="rows" aria-label="Personas">
              {personas.data.map((p) => (
                <PersonaRow key={p.id} ws={ws} persona={p} canEdit={canEdit} busy={write.isPending} write={write.mutate} />
              ))}
            </ul>
          )}
        </section>
        {canEdit && <CreatePersona ws={ws} write={write.mutateAsync} />}
      </main>
    </>
  )
}

function PersonaRow(props: { ws: Membership; persona: Persona; canEdit: boolean; busy: boolean; write: (w: () => Promise<unknown>) => void }) {
  const { ws, persona: p, canEdit, write } = props
  return (
    <li>
      {canEdit ? (
        <input
          type="color"
          className="h-8 w-8 cursor-pointer"
          aria-label={`Colour for ${p.name}`}
          defaultValue={p.colour.toLowerCase()}
          // The native change event fires once, when the picker closes; React's onChange fires on every move.
          ref={(el) => {
            if (el) el.onchange = () => write(() => api.patchPersona(ws.slug, p.id, { colour: el.value.toUpperCase() }))
          }}
        />
      ) : (
        <span className="h-8 w-8 rounded-full" style={{ background: p.colour }} aria-hidden />
      )}
      <span className="min-w-0 flex-1">
        <span className="block font-extrabold">
          <InlineText
            value={p.name}
            label={`persona ${p.name}`}
            editable={canEdit}
            maxLength={100}
            onSave={(name) => write(() => api.patchPersona(ws.slug, p.id, { name }))}
          />
        </span>
        <span className="text-muted block text-sm font-medium">
          {canEdit ? (
            <InlineText
              value={p.description || 'Add a description'}
              label={`description of ${p.name}`}
              editable
              maxLength={2000}
              onSave={(description) => {
                if (description !== 'Add a description') write(() => api.patchPersona(ws.slug, p.id, { description }))
              }}
            />
          ) : (
            p.description
          )}
        </span>
      </span>
      {canEdit && (
        <ConfirmButton
          label="Delete"
          confirm="Unlink from every objective"
          name={p.name}
          busy={props.busy}
          onConfirm={() => write(() => api.deletePersona(ws.slug, p.id))}
        />
      )}
    </li>
  )
}

function CreatePersona({ ws, write }: { ws: Membership; write: (w: () => Promise<unknown>) => Promise<unknown> }) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  return (
    <section aria-labelledby="persona-create" className="bg-base-200 border-line self-start border p-6">
      <h2 id="persona-create" className="up mb-5 text-xl">
        New persona
      </h2>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const n = name.trim()
          if (!n) return
          void write(() => api.createPersona(ws.slug, { name: n, description: description.trim(), colour: colourFor(n) })).then(() => {
            setName('')
            setDescription('')
          })
        }}
      >
        <Field label="Persona name">
          <input className="input w-full" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Description" hint="Who they are and what they need.">
          <textarea
            className="textarea w-full"
            maxLength={2000}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>
        <button className="btn btn-primary mt-2 w-full">Create persona</button>
      </form>
    </section>
  )
}
