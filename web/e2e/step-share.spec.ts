import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'share'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('a read-only link shows the map to someone without an account, until it is revoked', async ({ page, browser }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Share', slug } })).status()).toBe(201)
  const call = async (method: 'post' | 'patch', path: string, data: object) => {
    const r = await page.request[method](path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string } }
  }
  const me = (await (await page.request.get('/api/me')).json()) as { user: { id: string } }
  const map = await call('post', `${ws}/maps`, { name: 'Launch plan' })
  const api = `${ws}/maps/${map.id}`
  const obj = await call('post', `${api}/nodes`, { level: 'objective', title: 'Launch' })
  const act = await call('post', `${api}/nodes`, { level: 'activity', title: 'Announce', parentId: obj.id })
  const step = (await call('post', `${api}/steps`, { title: 'Write the blog post', descriptionMd: 'Draft by **Friday**', activityId: act.id })).step.id
  await call('post', `${api}/steps`, { title: 'Email customers', activityId: act.id })
  await call('patch', `${api}/steps/${step}`, { assigneeId: me.user.id })
  await call('post', `${api}/steps/${step}/comments`, { bodyMd: 'Internal: budget is tight' })

  // The owner makes a link from the Share dialog and copies it.
  await page.goto(`/w/${slug}/maps/${map.id}`)
  await page.getByRole('button', { name: 'Actions for map Launch plan' }).click()
  await page.getByRole('button', { name: 'Share…' }).click()
  const dialog = page.getByRole('dialog', { name: 'Share this map' })
  await dialog.getByRole('button', { name: 'Make a link' }).click()
  const url = await dialog.getByLabel('New link').inputValue()
  expect(url).toMatch(/\/s\/[A-Za-z0-9_-]{43}$/)
  await expect(dialog.getByRole('list', { name: 'Share links' }).getByRole('listitem')).toHaveCount(1)

  // Someone with no account opens it: the map and step details, nothing about people or the conversation.
  const ctx = await browser.newContext()
  const anon = await ctx.newPage()
  await anon.goto(new URL(url).pathname)
  await expect(anon.getByRole('heading', { name: 'Launch plan' })).toBeVisible()
  await expect(anon.getByText('Share › Read-only link')).toBeVisible()
  await anon.getByRole('button', { name: 'SHA-1 Write the blog post', exact: true }).click()
  const panel = anon.getByRole('complementary', { name: 'Step SHA-1' })
  await expect(panel.locator('strong')).toHaveText('Friday')
  await expect(panel.getByRole('tab')).toHaveCount(1)
  await expect(panel).not.toContainText('Assignee')
  await expect(anon.getByText('Internal: budget is tight')).toHaveCount(0)
  await expect(anon.getByText('Olive Owner')).toHaveCount(0)
  await expect(anon.getByRole('button', { name: /^Add |^Share$|^Activity$|^Views$/ })).toHaveCount(0)
  // Backbone menus only open details and collapse activities: reading and a view setting, not edits.
  await anon.getByRole('button', { name: 'Actions for activity Announce' }).click()
  await expect(anon.getByRole('group', { name: 'Actions for activity Announce' }).getByRole('button')).toHaveText(['Details', 'Collapse'])
  await anon.keyboard.press('Escape')
  const objMenu = anon.getByRole('button', { name: /^Actions for objective/ }).first()
  await objMenu.click()
  await expect(anon.getByRole('group', { name: /^Actions for objective/ }).getByRole('button')).toHaveText(['Details'])
  await anon.keyboard.press('Escape')
  await expect(panel.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  // Filtering works on the shared page.
  await anon.getByLabel('Filter steps by key or title').fill('email')
  await expect(anon.getByText('1 of 2 steps match')).toBeVisible()

  // Revoking the link ends it.
  await dialog.getByRole('button', { name: /^Revoke link/ }).click()
  await expect(dialog.getByText('No links yet.')).toBeVisible()
  await anon.reload()
  await expect(anon.getByRole('heading', { name: 'Link not available' })).toBeVisible()
  await ctx.close()
})
