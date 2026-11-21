import { expect, test, type Page } from '@playwright/test'
import { inviteLink } from './mailpit'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const viewer = { email: `views-viewer+${Date.now()}@example.com`, name: 'Vic Viewer', password: 'viewer-password-123' }
const slug = 'views'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('a filter is saved as a view, shared with the workspace, and opened by someone else', async ({ page, browser }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Views', slug } })).status()).toBe(201)
  const call = async (method: 'post' | 'patch', path: string, data: object) => {
    const r = await page.request[method](path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string } }
  }
  const auth = await call('post', `${ws}/labels`, { name: 'auth' })
  const map = await call('post', `${ws}/maps`, { name: 'Accounts' })
  const api = `${ws}/maps/${map.id}`
  const obj = await call('post', `${api}/nodes`, { level: 'objective', title: 'Grow' })
  const act = await call('post', `${api}/nodes`, { level: 'activity', title: 'Sign up', parentId: obj.id })
  const s1 = await call('post', `${api}/steps`, { title: 'Email login', activityId: act.id, releaseId: null })
  await call('post', `${api}/steps`, { title: 'Pick a plan', activityId: act.id, releaseId: null })
  await call('patch', `${api}/steps/${s1.step.id}`, { labelIds: [auth.id] })
  const mapUrl = `/w/${slug}/maps/${map.id}`

  await page.goto(mapUrl)
  const bar = page.getByRole('region', { name: 'Filter' })
  const menu = bar.getByRole('group', { name: 'Saved views' })

  // Save a shared view of the label filter.
  await bar.getByRole('button', { name: 'Filter', exact: true }).click()
  await bar.getByRole('group', { name: 'Filter by' }).getByLabel('auth').check()
  await page.keyboard.press('Escape')
  await bar.getByRole('button', { name: 'Views' }).click()
  await menu.getByLabel('Name for this view').fill('Auth work')
  await menu.getByLabel('Share with the workspace').check()
  await menu.getByRole('button', { name: 'Save view' }).click()
  await expect(bar.getByRole('button', { name: 'View: Auth work' })).toBeVisible()
  await bar.getByRole('button', { name: 'View: Auth work' }).click()
  await expect(menu.getByRole('list', { name: 'Shared views' })).toContainText('Auth work')
  await page.keyboard.press('Escape')

  // And a private one of a text filter.
  await bar.getByRole('button', { name: 'Clear' }).click()
  await bar.getByLabel('Filter steps by key or title').fill('plan')
  await bar.getByRole('button', { name: 'Views' }).click()
  await menu.getByLabel('Name for this view').fill('Plans')
  await menu.getByRole('button', { name: 'Save view' }).click()
  await bar.getByRole('button', { name: 'View: Plans' }).click()
  await expect(menu.getByRole('list', { name: 'Your views' })).toContainText('Plans')

  // Picking a view applies its filter.
  await menu.getByRole('button', { name: 'Auth work', exact: true }).click()
  await expect(bar.getByText('Label: auth')).toBeVisible()
  await expect(bar.getByLabel('Filter steps by key or title')).toHaveValue('')
  await expect(bar.getByText('1 of 2 steps match')).toBeVisible()

  // A viewer sees the shared view, not the private one, and can open it but not share or delete it.
  const invite = await page.request.post(`${ws}/invites`, { data: { email: viewer.email, role: 'viewer' } })
  expect(invite.status()).toBe(201)
  const token = (await inviteLink(viewer.email)).split('/invites/')[1]
  const ctx = await browser.newContext()
  expect((await ctx.request.post(`/api/invites/${token}/accept`, { data: { displayName: viewer.name, password: viewer.password } })).status()).toBe(201)
  const v = await ctx.newPage()
  await v.goto(mapUrl)
  const vbar = v.getByRole('region', { name: 'Filter' })
  await vbar.getByRole('button', { name: 'Views' }).click()
  const vmenu = vbar.getByRole('group', { name: 'Saved views' })
  await expect(vmenu.getByRole('list', { name: 'Shared views' })).toContainText('Auth work')
  await expect(vmenu).not.toContainText('Plans')
  await expect(vmenu.getByRole('button', { name: /^Delete view/ })).toHaveCount(0)
  await vmenu.getByRole('button', { name: /^Auth work/ }).click()
  await expect(vbar.getByText('Label: auth')).toBeVisible()
  await vbar.getByRole('button', { name: 'View: Auth work' }).click()
  await expect(vmenu.getByLabel('Share with the workspace')).toHaveCount(0)
  await ctx.close()
})
