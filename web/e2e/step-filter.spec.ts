import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'filter'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('a filter dims or hides steps, counts only matches, and lives in the URL', async ({ page }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Filter', slug } })).status()).toBe(201)
  const call = async (method: 'post' | 'patch', path: string, data: object) => {
    const r = await page.request[method](path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string } }
  }
  const auth = await call('post', `${ws}/labels`, { name: 'auth' })
  const social = await call('post', `${ws}/labels`, { name: 'social' })
  const admin = await call('post', `${ws}/personas`, { name: 'Admin' })
  const map = await call('post', `${ws}/maps`, { name: 'Accounts' })
  const api = `${ws}/maps/${map.id}`
  const grow = await call('post', `${api}/nodes`, { level: 'objective', title: 'Grow' })
  const keep = await call('post', `${api}/nodes`, { level: 'objective', title: 'Keep' })
  await call('patch', `${api}/nodes/${grow.id}`, { personaIds: [admin.id] })
  const signUp = await call('post', `${api}/nodes`, { level: 'activity', title: 'Sign up', parentId: grow.id })
  const renew = await call('post', `${api}/nodes`, { level: 'activity', title: 'Renew', parentId: keep.id })
  const mvp = await call('post', `${api}/releases`, { name: 'MVP' })
  const s1 = await call('post', `${api}/steps`, { title: 'Email login', activityId: signUp.id, releaseId: mvp.id })
  const s2 = await call('post', `${api}/steps`, { title: 'Social login', activityId: signUp.id, releaseId: null })
  await call('post', `${api}/steps`, { title: 'Renew plan', activityId: renew.id, releaseId: mvp.id })
  await call('patch', `${api}/steps/${s1.step.id}`, { labelIds: [auth.id] })
  await call('patch', `${api}/steps/${s2.step.id}`, { labelIds: [auth.id, social.id] })

  await page.goto(`/w/${slug}/maps/${map.id}`)
  const card = (key: string) => page.locator('.step-card', { has: page.getByRole('button', { name: new RegExp(`^${key} `) }) })
  const bar = page.getByRole('region', { name: 'Filter' })

  // Highlight: everything stays, non-matching steps are dimmed, totals count matches.
  await bar.getByRole('button', { name: 'Filter' }).click()
  await bar.getByRole('group', { name: 'Filter by' }).getByLabel('auth').check()
  await page.keyboard.press('Escape')
  await expect(bar.getByText('2 of 3 steps match')).toBeVisible()
  await expect(card('FIL-3')).toHaveClass(/dimmed/)
  await expect(card('FIL-1')).not.toHaveClass(/dimmed/)
  await expect(page.getByRole('heading', { name: 'Release MVP' })).toContainText('1 step')

  // Hide: the step goes, its activity column stays.
  await bar.getByRole('button', { name: 'Hide' }).click()
  await expect(card('FIL-3')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Move activity Renew' })).toBeVisible()

  // The filter is in the URL, so a reload (or a shared link) keeps it.
  await page.reload()
  await expect(bar.getByText('Label: auth')).toBeVisible()
  await expect(card('FIL-3')).toHaveCount(0)
  await expect(card('FIL-1')).toBeVisible()

  // Fields combine: label auth and text "email" leave one step; persona Admin (through the objective) keeps it.
  await bar.getByLabel('Filter steps by key or title').fill('email')
  await expect(bar.getByText('1 of 3 steps match')).toBeVisible()
  await expect(card('FIL-2')).toHaveCount(0)
  await bar.getByRole('button', { name: /^Filter/ }).click()
  await bar.getByRole('group', { name: 'Filter by' }).getByLabel('Admin').check()
  await page.keyboard.press('Escape')
  await expect(bar.getByText('1 of 3 steps match')).toBeVisible()

  // Removing a chip, then Clear, brings everything back.
  await bar.getByRole('button', { name: 'Remove filter Label: auth' }).click()
  await expect(bar.getByText('Label: auth')).toHaveCount(0)
  await bar.getByRole('button', { name: 'Clear' }).click()
  await expect(bar.getByText(/steps match/)).toHaveCount(0)
  await expect(card('FIL-3')).toBeVisible()
  await expect(bar.getByLabel('Filter steps by key or title')).toHaveValue('')
})
