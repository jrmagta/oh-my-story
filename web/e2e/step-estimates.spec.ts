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

const panel = (page: Page) => page.getByRole('complementary', { name: /^Step / })
const total = (page: Page, node: string) => page.getByLabel(new RegExp(`^Total for ${node}: `))

async function estimate(page: Page, key: string, value: string) {
  await page.getByRole('button', { name: new RegExp(`^${key} `) }).click()
  // Wait for the panel to show this step, or the fill lands in the previous one.
  const input = page.getByRole('complementary', { name: `Step ${key}` }).getByLabel('Estimate (points)')
  await input.fill(value)
  await input.press('Enter')
  await expect(input).toHaveValue(value)
}

test('estimates add up per activity, objective, release and map, live in a second browser', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Estimates', slug } })).status()).toBe(201)
  const ws = `/api/workspaces/${slug}`
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string }
  }
  const map = await post(`${ws}/maps`, { name: 'Checkout' })
  const api = `${ws}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Pay' })
  const card = await post(`${api}/nodes`, { level: 'activity', title: 'Card', parentId: obj.id })
  const invoice = await post(`${api}/nodes`, { level: 'activity', title: 'Invoice', parentId: obj.id })
  const rel = await post(`${api}/releases`, { name: 'MVP' })
  await post(`${api}/steps`, { title: 'Enter card', activityId: card.id, releaseId: rel.id })
  await post(`${api}/steps`, { title: 'Save card', activityId: card.id, releaseId: rel.id })
  await post(`${api}/steps`, { title: 'Email invoice', activityId: invoice.id, releaseId: null })
  mapUrl = `/w/${slug}/maps/${map.id}`

  // A second browser watches the same map.
  const ctx = await browser.newContext()
  const other = await ctx.newPage()
  await signIn(other)
  await other.goto(mapUrl)
  await expect(other.getByRole('status').filter({ hasText: 'Live' })).toBeVisible()

  await page.goto(mapUrl)
  await expect(total(page, 'objective Pay')).toHaveCount(0) // nothing estimated yet
  await estimate(page, 'EST-1', '3')
  await estimate(page, 'EST-2', '5')
  await estimate(page, 'EST-3', '0.5')

  await expect(total(page, 'activity Card')).toHaveText('8 points')
  await expect(total(page, 'activity Invoice')).toHaveText('0.5 points')
  await expect(total(page, 'objective Pay')).toHaveText('8.5 points')
  await expect(page.getByRole('heading', { name: 'Release MVP' })).toContainText('2 steps · 8 points')
  await expect(page.getByRole('heading', { name: 'Release Unscheduled' })).toContainText('1 step · 0.5 points')
  await expect(page.getByText(/· 3 steps · 8\.5 points/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'EST-1 Enter card', exact: true })).toContainText('3')

  await expect(total(other, 'objective Pay')).toHaveText('8.5 points')
  await estimate(page, 'EST-1', '1')
  await expect(total(other, 'objective Pay')).toHaveText('6.5 points')

  // Clearing an estimate counts the step as unestimated.
  await page.getByRole('button', { name: /^EST-3 / }).click()
  await expect(panel(page)).toHaveAccessibleName('Step EST-3')
  await panel(page).getByLabel('Estimate (points)').fill('')
  await panel(page).getByLabel('Estimate (points)').press('Enter')
  await expect(total(page, 'objective Pay')).toHaveText('6 points (1 unestimated)')
  await expect(total(other, 'objective Pay')).toHaveText('6 points (1 unestimated)')
  await expect(total(page, 'activity Invoice')).toHaveCount(0)

  await panel(page).getByRole('tab', { name: 'History' }).click()
  await expect(panel(page)).toContainText('cleared the estimate of EST-3')
  await ctx.close()
})

test('an admin changes the estimate unit in settings', async ({ page }) => {
  await signIn(page)
  await page.goto(`/w/${slug}/settings`)
  await page.getByLabel('Estimate unit').fill('days')
  await page.getByRole('button', { name: 'Save settings' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Settings saved.' })).toBeVisible()

  await page.goto(mapUrl)
  await expect(total(page, 'objective Pay')).toHaveText('6 days (1 unestimated)')
  await page.getByRole('button', { name: /^EST-2 / }).click()
  await expect(page.getByRole('complementary', { name: 'Step EST-2' }).getByLabel('Estimate (days)')).toHaveValue('5')
})
