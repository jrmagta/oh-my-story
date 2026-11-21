import { expect, test, type Page } from '@playwright/test'
import { clearMail, inviteLink, mailLink } from './mailpit'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const cora = { email: `cora+${Date.now()}@example.com`, name: 'Cora Commenter', password: 'cora-password-123' }
const slug = 'comments'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('a commenter mentions someone, who is emailed and sees it live, and the thread is replied to and resolved', async ({ page, browser }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Comments', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string; step: { id: string } }
  }
  const map = await post(`${ws}/maps`, { name: 'Login' })
  const obj = await post(`${ws}/maps/${map.id}/nodes`, { level: 'objective', title: 'Get in' })
  const act = await post(`${ws}/maps/${map.id}/nodes`, { level: 'activity', title: 'Sign in', parentId: obj.id })
  const step = (await post(`${ws}/maps/${map.id}/steps`, { title: 'Email login', activityId: act.id })).step.id
  const stepUrl = `/w/${slug}/maps/${map.id}?step=${step}`

  // Cora joins as a commenter.
  expect((await page.request.post(`${ws}/invites`, { data: { email: cora.email, role: 'commenter' } })).status()).toBe(201)
  const token = (await inviteLink(cora.email)).split('/invites/')[1]
  const ctx = await browser.newContext()
  expect((await ctx.request.post(`/api/invites/${token}/accept`, { data: { displayName: cora.name, password: cora.password } })).status()).toBe(201)
  const c = await ctx.newPage()

  // The owner watches the step's comments.
  await clearMail(owner.email)
  await page.goto(stepUrl)
  await expect(page.getByRole('status').filter({ hasText: 'Live' })).toBeVisible()
  await page.getByRole('tab', { name: /^Comments/ }).click()
  await expect(page.getByText('No comments yet.')).toBeVisible()

  // Cora writes a comment and picks the owner from the @ list.
  await c.goto(stepUrl)
  await c.getByRole('tab', { name: /^Comments/ }).click()
  const box = c.getByRole('textbox', { name: 'New comment' })
  await box.pressSequentially('Can we use @Oli')
  await expect(c.getByRole('listbox', { name: 'Mention someone' })).toContainText('Olive Owner')
  await box.press('Enter')
  await expect(box).toHaveValue('Can we use @Olive Owner ')
  await box.pressSequentially('magic links?')
  await c.getByRole('button', { name: 'Comment', exact: true }).click()
  await expect(c.getByRole('article', { name: 'Comment by Cora Commenter' })).toContainText('magic links?')

  // The owner sees it without reloading, and the card counts it.
  await expect(page.getByRole('article', { name: 'Comment by Cora Commenter' })).toContainText('Can we use @Olive Owner magic links?')
  await expect(page.getByRole('tab', { name: 'Comments (1)' })).toBeVisible()
  await expect(page.locator('.step-card .comment-count')).toHaveText('1')

  // The mention emails the owner a link to the step.
  const link = await mailLink(owner.email, /http:\/\/localhost:18080\/w\/comments\/maps\/\S+/)
  expect(link).toContain(`step=${step}`)

  // The owner replies and resolves; Cora sees both.
  const thread = page.getByRole('listitem', { name: 'Thread by Cora Commenter' })
  await thread.getByRole('button', { name: 'Reply' }).click()
  await thread.getByRole('textbox', { name: 'Reply to Cora Commenter' }).fill('Yes, see the auth spike.')
  await thread.getByRole('button', { name: 'Reply' }).click()
  await expect(c.getByRole('article', { name: 'Comment by Olive Owner' })).toContainText('see the auth spike')
  await thread.getByRole('article', { name: 'Comment by Cora Commenter' }).getByRole('button', { name: 'Resolve' }).click()
  await expect(c.getByRole('listitem', { name: 'Thread by Cora Commenter' })).toContainText('Resolved')
  await expect(c.getByRole('tab', { name: 'Comments (2)' })).toBeVisible()

  // A commenter cannot resolve or delete someone else's comment.
  const ownerReply = c.getByRole('article', { name: 'Comment by Olive Owner' })
  await expect(ownerReply.getByRole('button', { name: 'Delete' })).toHaveCount(0)
  await expect(ownerReply.getByRole('button', { name: 'Resolve' })).toHaveCount(0)
  await ctx.close()
})
