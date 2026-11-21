// Client for the HTTP API in docs/API.md.

import type { HistoryPage } from './map/history'
import type { Filter } from './map/filter'

export type Role = 'owner' | 'admin' | 'editor' | 'commenter' | 'viewer'

export const roles: readonly Role[] = ['owner', 'admin', 'editor', 'commenter', 'viewer']

const rank: Record<Role, number> = { viewer: 1, commenter: 2, editor: 3, admin: 4, owner: 5 }

export function atLeast(role: Role, minimum: Role): boolean {
  return rank[role] >= rank[minimum]
}

export interface User {
  id: string
  email: string
  displayName: string
}

export interface Workspace {
  id: string
  name: string
  slug: string
  keyPrefix: string
  estimateUnit: string
}

export interface Membership extends Workspace {
  role: Role
}

export interface Me {
  user: User
  workspaces: Membership[]
}

export interface Member {
  userId: string
  email: string
  displayName: string
  role: Role
}

export interface Invite {
  id: string
  workspaceName: string
  email: string
  role: Role
  expiresAt: string
}

export interface InvitePreview {
  workspaceName: string
  email: string
  role: Role
}

export interface Providers {
  oidc: { enabled: false } | { enabled: true; name: string }
}

export type Category = 'todo' | 'doing' | 'done' | 'blocked'

export interface Status {
  id: string
  workflowId: string
  name: string
  category: Category
  colour: string | null
  rank: string
}

export interface Workflow {
  id: string
  name: string
  isDefault: boolean
  statuses: Status[]
}

export interface StoryMap {
  id: string
  workflowId: string
  name: string
  description: string
}

export type Level = 'objective' | 'activity'

export interface MapNode {
  id: string
  parentId: string | null
  level: Level
  title: string
  description: string
  colour: string | null
  /** Objectives only (activities have none); sorted. Names are in MapView.personas. */
  personaIds: string[]
  rank: string
  version: number
}

/** Someone a map serves; shared by the workspace's maps and linked to objectives. */
export interface Persona {
  id: string
  name: string
  description: string
  colour: string
}

export interface Release {
  id: string
  name: string
  description: string
  targetDate: string | null
  rank: string
}

export interface Step {
  id: string
  key: string
  title: string
  descriptionMd: string
  statusId: string
  assigneeId: string | null
  /** In the workspace's estimateUnit; null when not estimated. */
  estimate: number | null
  /** Sorted; names and colours are in MapView.labels. */
  labelIds: string[]
  version: number
}

/** Shared by every map of the workspace. */
export interface Label {
  id: string
  name: string
  colour: string
}

export interface Placement {
  id: string
  stepId: string
  activityId: string
  releaseId: string | null
  rank: string
}

export interface MapView {
  map: StoryMap
  workflow: Workflow
  labels: Label[]
  personas: Persona[]
  nodes: MapNode[]
  releases: Release[]
  steps: Step[]
  placements: Placement[]
  lastEventId: number
}

/** A named map filter: private to its creator until shared with the workspace. */
export interface SavedView {
  id: string
  name: string
  filter: Filter
  shared: boolean
  createdBy: string
  creatorName: string
}

/** Where a new or moved item goes: next to one sibling, or last when both are absent. */
export interface Position {
  afterId?: string
  beforeId?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(method: 'GET' | 'POST' | 'PATCH' | 'DELETE', path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin' }
  if (method !== 'GET') {
    // The server's CSRF guard requires JSON on every write, even an empty one.
    init.headers = { 'Content-Type': 'application/json' }
    if (body !== undefined) init.body = JSON.stringify(body)
  }
  const res = await fetch(path, init)
  if (res.status === 204) return undefined as T
  const data: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const e = (data as { error?: { code?: string; message?: string } } | null)?.error
    throw new ApiError(res.status, e?.code ?? 'internal', e?.message ?? `request failed (${res.status})`)
  }
  return data as T
}

