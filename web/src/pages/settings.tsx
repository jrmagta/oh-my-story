import { useCallback, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useOutletContext } from 'react-router'
import { api, atLeast, type Membership } from '../api'
import { refreshSession } from '../session'
import { ErrorAlert, Field, Notice } from '../ui'
import { PageBand } from './app'
import { ProjectKeyField } from './project-key'
import { ApiTokens } from './api-tokens'
import { Webhooks } from './webhooks'

/** Workspace name, step key prefix and estimate unit. Admins change them; everyone else reads. */
export function SettingsPage() {
  const ws = useOutletContext<Membership>()
  const qc = useQueryClient()
  const canManage = atLeast(ws.role, 'admin')
  const [name, setName] = useState(ws.name)
  const [keyPrefix, setKeyPrefix] = useState(ws.keyPrefix)
  const [keyOk, setKeyOk] = useState(true)
  const onKey = useCallback((k: string) => setKeyPrefix(k), [])
  const [estimateUnit, setEstimateUnit] = useState(ws.estimateUnit)
  const save = useMutation({
    mutationFn: () => api.updateWorkspace(ws.slug, { name, keyPrefix, estimateUnit }),
    onSuccess: async (updated) => {
      qc.setQueryData(['ws', ws.slug], updated)
      await refreshSession(qc)
    },
  })
  return (
    <>
      <PageBand kicker={`${ws.name} › Settings`} title="Settings" />
      <main className="mx-auto max-w-xl px-7 py-10">
        <ErrorAlert error={save.error} />
        {save.isSuccess && <Notice>Settings saved.</Notice>}
        <form
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <fieldset disabled={!canManage}>
            <Field label="Workspace name">
              <input className="input w-full" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <ProjectKeyField
              name={name}
              value={keyPrefix}
              edited
              current={ws.keyPrefix}
              onChange={onKey}
              onAvailability={setKeyOk}
            />
            <p className="text-muted -mt-2 mb-4 text-[13px] font-medium">Existing steps keep their IDs; new steps use the new key.</p>
            <Field label="Estimate unit" hint="What estimates count, such as points or days.">
              <input
                className="input w-full"
                required
                maxLength={20}
                value={estimateUnit}
                onChange={(e) => setEstimateUnit(e.target.value)}
              />
            </Field>
          </fieldset>
          {canManage ? (
            <button className="btn btn-primary mt-2" disabled={save.isPending || !keyOk}>
              Save settings
            </button>
          ) : (
            <p className="text-muted font-medium">Only admins change these settings.</p>
          )}
        </form>
        {canManage && <ApiTokens ws={ws} />}
        {canManage && <Webhooks ws={ws} />}
      </main>
    </>
  )
}
