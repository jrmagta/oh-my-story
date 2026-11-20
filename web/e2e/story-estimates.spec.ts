import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'estimates'
let mapUrl = ''

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

const panel = (page: Page) => page.getByRole('complementary', { name: /^Story / })
const total = (page: Page, node: string) => page.getByLabel(new RegExp(`^Total for ${node}: `))

async function estimate(page: Page, key: string, value: string) {
  await page.getByRole('button', { name: new RegExp(`^${key} `) }).click()
  const input = panel(page).getByLabel('Estimate (points)')
  await input.fill(value)
  await input.press('Enter')
  await expect(input).toHaveValue(value)
}

test('estimates add up per step, activity, release and map, live in a second browser', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Estimates', slug } })).status()).toBe(201)
  const ws = `/api/workspaces/${slug}`
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string }
  }
  const map = await post(`${ws}/maps`, { name: 'Checkout', levels: 3 })
  const api = `${ws}/maps/${map.id}`
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Pay' })
  const card = await post(`${api}/nodes`, { level: 'step', title: 'Card', parentId: act.id })
  const invoice = await post(`${api}/nodes`, { level: 'step', title: 'Invoice', parentId: act.id })
  const rel = await post(`${api}/releases`, { name: 'MVP' })
  await post(`${api}/stories`, { title: 'Enter card', stepId: card.id, releaseId: rel.id })
  await post(`${api}/stories`, { title: 'Save card', stepId: card.id, releaseId: rel.id })
  await post(`${api}/stories`, { title: 'Email invoice', stepId: invoice.id, releaseId: null })
  mapUrl = `/w/${slug}/maps/${map.id}`

  // A second browser watches the same map.
  const ctx = await browser.newContext()
  const other = await ctx.newPage()
  await signIn(other)
  await other.goto(mapUrl)
  await expect(other.getByRole('status').filter({ hasText: 'Live' })).toBeVisible()

  await page.goto(mapUrl)
  await expect(total(page, 'activity Pay')).toHaveCount(0) // nothing estimated yet
  await estimate(page, 'STORY-1', '3')
  await estimate(page, 'STORY-2', '5')
  await estimate(page, 'STORY-3', '0.5')

  await expect(total(page, 'step Card')).toHaveText('8 points')
  await expect(total(page, 'step Invoice')).toHaveText('0.5 points')
  await expect(total(page, 'activity Pay')).toHaveText('8.5 points')
  await expect(page.getByRole('heading', { name: 'Release MVP' })).toContainText('2 stories · 8 points')
  await expect(page.getByRole('heading', { name: 'Release Unscheduled' })).toContainText('1 story · 0.5 points')
  await expect(page.getByText(/· 3 stories · 8\.5 points$/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'STORY-1 Enter card', exact: true })).toContainText('3')

  await expect(total(other, 'activity Pay')).toHaveText('8.5 points')
  await estimate(page, 'STORY-1', '1')
  await expect(total(other, 'activity Pay')).toHaveText('6.5 points')

  // Clearing an estimate counts the story as unestimated.
  await page.getByRole('button', { name: /^STORY-3 / }).click()
  await panel(page).getByLabel('Estimate (points)').fill('')
  await panel(page).getByLabel('Estimate (points)').press('Enter')
  await expect(total(page, 'activity Pay')).toHaveText('6 points (1 unestimated)')
  await expect(total(other, 'activity Pay')).toHaveText('6 points (1 unestimated)')
  await expect(total(page, 'step Invoice')).toHaveCount(0)

  await panel(page).getByRole('tab', { name: 'History' }).click()
  await expect(panel(page)).toContainText('cleared the estimate of STORY-3')
  await ctx.close()
})

test('an admin changes the estimate unit in settings', async ({ page }) => {
  await signIn(page)
  await page.goto(`/w/${slug}/settings`)
  await page.getByLabel('Estimate unit').fill('days')
  await page.getByRole('button', { name: 'Save settings' }).click()
  await expect(page.getByRole('status')).toHaveText('Settings saved.')

  await page.goto(mapUrl)
  await expect(total(page, 'activity Pay')).toHaveText('6 days (1 unestimated)')
  await page.getByRole('button', { name: /^STORY-2 / }).click()
  await expect(panel(page).getByLabel('Estimate (days)')).toHaveValue('5')
})