const ws = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`
const inv = (token: string) => `/api/invites/${encodeURIComponent(token)}`
const mp = (slug: string, mapId: string) => `${ws(slug)}/maps/${encodeURIComponent(mapId)}`

export const api = {
  setup: () => request<{ needsBootstrap: boolean }>('GET', '/api/setup'),
  bootstrap: (b: { email: string; displayName: string; password: string }) => request<User>('POST', '/api/setup', b),
  login: (b: { email: string; password: string }) => request<User>('POST', '/api/auth/login', b),
  logout: () => request<void>('POST', '/api/auth/logout', {}),
  providers: () => request<Providers>('GET', '/api/auth/providers'),
  /** Always succeeds; an email goes out only if a local account has that address. */
  requestReset: (b: { email: string }) => request<void>('POST', '/api/auth/reset', b),
  resetPreview: (token: string) => request<{ email: string }>('GET', `/api/auth/reset/${encodeURIComponent(token)}`),
  /** Sets the password, ends the user's other sessions and signs this browser in. */
  resetPassword: (token: string, b: { password: string }) =>
    request<User>('POST', `/api/auth/reset/${encodeURIComponent(token)}`, b),

  /** The signed-in user, or null when there is no session. */
  me: async (): Promise<Me | null> => {
    try {
      return await request<Me>('GET', '/api/me')
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return null
      throw e
    }
  },

  /** keyPrefix is the project key (CHC in CHC-12); left out, the server suggests one. 409 names a taken slug or key. */
  createWorkspace: (b: { name: string; slug: string; keyPrefix?: string }) => request<Membership>('POST', '/api/workspaces', b),
  /** A free project key for a workspace name, and whether key is free (when given). */
  projectKeys: (name: string, key?: string) =>
    request<{ suggestion: string; available?: boolean }>(
      'GET',
      `/api/project-keys?${new URLSearchParams({ name, ...(key ? { key } : {}) }).toString()}`,
    ),
  workspace: (slug: string) => request<Membership>('GET', ws(slug)),
  updateWorkspace: (slug: string, b: { name?: string; keyPrefix?: string; estimateUnit?: string }) =>
    request<Membership>('PATCH', ws(slug), b),
  members: (slug: string) => request<Member[]>('GET', `${ws(slug)}/members`),
  setRole: (slug: string, userId: string, role: Role) =>
    request<Member>('PATCH', `${ws(slug)}/members/${encodeURIComponent(userId)}`, { role }),
  removeMember: (slug: string, userId: string) =>
    request<void>('DELETE', `${ws(slug)}/members/${encodeURIComponent(userId)}`),
  invites: (slug: string) => request<Invite[]>('GET', `${ws(slug)}/invites`),
  invite: (slug: string, b: { email: string; role: Role }) => request<Invite>('POST', `${ws(slug)}/invites`, b),
  revokeInvite: (slug: string, id: string) => request<void>('DELETE', `${ws(slug)}/invites/${encodeURIComponent(id)}`),

  workflows: (slug: string) => request<Workflow[]>('GET', `${ws(slug)}/workflows`),
  labels: (slug: string) => request<Label[]>('GET', `${ws(slug)}/labels`),
  /** 409 when a label of that name exists, ignoring case. */
  createLabel: (slug: string, b: { name: string; colour?: string }) => request<Label>('POST', `${ws(slug)}/labels`, b),
  patchLabel: (slug: string, id: string, b: { name?: string; colour?: string }) =>
    request<Label>('PATCH', `${ws(slug)}/labels/${encodeURIComponent(id)}`, b),
  deleteLabel: (slug: string, id: string) => request<void>('DELETE', `${ws(slug)}/labels/${encodeURIComponent(id)}`),
  personas: (slug: string) => request<Persona[]>('GET', `${ws(slug)}/personas`),
  /** 409 when a persona of that name exists, ignoring case. */
  createPersona: (slug: string, b: { name: string; description?: string; colour?: string }) =>
    request<Persona>('POST', `${ws(slug)}/personas`, b),
  patchPersona: (slug: string, id: string, b: { name?: string; description?: string; colour?: string }) =>
    request<Persona>('PATCH', `${ws(slug)}/personas/${encodeURIComponent(id)}`, b),
  deletePersona: (slug: string, id: string) => request<void>('DELETE', `${ws(slug)}/personas/${encodeURIComponent(id)}`),
  createWorkflow: (slug: string, b: { name: string }) => request<Workflow>('POST', `${ws(slug)}/workflows`, b),
  renameWorkflow: (slug: string, id: string, b: { name: string }) =>
    request<Workflow>('PATCH', `${ws(slug)}/workflows/${encodeURIComponent(id)}`, b),
  createStatus: (slug: string, workflowId: string, b: { name: string; category: Category; colour?: string } & Position) =>
    request<Status>('POST', `${ws(slug)}/workflows/${encodeURIComponent(workflowId)}/statuses`, b),
  patchStatus: (slug: string, id: string, b: { name?: string; category?: Category; colour?: string }) =>
    request<Status>('PATCH', `${ws(slug)}/statuses/${encodeURIComponent(id)}`, b),
  moveStatus: (slug: string, id: string, b: Position) => request<Status>('POST', `${ws(slug)}/statuses/${encodeURIComponent(id)}/move`, b),
  /** Steps in the status move to the replacement, another status of the same workflow. */
  deleteStatus: (slug: string, id: string, replacementId: string) =>
    request<void>('DELETE', `${ws(slug)}/statuses/${encodeURIComponent(id)}?replacementId=${encodeURIComponent(replacementId)}`),
  maps: (slug: string) => request<StoryMap[]>('GET', `${ws(slug)}/maps`),
  createMap: (slug: string, b: { name: string; description?: string; workflowId?: string }) =>
    request<StoryMap>('POST', `${ws(slug)}/maps`, b),
  map: (slug: string, mapId: string) => request<MapView>('GET', mp(slug, mapId)),
  patchMap: (slug: string, mapId: string, b: { name?: string; description?: string }) =>
    request<StoryMap>('PATCH', mp(slug, mapId), b),
  deleteMap: (slug: string, mapId: string) => request<void>('DELETE', mp(slug, mapId)),

  /** before: the next cursor of the previous page; omit for the newest. */
  mapHistory: (slug: string, mapId: string, before?: number) =>
    request<HistoryPage>('GET', `${mp(slug, mapId)}/history${before ? `?before=${before}` : ''}`),
  stepHistory: (slug: string, mapId: string, id: string, before?: number) =>
    request<HistoryPage>('GET', `${mp(slug, mapId)}/steps/${id}/history${before ? `?before=${before}` : ''}`),

  createNode: (slug: string, mapId: string, b: { level: Level; title: string; parentId?: string } & Position) =>
    request<MapNode>('POST', `${mp(slug, mapId)}/nodes`, b),
  patchNode: (slug: string, mapId: string, id: string, b: { title?: string; description?: string; colour?: string | null; personaIds?: string[] }) =>
    request<MapNode>('PATCH', `${mp(slug, mapId)}/nodes/${id}`, b),
  moveNode: (slug: string, mapId: string, id: string, b: { parentId: string | null } & Position) =>
    request<MapNode>('POST', `${mp(slug, mapId)}/nodes/${id}/move`, b),
  deleteNode: (slug: string, mapId: string, id: string) => request<void>('DELETE', `${mp(slug, mapId)}/nodes/${id}`),
  restoreNode: (slug: string, mapId: string, id: string) =>
    request<MapNode>('POST', `${mp(slug, mapId)}/nodes/${id}/restore`, {}),

  createRelease: (slug: string, mapId: string, b: { name: string; targetDate?: string } & Position) =>
    request<Release>('POST', `${mp(slug, mapId)}/releases`, b),
  patchRelease: (slug: string, mapId: string, id: string, b: { name?: string; targetDate?: string | null }) =>
    request<Release>('PATCH', `${mp(slug, mapId)}/releases/${id}`, b),
  moveRelease: (slug: string, mapId: string, id: string, b: Position) =>
    request<Release>('POST', `${mp(slug, mapId)}/releases/${id}/move`, b),
  deleteRelease: (slug: string, mapId: string, id: string) =>
    request<void>('DELETE', `${mp(slug, mapId)}/releases/${id}`),

  createStep: (slug: string, mapId: string, b: { title: string; activityId: string; releaseId: string | null } & Position) =>
    request<{ step: Step; placement: Placement }>('POST', `${mp(slug, mapId)}/steps`, b),
  patchStep: (
    slug: string,
    mapId: string,
    id: string,
    b: {
      title?: string
      descriptionMd?: string
      statusId?: string
      assigneeId?: string | null
      estimate?: number | null
      /** Replaces the step's labels. */
      labelIds?: string[]
    },
  ) => request<Step>('PATCH', `${mp(slug, mapId)}/steps/${id}`, b),
  views: (slug: string, mapId: string) => request<SavedView[]>('GET', `${mp(slug, mapId)}/views`),
  /** shared: true needs editor. */
  createView: (slug: string, mapId: string, b: { name: string; filter: Filter; shared?: boolean }) =>
    request<SavedView>('POST', `${mp(slug, mapId)}/views`, b),
  /** Only the creator; sharing needs editor. */
  patchView: (slug: string, mapId: string, id: string, b: { name?: string; filter?: Filter; shared?: boolean }) =>
    request<SavedView>('PATCH', `${mp(slug, mapId)}/views/${encodeURIComponent(id)}`, b),
  /** The creator, or an admin for a shared view. */
  deleteView: (slug: string, mapId: string, id: string) => request<void>('DELETE', `${mp(slug, mapId)}/views/${encodeURIComponent(id)}`),
  /** version is the step version the client showed; stale says someone changed it since. */
  moveStep: (slug: string, mapId: string, id: string, b: { activityId: string; releaseId: string | null; version: number } & Position) =>
    request<{ placement: Placement; stale: boolean }>('POST', `${mp(slug, mapId)}/steps/${id}/move`, b),
  deleteStep: (slug: string, mapId: string, id: string) => request<void>('DELETE', `${mp(slug, mapId)}/steps/${id}`),
  restoreStep: (slug: string, mapId: string, id: string) =>
    request<Step>('POST', `${mp(slug, mapId)}/steps/${id}/restore`, {}),

  invitePreview: (token: string) => request<InvitePreview>('GET', inv(token)),
  acceptInvite: (token: string, newAccount?: { displayName: string; password: string }) =>
    request<{ workspace: Workspace; user?: User }>('POST', `${inv(token)}/accept`, newAccount ?? {}),
}

/** A same-site path to return to after signing in; anything else becomes "/". */
export function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/\\') ? next : '/'
}
