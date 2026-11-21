import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { Field } from '../ui'

export const keyPattern = /^[A-Z][A-Z0-9]{0,9}$/

/** value, once it has stopped changing for ms. */
function useSettled<T>(value: T, ms = 250): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return settled
}

/**
 * The project key field (CHC in CHC-12). Until the user types one, it shows
 * the server's suggestion for the workspace name. A typed key is checked as
 * they go; a taken one is flagged with a free alternative to use instead.
 * current is the workspace's own key, which counts as free.
 */
export function ProjectKeyField(props: {
  name: string
  value: string
  edited: boolean
  current?: string
  onChange: (key: string, edited: boolean) => void
  onAvailability: (ok: boolean) => void
}) {
  const { value, edited, current, onChange, onAvailability } = props
  const name = useSettled(props.name)
  const key = useSettled(value)
  const valid = keyPattern.test(value)
  const check = edited && valid && key === value && key !== current
  const q = useQuery({
    queryKey: ['project-keys', name, check ? key : ''],
    queryFn: () => api.projectKeys(name, check ? key : undefined),
    enabled: name.trim() !== '' && (!edited || check),
    staleTime: 5000,
  })
  const suggestion = q.data?.suggestion
  useEffect(() => {
    if (edited) return
    if (!props.name.trim()) {
      if (value) onChange('', false)
    } else if (suggestion && suggestion !== value) onChange(suggestion, false)
  }, [edited, suggestion, value, onChange, props.name])
  const taken = check && q.data?.available === false
  const ok = valid && (!edited || value === current || (check && q.data?.available === true))
  useEffect(() => onAvailability(ok), [ok, onAvailability])

  return (
    <Field label="Project key" hint={`Step IDs look like ${value || 'KEY'}-1. Unique on this server.`}>
      <input
        className={`input w-full font-extrabold uppercase ${taken || (value && !valid) ? 'input-error' : ''}`}
        required
        maxLength={10}
        pattern="[A-Z][A-Z0-9]{0,9}"
        aria-invalid={taken || (!!value && !valid)}
        aria-describedby="project-key-status"
        value={value}
        onChange={(e) => onChange(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ''), true)}
      />
      <span id="project-key-status" role="status" className="mt-1 block text-[13px] font-bold">
        {value && !valid && <span className="text-error">Start with a letter; letters and digits only.</span>}
        {taken && (
          <span className="text-error">
            {value} is taken.{' '}
            {suggestion && (
              <button type="button" className="link font-extrabold" onClick={() => onChange(suggestion, true)}>
                Use {suggestion}
              </button>
            )}
          </span>
        )}
      </span>
    </Field>
  )
}
