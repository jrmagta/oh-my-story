import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Membership, type Webhook } from '../api'
import { ConfirmButton, ErrorAlert } from '../ui'
import { ago } from '../map/history'

/** The event types a webhook can ask for, grouped as people think of them. */
const groups: [string, string[]][] = [
  ['Steps', ['step.create', 'step.update', 'step.delete', 'step.restore', 'placement.move', 'step.place', 'step.unplace']],
  ['Comments', ['comment.create', 'comment.update', 'comment.delete']],
  ['Attachments', ['attachment.create', 'attachment.delete', 'attachment.restore']],
  ['Dependencies', ['dependency.create', 'dependency.delete']],
  ['Backbone', ['node.create', 'node.update', 'node.move', 'node.delete', 'node.restore']],
  ['Releases', ['release.create', 'release.update', 'release.move', 'release.delete']],
  ['Maps', ['map.create', 'map.update', 'map.delete']],
  ['Labels and personas', ['label.create', 'label.update', 'label.delete', 'persona.create', 'persona.update', 'persona.delete']],
  ['Workflows', ['workflow.create', 'workflow.update', 'status.create', 'status.update', 'status.move', 'status.delete']],
]

/**
 * Admins send the workspace's changes to other systems: a URL, the events it
 * wants, and a secret to check the X-OMS-Signature header with. Each webhook
 * lists its recent deliveries and can send a test.
 */
export function Webhooks({ ws }: { ws: Membership }) {
  const qc = useQueryClient()
  const key = ['ws', ws.slug, 'webhooks']
  const hooks = useQuery({ queryKey: key, queryFn: () => api.webhooks(ws.slug) })
  const [url, setUrl] = useState('')
  const [all, setAll] = useState(true)
  const [events, setEvents] = useState<string[]>(['step.create', 'step.update'])
  const [made, setMade] = useState<{ url: string; secret: string } | null>(null)
  const create = useMutation({
    mutationFn: () => api.createWebhook(ws.slug, { url: url.trim(), events: all ? ['*'] : events }),
    onSuccess: (w) => {
      setMade({ url: w.url, secret: w.secret })
      setUrl('')
      void qc.invalidateQueries({ queryKey: key })
    },
  })
  return (
    <section className="mt-12" aria-labelledby="webhooks">
      <h2 id="webhooks" className="up text-xl">
        Webhooks
      </h2>
      <p className="text-muted mt-1 mb-4 text-sm font-medium">
        Send this workspace's changes to another system as they happen: a signed JSON <code>POST</code> per change, retried for
        about a day if it fails. Check the <code>X-OMS-Signature</code> header (<code>sha256=</code> HMAC of the body) with the
        webhook's secret.
      </p>
      <ErrorAlert error={create.error} />
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <label>
          <span className="label-up mb-1 block">Payload URL</span>
          <input
            className="input input-sm w-full"
            type="url"
            required
            placeholder="https://example.com/oms-hook"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
        </label>
        <fieldset>
          <legend className="label-up mb-1">Events</legend>
          <label className="flex items-center gap-2 text-sm font-semibold">
            <input type="radio" name="hook-events" className="radio radio-xs" checked={all} onChange={() => setAll(true)} />
            Everything
          </label>
          <label className="flex items-center gap-2 text-sm font-semibold">
            <input type="radio" name="hook-events" className="radio radio-xs" checked={!all} onChange={() => setAll(false)} />
            Only these
          </label>
          {!all && <EventPicker value={events} onChange={setEvents} />}
        </fieldset>
        <div>
          <button className="btn btn-sm btn-primary" disabled={create.isPending || !url.trim() || (!all && events.length === 0)}>
            Add webhook
          </button>
        </div>
      </form>
      {made && (
        <div className="share-made mt-4" role="status">
          <input className="input input-sm w-full font-mono" readOnly aria-label="Signing secret" value={made.secret} onFocus={(e) => e.target.select()} />
          <button type="button" className="btn btn-sm btn-ol" onClick={() => void navigator.clipboard?.writeText(made.secret)}>
            Copy
          </button>
          <p className="text-muted col-span-2 text-[13px] font-medium">
            The secret for {made.url}. Copy it now: it is shown only once.
          </p>
        </div>
      )}
      {hooks.data?.length === 0 && <p className="text-muted mt-4 text-sm font-medium">No webhooks yet.</p>}
      <ul className="mt-4 flex flex-col gap-3" aria-label="Webhooks">
        {hooks.data?.map((w) => <WebhookRow key={w.id} ws={ws} hook={w} />)}
      </ul>
    </section>
  )
}

function EventPicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  return (
    <div className="hook-events">
      {groups.map(([name, types]) => (
        <div key={name} role="group" aria-label={name}>
          <span className="text-muted text-xs font-extrabold uppercase">{name}</span>
          {types.map((t) => (
            <label key={t} className="flex items-center gap-2 text-[13px] font-semibold">
              <input
                type="checkbox"
                className="checkbox checkbox-xs"
                checked={value.includes(t)}
                onChange={() => onChange(value.includes(t) ? value.filter((x) => x !== t) : [...value, t])}
              />
              <code>{t}</code>
            </label>
          ))}
        </div>
      ))}
    </div>
  )
}

function WebhookRow({ ws, hook }: { ws: Membership; hook: Webhook }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const key = ['ws', ws.slug, 'webhooks']
  const dkey = [...key, hook.id, 'deliveries']
  // While open, the list follows deliveries as they are sent and retried.
  const deliveries = useQuery({ queryKey: dkey, queryFn: () => api.deliveries(ws.slug, hook.id), enabled: open, refetchInterval: open ? 3000 : false })
  // The switch moves at once; it goes back if the change fails.
  const [active, setActive] = useState(hook.active)
  useEffect(() => setActive(hook.active), [hook.active])
  const patch = useMutation({
    mutationFn: (b: { active: boolean }) => api.patchWebhook(ws.slug, hook.id, b),
    onError: () => setActive(hook.active),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  })
  const test = useMutation({
    mutationFn: () => api.testWebhook(ws.slug, hook.id),
    onSuccess: () => {
      setOpen(true)
      void qc.invalidateQueries({ queryKey: dkey })
    },
  })
  const remove = useMutation({
    mutationFn: () => api.deleteWebhook(ws.slug, hook.id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  })
  return (
    <li className="hook" aria-label={`Webhook ${hook.url}`}>
      <ErrorAlert error={patch.error ?? test.error ?? remove.error} />
      <div className="flex flex-wrap items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-sm font-semibold" title={hook.url}>
          {hook.url}
        </span>
        <label className="flex items-center gap-2 text-xs font-extrabold uppercase">
          <input
            type="checkbox"
            className="toggle toggle-xs"
            checked={active}
            onChange={(e) => {
              setActive(e.target.checked)
              patch.mutate({ active: e.target.checked })
            }}
          />
          Active
        </label>
      </div>
      <p className="text-muted text-[12px] font-medium">
        {hook.events.includes('*') ? 'Everything' : hook.events.join(', ')} · added {ago(hook.createdAt)}
      </p>
      <div className="mt-2 flex gap-2">
        <button type="button" className="btn btn-xs btn-ol" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
          {open ? 'Hide deliveries' : 'Deliveries'}
        </button>
        <button type="button" className="btn btn-xs btn-ol" disabled={test.isPending} onClick={() => test.mutate()}>
          Send test
        </button>
        <ConfirmButton label="Delete webhook" confirm="Confirm delete" name={hook.url} busy={remove.isPending} onConfirm={() => remove.mutate()} />
      </div>
      {open && (
        <table className="hook-deliveries mt-3" aria-label={`Deliveries to ${hook.url}`}>
          <thead>
            <tr>
              <th>Event</th>
              <th>Status</th>
              <th>Tries</th>
              <th>Result</th>
              <th>When</th>
            </tr>
          </thead>
          <tbody>
            {deliveries.data?.length === 0 && (
              <tr>
                <td colSpan={5} className="text-muted">
                  Nothing sent yet.
                </td>
              </tr>
            )}
            {deliveries.data?.map((d) => (
              <tr key={d.id}>
                <td>
                  <code>{d.type}</code>
                </td>
                <td>
                  <span className={`hook-status ${d.status}`}>{d.status === 'pending' && d.attempts > 0 ? 'retrying' : d.status}</span>
                </td>
                <td>{d.attempts}</td>
                <td className="truncate" title={d.lastError ?? undefined}>
                  {d.lastStatusCode ?? ''} {d.lastError && d.status !== 'sent' ? d.lastError : ''}
                </td>
                <td title={d.createdAt}>
                  {ago(d.createdAt)}
                  {d.status === 'pending' && d.nextAttemptAt && d.attempts > 0 && (
                    <span className="text-muted block text-[11px]">next {new Date(d.nextAttemptAt).toLocaleTimeString()}</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </li>
  )
}
