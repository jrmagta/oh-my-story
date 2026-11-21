import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'search'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('the search palette finds steps across maps and opens them', async ({ page }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Search', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string }
  }
  const build = async (name: string, activity: string, steps: [string, string][]) => {
    const map = await post(`${ws}/maps`, { name })
    const obj = await post(`${ws}/maps/${map.id}/nodes`, { level: 'objective', title: 'Main' })
    const act = await post(`${ws}/maps/${map.id}/nodes`, { level: 'activity', title: activity, parentId: obj.id })
    for (const [title, descriptionMd] of steps) await post(`${ws}/maps/${map.id}/steps`, { title, descriptionMd, activityId: act.id })
    return map.id
  }
  await build('Shop', 'Pay', [
    ['Checkout flow', 'Pay with **Stripe**'],
    ['Wish list', ''],
  ])
  const admin = await build('Admin', 'Money', [['Refund order', 'Use Stripe refunds']])

  await page.goto(`/w/${slug}/maps`)
  const palette = page.getByRole('dialog', { name: 'Search steps' })
  const input = palette.getByRole('combobox', { name: 'Search steps by key or words' })
  const results = palette.getByRole('option')

  // Ctrl+K opens it (its listener belongs to the nav's Search button, so wait for that to render);
  // a word from two descriptions finds both, with their map and activity.
  await expect(page.getByRole('button', { name: 'Search steps' })).toBeVisible()
  await page.keyboard.press('Control+k')
  await expect(palette).toBeVisible()
  await input.fill('strip')
  await expect(results).toHaveCount(2)
  await expect(palette).toContainText('Admin › Money')
  await expect(palette).toContainText('Shop › Pay')
  // Equal matches come in key order, so the second is SEA-3 on the Admin map.
  await expect(results.nth(1)).toContainText('SEA-3')
  await input.press('ArrowDown')
  await expect(results.nth(1)).toHaveAttribute('aria-selected', 'true')
  await input.press('Enter')
  await expect(palette).toHaveCount(0)
  await expect(page).toHaveURL(new RegExp(`/maps/${admin}\\?step=`))
  await expect(page.getByRole('complementary', { name: 'Step SEA-3' })).toBeVisible()

  // The nav button opens it too; a key finds its step first, and a click opens it.
  await page.getByRole('button', { name: 'Search steps' }).click()
  await input.fill('sea-2')
  await expect(results.first()).toContainText('Wish list')
  await results.first().click()
  await expect(page.getByRole('complementary', { name: 'Step SEA-2' })).toBeVisible()

  // No match says so; Escape closes.
  await page.keyboard.press('Control+k')
  await input.fill('nothing like this')
  await expect(palette).toContainText('No steps match')
  await input.press('Escape')
  await expect(palette).toHaveCount(0)
})
