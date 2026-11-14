import { useState, type ReactNode } from 'react'
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
