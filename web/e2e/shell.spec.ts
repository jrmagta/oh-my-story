import { expect, test, type Page } from '@playwright/test'
import { clearMail, inviteLink } from './mailpit'

// One scenario, in order, against a fresh database: the owner sets up the
// instance, then people join by email and by Keycloak.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', name: 'Olive Owner', password: 'owner-password-123' }
// Mailpit keeps mail across runs; a fresh address per run keeps links unambiguous.
const carol = { email: `carol+${Date.now()}@example.com`, name: 'Carol Example', password: 'carol-password-123' }

async function signIn(page: Page, email = owner.email, password = owner.password) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

async function keycloakSignIn(page: Page, user: string) {
  await page.getByRole('link', { name: 'Sign in with Keycloak' }).click()
  await expect(page).toHaveURL(/localhost:8180\/realms\/oms\//)
  await page.locator('#username').fill(user)
  await page.locator('#password').fill(user)
  await page.locator('#kc-login').click()
}

const members = (page: Page) => page.getByRole('list', { name: 'Members' }).getByRole('listitem')

test('the first visitor creates the owner account and a workspace', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Your name').fill(owner.name)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Create account' }).click()

  await expect(page.getByRole('heading', { name: 'Workspaces', exact: true })).toBeVisible()
  await expect(page.getByText("You're not in any workspace yet")).toBeVisible()

  await page.getByLabel('Workspace name').fill('Acme Product')
  await expect(page.getByLabel('URL slug')).toHaveValue('acme-product')
  await page.getByRole('button', { name: 'Create workspace' }).click()

  await expect(page).toHaveURL(/\/w\/acme-product\/members$/)
  await expect(page.getByRole('heading', { name: 'Members', exact: true })).toBeVisible()
  await expect(members(page)).toHaveCount(1)
  await expect(members(page).first()).toContainText(owner.name)
  await expect(members(page).first()).toContainText('owner')
})

test('setup is closed once the owner exists', async ({ page }) => {
  await page.goto('/setup')
  await expect(page).toHaveURL(/\/login$/)
})

test('signing in locally, and out', async ({ page }) => {
  await page.goto('/w/acme-product/members')
  await expect(page).toHaveURL(/\/login\?next=%2Fw%2Facme-product%2Fmembers$/)

  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill('not-the-password')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('email or password is incorrect')

  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/w\/acme-product\/members$/) // back where they were going

  await page.getByRole('button', { name: `Account: ${owner.name}` }).click()
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await page.goto('/')
  await expect(page).toHaveURL(/\/login$/)
})

test('an invited person accepts from the email with a new account', async ({ page, browser }) => {
  await signIn(page)
  await page.getByRole('link', { name: 'Open Acme Product' }).click()
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(carol.email)
  await page.getByRole('combobox', { name: 'Role', exact: true }).selectOption('editor')
  await page.getByRole('button', { name: 'Send invite' }).click()
  await expect(page.getByText(`Invite sent to ${carol.email}.`)).toBeVisible()
  await expect(page.getByRole('list', { name: 'Open invites' })).toContainText(carol.email)

  const link = await inviteLink(carol.email)
  const ctx = await browser.newContext()
  const guest = await ctx.newPage()
  await guest.goto(link)
  await expect(guest.getByRole('heading', { name: 'Join Acme Product', exact: true })).toBeVisible()
  await expect(guest.getByText(carol.email)).toBeVisible()
  await guest.getByLabel('Your name').fill(carol.name)
  await guest.getByLabel('Password').fill(carol.password)
  await guest.getByRole('button', { name: 'Create account and join' }).click()

  await expect(guest).toHaveURL(/\/w\/acme-product\/members$/)
  await expect(members(guest).filter({ hasText: carol.name })).toContainText('editor')
  // An editor sees the members but cannot manage them.
  await expect(guest.getByRole('heading', { name: 'Invite someone', exact: true })).toHaveCount(0)
  await expect(guest.getByRole('combobox', { name: `Role for ${owner.name}` })).toHaveCount(0)

  // The link works once.
  await guest.goto(link)
  await expect(guest.getByRole('heading', { name: 'Invite no longer valid', exact: true })).toBeVisible()
  await ctx.close()

  await page.reload()
  await expect(members(page)).toHaveCount(2)
  await expect(page.getByRole('list', { name: 'Open invites' })).toHaveCount(0)
})

test('the owner changes a role and it sticks', async ({ page }) => {
  await signIn(page)
  await page.goto('/w/acme-product/members')
  await page.getByRole('combobox', { name: `Role for ${carol.name}` }).selectOption('commenter')
  await expect(page.getByRole('combobox', { name: `Role for ${carol.name}` })).toBeEnabled()
  await page.reload()
  await expect(page.getByRole('combobox', { name: `Role for ${carol.name}` })).toHaveValue('commenter')
})

test('a Keycloak user accepts an invite through single sign-on', async ({ page, browser }) => {
  await clearMail('alice@example.com')
  await signIn(page)
  await page.goto('/w/acme-product/members')
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('alice@example.com')
  await page.getByRole('combobox', { name: 'Role', exact: true }).selectOption('viewer')
  await page.getByRole('button', { name: 'Send invite' }).click()
  await expect(page.getByText('Invite sent to alice@example.com.')).toBeVisible()

  const link = await inviteLink('alice@example.com')
  const ctx = await browser.newContext()
  const alice = await ctx.newPage()
  await alice.goto(link)
  await keycloakSignIn(alice, 'alice')

  // Single sign-on returns to the invite, now signed in.
  await expect(alice).toHaveURL(link)
  await alice.getByRole('button', { name: 'Accept invite' }).click()
  await expect(alice).toHaveURL(/\/w\/acme-product\/members$/)
  await expect(members(alice).filter({ hasText: 'Alice Example' })).toContainText('viewer')
  await ctx.close()
})

test('single sign-on without an invite is refused', async ({ page }) => {
  await page.goto('/login')
  await keycloakSignIn(page, 'bob')
  await expect(page).toHaveURL(/\/login\?error=not_invited$/)
  await expect(page.getByRole('alert')).toContainText('no account or open invite')
})

test('the theme toggle overrides the system preference and persists', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/login')
  const html = page.locator('html')
  await expect(html).toHaveAttribute('data-theme', 'blink-dark')
  await page.getByRole('button', { name: 'Switch to light theme' }).click()
  await expect(html).toHaveAttribute('data-theme', 'blink-light')
  await page.reload()
  await expect(html).toHaveAttribute('data-theme', 'blink-light')
})
