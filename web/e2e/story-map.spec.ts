import { expect, test, type Page } from '@playwright/test'
import { inviteLink } from './mailpit'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const viewer = { email: `viewer+${Date.now()}@example.com`, name: 'Vera Viewer', password: 'viewer-password-123' }
const slug = 'mapping'
let mapUrl = ''

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

/** Type into the input that is open, one value per Enter, then close it. */
async function typeEach(page: Page, label: string, values: string[]) {
  const input = page.getByRole('textbox', { name: label })
  for (const v of values) {
    await input.fill(v)
    await input.press('Enter')
    await expect(page.getByText(v, { exact: true }).first()).toBeVisible()
  }
  if (await input.count()) await input.press('Escape')
}

const card = (page: Page, name: string | RegExp) => page.getByRole('button', { name, exact: true })
const panel = (page: Page) => page.getByRole('complementary', { name: /^Story / })

test('an editor builds a 4-level map with a release and stories', async ({ page }) => {
  await signIn(page)
  const ws = await page.request.post('/api/workspaces', { data: { name: 'Mapping', slug } })
  expect(ws.status()).toBe(201)

  await page.goto(`/w/${slug}`)
  await expect(page).toHaveURL(new RegExp(`/w/${slug}/maps$`))
  await expect(page.getByText('No maps yet.')).toBeVisible()
  await page.getByLabel('Map name').fill('Onboarding')
  await page.getByRole('button', { name: 'Create map' }).click()
  await expect(page).toHaveURL(new RegExp(`/w/${slug}/maps/[0-9a-f-]{36}$`))
  mapUrl = new URL(page.url()).pathname
  await expect(page.getByRole('heading', { name: 'Onboarding' })).toBeVisible()
  await expect(page.getByText('An empty map')).toBeVisible()

  await page.getByRole('button', { name: 'Add objective' }).click()
  await typeEach(page, 'New objective', ['Get started'])
  await page.getByRole('button', { name: 'Add activity to Get started' }).click()
  await typeEach(page, 'New activity in Get started', ['Sign up'])
  await page.getByRole('button', { name: 'Add step to Sign up' }).click()
  await typeEach(page, 'New step in Sign up', ['Create account', 'Verify email'])

  await page.getByRole('button', { name: '+ Add release' }).click()
  await typeEach(page, 'New release name', ['MVP'])
  await expect(page.getByRole('heading', { name: 'Release MVP' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Release Unscheduled' })).toBeVisible()

  await page.getByRole('button', { name: 'Add story to Create account / MVP' }).click()
  await typeEach(page, 'New story in Create account / MVP', ['Register with email'])
  await page.getByRole('button', { name: 'Add story to Verify email / Unscheduled' }).click()
  await typeEach(page, 'New story in Verify email / Unscheduled', ['Send verification email'])

  await expect(page.getByRole('group', { name: 'Stories in Create account / MVP' })).toContainText('Register with email')
  await expect(page.getByText('4-level map · 1 objective · 1 activity · 2 steps · 2 stories')).toBeVisible()
})

test('the story panel edits the description and status, and they survive a reload', async ({ page }) => {
  await signIn(page)
  await page.goto(mapUrl)
  await card(page, 'STORY-1 Register with email').click()
  await expect(panel(page)).toBeVisible()

  await panel(page).getByRole('button', { name: 'Edit' }).click()
  await panel(page).getByLabel('Description (Markdown)').fill('## Notes\n\nHash passwords with **argon2id**.')
  await panel(page).getByRole('button', { name: 'Save' }).click()
  await expect(panel(page).getByRole('heading', { name: 'Notes' })).toBeVisible()
  await expect(panel(page).locator('strong')).toHaveText('argon2id')

  await panel(page).getByLabel('Status').selectOption({ label: 'In progress' })
  await expect(card(page, 'STORY-1 Register with email')).toContainText('In progress')

  await page.reload()
  await expect(panel(page)).toBeVisible() // the open story is in the URL
  await expect(panel(page).getByRole('heading', { name: 'Notes' })).toBeVisible()
  await expect(panel(page).getByLabel('Status')).toHaveValue(/.+/)
  await expect(panel(page).getByLabel('Status').locator('option:checked')).toHaveText('In progress')
  await expect(card(page, 'STORY-1 Register with email')).toContainText('In progress')
  await expect(page.getByLabel('Stories by status')).toContainText('1In progress')
})

test('renaming a step, and deleting it with undo', async ({ page }) => {
  await signIn(page)
  await page.goto(mapUrl)
  await page.getByRole('button', { name: 'Rename step Verify email' }).click()
  await page.getByRole('textbox', { name: 'New name for step Verify email' }).fill('Check inbox')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('button', { name: 'Rename step Check inbox' })).toBeVisible()

  await page.getByRole('button', { name: 'Delete step Check inbox' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Deleted step “Check inbox”' })).toBeVisible()
  await expect(card(page, 'STORY-2 Send verification email')).toHaveCount(0)
  await page.getByRole('button', { name: 'Undo' }).click()
  await expect(card(page, 'STORY-2 Send verification email')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Rename step Check inbox' })).toBeVisible()
})

/** Story keys in a cell, in order. */
async function cellKeys(page: Page, where: string): Promise<string[]> {
  const names = await page
    .getByRole('group', { name: `Stories in ${where}` })
    .locator('.card-open')
    .evaluateAll((els) => els.map((e) => e.getAttribute('aria-label') ?? ''))
  return names.map((n) => n.split(' ')[0] ?? '')
}

/** Step titles across the backbone, left to right. */
async function stepOrder(page: Page): Promise<string[]> {
  const names = await page.getByRole('button', { name: /^Rename step / }).allTextContents()
  return names.map((n) => n.trim())
}

/** A pointer drag with intermediate moves, as a person makes it. */
async function pointerDrag(page: Page, from: { x: number; y: number }, to: { x: number; y: number }) {
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(from.x + 12, from.y + 12, { steps: 4 })
  await page.mouse.move(to.x, to.y, { steps: 12 })
  await page.mouse.up()
}

async function centre(locator: ReturnType<Page["locator"]>) {
  const b = await locator.boundingBox()
  if (!b) throw new Error('not visible')
  return { x: b.x + b.width / 2, y: b.y + b.height / 2, bottom: b.y + b.height }
}

/**
 * Pick up with Space, take one arrow-key step, drop with Space. Each key waits
 * for the screen to catch up, as a person would: at pickup the item's own
 * container is already the highlighted target, so the step is done only when
 * the highlight has moved somewhere else.
 */
async function keyboardMove(page: Page, handle: string, key: string) {
  const target = () => page.locator('.drop').evaluateAll((els) => els.map((e) => e.textContent).join('|'))
  await page.getByRole('button', { name: handle }).focus()
  await page.keyboard.press('Space')
  await expect(page.locator('.drag-ghost')).toBeVisible()
  const before = await target()
  await page.keyboard.press(key)
  await expect.poll(target).not.toBe(before)
  await expect(page.locator('.drop')).toHaveCount(1)
  await page.keyboard.press('Space')
  await expect(page.locator('.drag-ghost')).toHaveCount(0)
}

test('dragging a story with the pointer to another cell and release', async ({ page }) => {
  await signIn(page)
  await page.goto(mapUrl)
  await expect.poll(() => cellKeys(page, 'Check inbox / Unscheduled')).toEqual(['STORY-2'])

  const source = await centre(page.locator('.story-card', { has: page.getByRole('button', { name: /^STORY-2 / }) }))
  const cell = await centre(page.getByRole('group', { name: 'Stories in Create account / MVP' }))
  // Drop low in the cell, below its card: the end of the cell.
  await pointerDrag(page, source, { x: cell.x, y: cell.bottom - 6 })

  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-1', 'STORY-2'])
  expect(await cellKeys(page, 'Check inbox / Unscheduled')).toEqual([])
  await page.reload()
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-1', 'STORY-2'])
})

test('reordering with the keyboard: a story in its cell, a step, a release', async ({ page }) => {
  await signIn(page)
  await page.goto(mapUrl)

  // Picking up and dropping at once moves nothing.
  await page.getByRole('button', { name: 'Move STORY-2 Send verification email' }).focus()
  await page.keyboard.press('Space')
  await expect(page.locator('.drag-ghost')).toBeVisible()
  await page.keyboard.press('Space')
  await expect(page.locator('.drag-ghost')).toHaveCount(0)
  await page.reload()
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-1', 'STORY-2'])

  await keyboardMove(page, 'Move STORY-2 Send verification email', 'ArrowUp')
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-2', 'STORY-1'])

  await expect.poll(() => stepOrder(page)).toEqual(['Create account', 'Check inbox'])
  await keyboardMove(page, 'Move step Check inbox', 'ArrowLeft')
  await expect.poll(() => stepOrder(page)).toEqual(['Check inbox', 'Create account'])

  await page.getByRole('button', { name: '+ Add release' }).click()
  await typeEach(page, 'New release name', ['Later'])
  await keyboardMove(page, 'Move release Later', 'ArrowUp')
  const bands = () => page.getByRole('heading', { name: /^Release / }).evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
  await expect.poll(bands).toEqual(['Release Later', 'Release MVP', 'Release Unscheduled'])

  await page.reload()
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-2', 'STORY-1'])
  expect(await stepOrder(page)).toEqual(['Check inbox', 'Create account'])
  await expect.poll(bands).toEqual(['Release Later', 'Release MVP', 'Release Unscheduled'])
})

test('a move that crosses another edit says so, and one that fails is undone', async ({ page }) => {
  await signIn(page)
  // Hold this page's live socket open but silent, so it keeps showing the map
  // as it was while someone else changes it.
  await page.routeWebSocket(/\/live\?/, () => {})
  await page.goto(mapUrl)
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-2', 'STORY-1'])

  const api = `/api/workspaces/${slug}/maps/${mapUrl.split('/').pop()}`
  const view = (await (await page.request.get(api)).json()) as { stories: { id: string; key: string }[] }
  const id = (key: string) => view.stories.find((s) => s.key === key)?.id ?? ''

  // Someone else deletes STORY-2 while this page still shows it: the move fails and is put back.
  const json = { 'Content-Type': 'application/json' }
  expect((await page.request.delete(`${api}/stories/${id('STORY-2')}`, { headers: json })).status()).toBe(204)
  await keyboardMove(page, 'Move STORY-2 Send verification email', 'ArrowDown')
  await expect(page.getByRole('alert')).toContainText('not found')
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual(['STORY-1'])

  // Someone else edits STORY-1 while this page shows the old version: the move applies, and says so.
  const me = (await (await page.request.get('/api/me')).json()) as { user: { id: string } }
  expect((await page.request.patch(`${api}/stories/${id('STORY-1')}`, { data: { assigneeId: me.user.id } })).status()).toBe(200)
  await keyboardMove(page, 'Move STORY-1 Register with email', 'ArrowDown')
  await expect(page.getByRole('status').filter({ hasText: 'STORY-1 was changed by someone else' })).toBeVisible()
  await expect.poll(() => cellKeys(page, 'Create account / MVP')).toEqual([])
})

test('a viewer sees the map without edit controls', async ({ page, browser }) => {
  await signIn(page)
  const invite = await page.request.post(`/api/workspaces/${slug}/invites`, { data: { email: viewer.email, role: 'viewer' } })
  expect(invite.status()).toBe(201)
  const token = (await inviteLink(viewer.email)).split('/invites/')[1]

  const ctx = await browser.newContext()
  const accept = await ctx.request.post(`/api/invites/${token}/accept`, {
    data: { displayName: viewer.name, password: viewer.password },
  })
  expect(accept.status()).toBe(201)
  const v = await ctx.newPage()
  await v.goto(mapUrl)
  await expect(v.getByRole('heading', { name: 'Onboarding' })).toBeVisible()
  await expect(card(v, 'STORY-1 Register with email')).toBeVisible()
  await expect(v.getByRole('button', { name: /^Add / })).toHaveCount(0)
  await expect(v.getByRole('button', { name: /^Rename / })).toHaveCount(0)
  await expect(v.getByRole('button', { name: /^Delete / })).toHaveCount(0)
  await expect(v.getByRole('button', { name: /^Move / })).toHaveCount(0)
  await expect(v.getByRole('button', { name: '+ Add release' })).toHaveCount(0)

  await card(v, 'STORY-1 Register with email').click()
  await expect(panel(v)).toContainText('In progress')
  await expect(panel(v).getByRole('combobox')).toHaveCount(0)
  await expect(panel(v).getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(panel(v).getByRole('heading', { name: 'Notes' })).toBeVisible()
  await ctx.close()
})

test('two people on one map see each other and each other’s edits live', async ({ page, browser }) => {
  await signIn(page)
  await page.goto(mapUrl)
  await expect(page.getByRole('status').filter({ hasText: 'Live' })).toBeVisible()

  const ctx = await browser.newContext()
  const v = await ctx.newPage()
  await v.goto('/login')
  await v.getByLabel('Email').fill(viewer.email)
  await v.getByLabel('Password').fill(viewer.password)
  await v.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(v).not.toHaveURL(/\/login/)
  await v.goto(mapUrl)

  const present = (p: Page) => p.getByLabel('On this map now')
  await expect(present(page)).toContainText(viewer.name)
  await expect(present(page)).toContainText('Olive Owner')
  await expect(present(v)).toContainText('Olive Owner')

  // The owner adds a story and renames a step; the viewer sees both without reloading.
  await page.getByRole('button', { name: 'Add story to Check inbox / MVP' }).click()
  await typeEach(page, 'New story in Check inbox / MVP', ['Resend the link'])
  await expect(v.getByRole('group', { name: 'Stories in Check inbox / MVP' })).toContainText('Resend the link')
  await page.getByRole('button', { name: 'Rename step Check inbox' }).click()
  await page.getByRole('textbox', { name: 'New name for step Check inbox' }).fill('Open the email')
  await page.keyboard.press('Enter')
  await expect(v.getByRole('group', { name: 'Stories in Open the email / MVP' })).toContainText('Resend the link')

  // A move shows up too: up into the Later release.
  await keyboardMove(page, 'Move STORY-3 Resend the link', 'ArrowUp')
  await expect(page.getByRole('group', { name: 'Stories in Open the email / Later' })).toContainText('Resend the link')
  await expect(v.getByRole('group', { name: 'Stories in Open the email / Later' })).toContainText('Resend the link')
  await expect(v.getByRole('group', { name: 'Stories in Open the email / MVP' })).not.toContainText('Resend the link')

  await ctx.close()
  await expect(present(page)).not.toContainText(viewer.name)
})
