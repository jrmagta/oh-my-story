import { useCallback, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate, useParams } from 'react-router'
import { api, ApiError, type Membership } from '../api'
import { refreshSession, useMe, useSession } from '../session'
import { Avatar, ErrorAlert, Field, Loading, Logo, Stripe, ThemeToggle } from '../ui'
import { ProjectKeyField } from './project-key'
import { SearchPalette } from './search'

/** Gate for every signed-in screen: sends visitors to setup or sign-in. */
export function RequireAuth() {
  const me = useMe()
  const location = useLocation()
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setup, enabled: me.data === null })

  if (me.isPending) return <Loading />
  if (me.isError) return <ErrorAlert error={me.error} />
  if (me.data === null) {
    if (setup.isPending) return <Loading />
    if (setup.data?.needsBootstrap) return <Navigate to="/setup" replace />
    const next = location.pathname + location.search
    return <Navigate to={next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`} replace />
  }
  return <Outlet />
}

function UserMenu() {
  const { user } = useSession()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const signOut = async () => {
    await api.logout()
    qc.clear()
    await refreshSession(qc)
    navigate('/login', { replace: true })
  }
  return (
    <div className="dropdown dropdown-end">
      <button type="button" tabIndex={0} className="flex items-center gap-3" aria-label={`Account: ${user.displayName}`}>
        <Avatar id={user.id} name={user.displayName} size={34} />
      </button>
      <div tabIndex={0} className="dropdown-content bg-base-100 text-base-content border-card-line z-10 mt-3 w-64 border-2 p-4">
        <p className="font-extrabold">{user.displayName}</p>
        <p className="text-muted mb-4 text-sm font-medium break-all">{user.email}</p>
        <button type="button" className="btn btn-sm btn-ol w-full" onClick={signOut}>
          Sign out
        </button>
      </div>
    </div>
  )
}

/** Nav, stripe and page for signed-in screens. Inside a workspace the nav shows its sections. */
export function AppShell() {
  const { slug } = useParams()
  const { workspaces } = useSession()
  const current = slug ? workspaces.find((w) => w.slug === slug) : undefined
  return (
    <>
      <header className="topnav">
        <Logo />
        {current && (
          <nav className="flex items-center gap-7" aria-label="Workspace">
            <Link to="/" className="navlink text-[#A1A1AA]">
              {current.name}
            </Link>
            <NavLink to={`/w/${current.slug}/maps`} className="navlink">
              Maps
            </NavLink>
            <NavLink to={`/w/${current.slug}/members`} className="navlink">
              Members
            </NavLink>
            <NavLink to={`/w/${current.slug}/workflows`} className="navlink">
              Workflows
            </NavLink>
            <NavLink to={`/w/${current.slug}/personas`} className="navlink">
              Personas
            </NavLink>
            <NavLink to={`/w/${current.slug}/labels`} className="navlink">
              Labels
            </NavLink>
            <NavLink to={`/w/${current.slug}/settings`} className="navlink">
              Settings
            </NavLink>
          </nav>
        )}
        <div className="flex-1" />
        {current && <SearchPalette slug={current.slug} />}
        <ThemeToggle />
        <UserMenu />
      </header>
      <Stripe />
      <Outlet />
    </>
  )
}

export function PageBand({ kicker, title, children }: { kicker: string; title: string; children?: ReactNode }) {
  return (
    <section className="band">
      <div className="mx-auto flex max-w-5xl items-end gap-6 px-7">
        <div className="flex-1">
          <p className="label-up">{kicker}</p>
          <h1 className="mt-1">{title}</h1>
        </div>
        {children}
      </div>
    </section>
  )
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
    .replace(/-+$/, '')
}

export function WorkspacesPage() {
  const { user, workspaces } = useSession()
  return (
    <>
      <PageBand kicker={`Signed in as ${user.displayName}`} title="Workspaces" />
      <main className="mx-auto grid max-w-5xl gap-12 px-7 py-10 lg:grid-cols-[1fr_340px]">
        <section aria-labelledby="ws-list">
          <h2 id="ws-list" className="label-up mb-3">
            Your workspaces
          </h2>
          {workspaces.length === 0 ? (
            <p className="text-muted font-medium">
              You're not in any workspace yet. Create one, or ask an admin to invite you to theirs.
            </p>
          ) : (
            <ul className="rows" aria-label="Workspaces">
              {workspaces.map((w) => (
                <WorkspaceRow key={w.id} w={w} />
              ))}
            </ul>
          )}
        </section>
        <CreateWorkspace />
      </main>
    </>
  )
}

function WorkspaceRow({ w }: { w: Membership }) {
  return (
    <li>
      <div className="min-w-0 flex-1">
        <Link to={`/w/${w.slug}/maps`} className="up block truncate text-2xl hover:underline">
          {w.name}
        </Link>
        <span className="text-muted text-sm font-medium">/{w.slug}</span>
      </div>
      <span className="label-up">{w.role}</span>
      <Link to={`/w/${w.slug}/maps`} className="btn btn-sm btn-ol" aria-label={`Open ${w.name}`}>
        Open
      </Link>
    </li>
  )
}

function CreateWorkspace() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugEdited, setSlugEdited] = useState(false)
  const [key, setKey] = useState('')
  const [keyEdited, setKeyEdited] = useState(false)
  const [keyOk, setKeyOk] = useState(false)
  const onKey = useCallback((k: string, edited: boolean) => {
    setKey(k)
    if (edited) setKeyEdited(true)
  }, [])
  const create = useMutation({
    mutationFn: () => api.createWorkspace({ name, slug, keyPrefix: key }),
    onSuccess: async (w) => {
      await refreshSession(qc)
      navigate(`/w/${w.slug}/maps`)
    },
  })
  return (
    <section aria-labelledby="ws-create" className="bg-base-200 border-line self-start border p-6">
      <h2 id="ws-create" className="up mb-5 text-xl">
        New workspace
      </h2>
      <ErrorAlert error={create.error} />
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Workspace name">
          <input
            className="input w-full"
            required
            maxLength={100}
            value={name}
            onChange={(e) => {
              setName(e.target.value)
              if (!slugEdited) setSlug(slugify(e.target.value))
            }}
          />
        </Field>
        <Field label="URL slug" hint="Lowercase letters, digits and dashes.">
          <input
            className="input w-full"
            required
            pattern="[a-z0-9](?:[a-z0-9\-]{0,38}[a-z0-9])?"
            value={slug}
            onChange={(e) => {
              setSlug(e.target.value)
              setSlugEdited(true)
            }}
          />
        </Field>
        <ProjectKeyField name={name} value={key} edited={keyEdited} onChange={onKey} onAvailability={setKeyOk} />
        <button className="btn btn-primary mt-2 w-full" disabled={create.isPending || !keyOk}>
          Create workspace
        </button>
      </form>
    </section>
  )
}

/** Loads the workspace for /w/:slug; non-members get the same "not found" the API gives. */
export function WorkspaceRoute() {
  const { slug = '' } = useParams()
  const ws = useQuery({ queryKey: ['ws', slug], queryFn: () => api.workspace(slug), retry: false })
  if (ws.isPending) return <Loading />
  if (ws.isError) {
    const notFound = ws.error instanceof ApiError && ws.error.status === 404
    return (
      <>
        <PageBand kicker="Workspace" title={notFound ? 'Not found' : 'Error'} />
        <main className="mx-auto max-w-5xl px-7 py-10">
          {notFound ? (
            <p className="text-muted mb-6 font-medium">This workspace doesn't exist, or you're not a member.</p>
          ) : (
            <ErrorAlert error={ws.error} />
          )}
          <Link className="btn btn-primary" to="/">
            Your workspaces
          </Link>
        </main>
      </>
    )
  }
  return <Outlet context={ws.data} />
}
