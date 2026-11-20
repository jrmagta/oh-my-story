import { expect, test } from '@playwright/test'
import { inviteLink, mailLink } from './mailpit'

// Runs after shell.spec.ts, which creates the owner and the Acme Product workspace.
const person = { email: `forgetful+${Date.now()}@example.com`, name: 'Fran Forgetful', old: 'the-old-password-1', next: 'the-new-password-2' }
const resetLink = /http:\/\/localhost:18080\/reset\/[A-Za-z0-9_-]{43}/

test('a forgotten password is reset from the email, and other sessions end', async ({ page, browser, request }) => {
  // The owner invites a person, who joins with a password.
  const owner = await browser.newContext()
  const login = await owner.request.post('/api/auth/login', { data: { email: 'owner@example.com', password: 'owner-password-123' } })
  expect(login.status()).toBe(200)
  const invite = await owner.request.post('/api/workspaces/acme-product/invites', { data: { email: person.email, role: 'viewer' } })
  expect(invite.status()).toBe(201)
  const token = (await inviteLink(person.email)).split('/invites/')[1]
  const accept = await request.post(`/api/invites/${token}/accept`, { data: { displayName: person.name, password: person.old } })
  expect(accept.status()).toBe(201)
  await owner.close()

  // They are still signed in on another device.
  const other = await browser.newContext()
  expect((await other.request.post('/api/auth/login', { data: { email: person.email, password: person.old } })).status()).toBe(200)
  expect((await other.request.get('/api/me')).status()).toBe(200)

  await page.goto('/login')
  await page.getByRole('link', { name: 'Forgot your password?' }).click()
  await page.getByLabel('Email').fill(person.email)
  await page.getByRole('button', { name: 'Send reset link' }).click()
  await expect(page.getByText('Check your inbox.')).toBeVisible()

  const link = await mailLink(person.email, resetLink)
  await page.goto(link)
  await expect(page.getByRole('heading', { name: 'Choose a new password' })).toBeVisible()
  await expect(page.getByText(person.email)).toBeVisible()
  await page.getByLabel('New password').fill(person.next)
  await page.getByRole('button', { name: 'Save and sign in' }).click()
  await expect(page.getByRole('heading', { name: 'Workspaces', exact: true })).toBeVisible()

  // The other device is signed out; only the new password works.
  expect((await other.request.get('/api/me')).status()).toBe(401)
  await other.close()
  expect((await request.post('/api/auth/login', { data: { email: person.email, password: person.old } })).status()).toBe(401)
  expect((await request.post('/api/auth/login', { data: { email: person.email, password: person.next } })).status()).toBe(200)

  // The link works once.
  await page.goto(link)
  await expect(page.getByRole('heading', { name: 'Link no longer valid' })).toBeVisible()
})

test('asking for an address with no account looks the same', async ({ page }) => {
  await page.goto('/forgot')
  await page.getByLabel('Email').fill(`nobody+${Date.now()}@example.com`)
  await page.getByRole('button', { name: 'Send reset link' }).click()
  await expect(page.getByText('Check your inbox.')).toBeVisible()
})
