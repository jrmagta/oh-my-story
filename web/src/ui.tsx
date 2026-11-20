import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { ApiError } from './api'

type Theme = 'blink-dark' | 'blink-light'

function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'blink-light' ? 'blink-light' : 'blink-dark'
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(currentTheme)
  const next: Theme = theme === 'blink-dark' ? 'blink-light' : 'blink-dark'
  const label = next === 'blink-light' ? 'Light' : 'Dark'
  return (
    <button
      type="button"
      className="btn btn-sm btn-ol on-dark"
      aria-label={`Switch to ${label.toLowerCase()} theme`}
      onClick={() => {
        document.documentElement.dataset.theme = next
        try {
          localStorage.setItem('oms-theme', next)
        } catch {
          // Private windows may refuse storage; the switch still applies to this page.
        }
        setTheme(next)
      }}
    >
      {label}
    </button>
  )
}

export function Logo() {
  return (
    <Link to="/" className="logo" aria-label="Oh My Story home">
      Oh My Story
      <i aria-hidden style={{ color: 'var(--color-brand-red)', background: 'var(--color-brand-red)' }} />
      <i aria-hidden style={{ color: 'var(--color-brand-amber)', background: 'var(--color-brand-amber)' }} />
      <i aria-hidden style={{ color: 'var(--color-brand-green)', background: 'var(--color-brand-green)' }} />
    </Link>
  )
}

export function Stripe() {
  return (
    <div className="stripe" aria-hidden>
      <b />
      <b />
      <b />
    </div>
  )
}

const avatarColours = ['amber', 'green', 'red'] as const

function initials(name: string): string {
  const parts = name.replace(/@.*/, '').split(/[\s._-]+/).filter(Boolean)
  return ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? '') : '')).toUpperCase()
}

export function Avatar({ id, name, size = 30 }: { id: string; name: string; size?: number }) {
  let h = 0
  for (const ch of id) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return (
    <span
      className={`ring-disc ${avatarColours[h % avatarColours.length]}`}
      style={{ width: size, height: size, fontSize: Math.round(size * 0.37) }}
      aria-hidden
    >
      {initials(name)}
    </span>
  )
}

/** Logo-only chrome for the sign-in, setup and invite screens. */
export function AuthShell({ kicker, title, children }: { kicker: string; title: string; children: ReactNode }) {
  return (
    <>
      <header className="topnav">
        <Logo />
        <div className="flex-1" />
        <ThemeToggle />
      </header>
      <Stripe />
      <main className="mx-auto w-full max-w-[440px] px-4 py-14">
        <p className="label-up">{kicker}</p>
        <h1 className="up mt-1 mb-8 text-[40px] leading-none tracking-tight">{title}</h1>
        {children}
      </main>
    </>
  )
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="mb-4 block">
      <span className="label-up mb-1.5 block">{label}</span>
      {children}
      {hint && <span className="text-muted mt-1 block text-[13px] font-medium">{hint}</span>}
    </label>
  )
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message
  return 'Something went wrong. Check your connection and try again.'
}

export function ErrorAlert({ error }: { error: unknown }) {
  if (!error) return null
  return (
    <div role="alert" className="bg-brand-red mb-5 px-4 py-3 font-bold text-white">
      {typeof error === 'string' ? error : errorMessage(error)}
    </div>
  )
}

export function Notice({ children }: { children: ReactNode }) {
  return (
    <div role="status" className="bg-brand-green mb-5 px-4 py-3 font-bold text-white">
      {children}
    </div>
  )
}

export function Loading() {
  return (
    <div className="flex justify-center py-24" role="status" aria-label="Loading">
      <span className="loading loading-spinner loading-lg text-primary" />
    </div>
  )
}

