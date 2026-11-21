import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Needs the compose Keycloak, whose realm has
// carol (never signed in here) and lets the app's client search its users.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'directory'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('an admin adds someone from Keycloak, who is a member at once and signs in with SSO', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Directory', slug } })).status()).toBe(201)
  await page.goto(`/w/${slug}/members`)

  // Find carol in Keycloak and add her as a commenter.
  const panel = page.getByRole('region', { name: 'Add from Keycloak' })
  await panel.getByLabel('Find by name or email').fill('carol')
  await panel.getByLabel('Role for people added').selectOption('commenter')
  const people = panel.getByRole('list', { name: 'People in Keycloak' })
  await expect(people.getByRole('listitem')).toContainText(['Carol Directory'])
  await expect(people).toContainText('carol@example.com')
  await people.getByRole('button', { name: 'Add Carol Directory' }).click()
  await expect(panel.getByText('Carol Directory is a member now.')).toBeVisible()
  await expect(people.getByRole('listitem').filter({ hasText: 'Carol Directory' })).toContainText('Member')

  // She is in the member list straight away, with her name from Keycloak, and no invite was made.
  const row = page.getByRole('list', { name: 'Members' }).getByRole('listitem').filter({ hasText: 'Carol Directory' })
  await expect(row).toContainText('carol@example.com')
  await expect(row.getByRole('combobox')).toHaveValue('commenter')
  expect(await (await page.request.get(`/api/workspaces/${slug}/invites`)).json()).toEqual([])

  // Carol signs in with Keycloak and lands in the workspace.
  const ctx = await browser.newContext()
  const carol = await ctx.newPage()
  await carol.goto('/login')
  await carol.getByRole('link', { name: 'Sign in with Keycloak' }).click()
  await carol.locator('#username').fill('carol')
  await carol.locator('#password').fill('carol')
  await carol.locator('#kc-login').click()
  await expect(carol).not.toHaveURL(/\/login|localhost:8180/)
  const me = (await (await carol.request.get('/api/me')).json()) as { user: { displayName: string }; workspaces: { slug: string; role: string }[] }
  expect(me.user.displayName).toBe('Carol Directory')
  expect(me.workspaces).toEqual([expect.objectContaining({ slug, role: 'commenter' })])
  await ctx.close()
})
