import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'cardtalk'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('objectives and activities have comments and their own history, counted on the card and live', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Card talk', slug, keyPrefix: 'CTK' } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string; key: string } }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Checkout' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Buy' })
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Pay', parentId: obj.id })
  const { step } = await post(`${api}/steps`, { title: 'Card form', activityId: act.id, releaseId: null })
  await post(`${api}/steps/${step.id}/comments`, { bodyMd: 'About the step only' })
  const mapUrl = `/w/${slug}/maps/${map.id}`

  // A second browser has the activity's comments open.
  const other = await (await browser.newContext()).newPage()
  await signIn(other)
  await other.goto(`${mapUrl}?node=${act.id}`)
  const otherPanel = other.getByRole('complementary', { name: 'Activity Pay' })
  await otherPanel.getByRole('tab', { name: 'Comments' }).click()
  await expect(otherPanel.getByText('No comments yet.')).toBeVisible()

  // Comment on the activity from its panel.
  await page.goto(mapUrl)
  await page.getByRole('button', { name: 'activity Pay', exact: true }).click()
  const panel = page.getByRole('complementary', { name: 'Activity Pay' })
  await panel.getByRole('tab', { name: 'Comments' }).click()
  await panel.getByRole('textbox', { name: 'New comment' }).fill('Should we take wallets here?')
  await panel.getByRole('button', { name: 'Comment', exact: true }).click()
  await expect(panel.getByRole('article', { name: 'Comment by Olive Owner' })).toContainText('Should we take wallets here?')
  await expect(panel.getByRole('tab', { name: 'Comments (1)' })).toBeVisible()
  // The step's comment stays on the step.
  await expect(panel).not.toContainText('About the step only')

  // The card counts it, and the other browser sees the comment and the count live.
  await expect(page.getByRole('button', { name: '1 comment on activity Pay' })).toBeVisible()
  await expect(otherPanel.getByRole('article', { name: 'Comment by Olive Owner' })).toContainText('Should we take wallets here?')
  await expect(other.getByRole('button', { name: '1 comment on activity Pay' })).toBeVisible()

  // A reply counts too; the objective has none of its activity's comments.
  await panel.getByRole('button', { name: 'Reply', exact: true }).click()
  await panel.getByRole('textbox', { name: 'Reply to Olive Owner' }).fill('Yes, phase two.')
  await panel.getByRole('button', { name: 'Reply', exact: true }).click()
  await expect(page.getByRole('button', { name: '2 comments on activity Pay' })).toBeVisible()
  await expect(page.getByRole('button', { name: /comments? on objective Buy/ })).toHaveCount(0)

  // Its history is its own: the rename and the comments, not the step's.
  await panel.getByRole('tab', { name: 'Details' }).click()
  await panel.getByRole('button', { name: 'Rename activity' }).click()
  await panel.getByRole('textbox', { name: 'New name for activity' }).fill('Pay now')
  await panel.getByRole('textbox', { name: 'New name for activity' }).press('Enter')
  const renamed = page.getByRole('complementary', { name: 'Activity Pay now' })
  await renamed.getByRole('tab', { name: 'History' }).click()
  const history = renamed.getByRole('list', { name: 'History of Pay now' })
  await expect(history).toContainText('renamed activity “Pay” to “Pay now”')
  await expect(history).toContainText('replied to a comment on “Pay now”')
  await expect(history).toContainText('commented on “Pay now”')
  await expect(history).toContainText('added activity “Pay”')
  await expect(history).not.toContainText(step.key)
})