/** Two clicks: the first arms the button for a few seconds, the second acts. */
export function ConfirmButton(props: { label: string; confirm: string; name: string; busy: boolean; onConfirm: () => void }) {
  const [armed, setArmed] = useState(false)
  useEffect(() => {
    if (!armed) return
    const t = setTimeout(() => setArmed(false), 4000)
    return () => clearTimeout(t)
  }, [armed])
  return (
    <button
      type="button"
      className={`btn btn-sm ${armed ? 'btn-error' : 'btn-ol'}`}
      aria-label={armed ? `${props.confirm}: ${props.name}` : `${props.label} ${props.name}`}
      disabled={props.busy}
      onClick={() => (armed ? props.onConfirm() : setArmed(true))}
    >
      {armed ? props.confirm : props.label}
    </button>
  )
}

/**
 * Text that becomes an input when editable and clicked. Enter or leaving the
 * field saves; Escape cancels. Saving an empty or unchanged value does nothing.
 */
export function InlineText(props: {
  value: string
  label: string
  editable: boolean
  onSave: (v: string) => void
  className?: string
  maxLength?: number
}) {
  const [editing, setEditing] = useState(false)
  if (!props.editable) return <span className={props.className}>{props.value}</span>
  if (!editing) {
    return (
      <button
        type="button"
        className={`inline-edit ${props.className ?? ''}`}
        aria-label={`Rename ${props.label}`}
        onClick={() => setEditing(true)}
      >
        {props.value}
      </button>
    )
  }
  return (
    <TextInput
      label={`New name for ${props.label}`}
      initial={props.value}
      maxLength={props.maxLength ?? 500}
      className={props.className}
      onDone={(v) => {
        setEditing(false)
        if (v !== null && v !== props.value) props.onSave(v)
      }}
    />
  )
}

/** A one-line input that reports its trimmed value on Enter or blur, or null on Escape or when empty. */
export function TextInput(props: {
  label: string
  initial?: string
  placeholder?: string
  maxLength?: number
  className?: string
  keepOpen?: boolean
  onDone: (v: string | null) => void
}) {
  const [value, setValue] = useState(props.initial ?? '')
  const done = useRef(false)
  const finish = (v: string | null) => {
    if (done.current) return
    const t = v?.trim() ?? ''
    if (props.keepOpen && v !== null && t) {
      props.onDone(t)
      setValue('')
      return
    }
    done.current = true
    props.onDone(t ? t : null)
  }
  return (
    <input
      autoFocus
      className={`input input-sm w-full ${props.className ?? ''}`}
      aria-label={props.label}
      placeholder={props.placeholder}
      maxLength={props.maxLength ?? 500}
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onBlur={() => finish(props.keepOpen ? null : value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          finish(value)
        } else if (e.key === 'Escape') {
          finish(null)
        }
      }}
    />
  )
}

export interface ToastMessage {
  id: number
  text: string
  tone: 'info' | 'error'
  action?: { label: string; run: () => void }
}

/** Bottom-left notices that disappear after a few seconds. */
export function Toasts({ items, dismiss }: { items: ToastMessage[]; dismiss: (id: number) => void }) {
  return (
    <div className="toasts" aria-live="polite">
      {items.map((t) => (
        <Toast key={t.id} t={t} dismiss={dismiss} />
      ))}
    </div>
  )
}

function Toast({ t, dismiss }: { t: ToastMessage; dismiss: (id: number) => void }) {
  useEffect(() => {
    const timer = setTimeout(() => dismiss(t.id), t.tone === 'error' ? 8000 : 6000)
    return () => clearTimeout(timer)
  }, [t, dismiss])
  return (
    <div role={t.tone === 'error' ? 'alert' : 'status'} className={`toast-item ${t.tone}`}>
      <span className="flex-1">{t.text}</span>
      {t.action && (
        <button
          type="button"
          className="btn btn-xs btn-ol"
          onClick={() => {
            t.action?.run()
            dismiss(t.id)
          }}
        >
          {t.action.label}
        </button>
      )}
    </div>
  )
}
