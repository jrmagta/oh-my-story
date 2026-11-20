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

const card = (page: Page, name: string | RegExp) => page.getByRole('button', { name })
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
  await expect(card(page, /Send verification email/)).toHaveCount(0)
  await page.getByRole('button', { name: 'Undo' }).click()
  await expect(card(page, /Send verification email/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Rename step Check inbox' })).toBeVisible()
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
  await expect(v.getByRole('button', { name: '+ Add release' })).toHaveCount(0)

  await card(v, 'STORY-1 Register with email').click()
  await expect(panel(v)).toContainText('In progress')
  await expect(panel(v).getByRole('combobox')).toHaveCount(0)
  await expect(panel(v).getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(panel(v).getByRole('heading', { name: 'Notes' })).toBeVisible()
  await ctx.close()
})
