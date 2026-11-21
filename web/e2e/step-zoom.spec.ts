import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'zoom'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('card size, collapsed activities and releases, and focus are kept per map in this browser', async ({ page }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Zoom', slug } })).status()).toBe(201)
  const call = async (method: 'post' | 'patch', path: string, data: object) => {
    const r = await page.request[method](path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string } }
  }
  const label = await call('post', `${ws}/labels`, { name: 'ux' })
  const map = await call('post', `${ws}/maps`, { name: 'Shop' })
  const api = `${ws}/maps/${map.id}`
  const obj = await call('post', `${api}/nodes`, { level: 'objective', title: 'Buy things' })
  const browse = await call('post', `${api}/nodes`, { level: 'activity', title: 'Browse', parentId: obj.id })
  const buy = await call('post', `${api}/nodes`, { level: 'activity', title: 'Buy', parentId: obj.id })
  const mvp = await call('post', `${api}/releases`, { name: 'MVP' })
  const later = await call('post', `${api}/releases`, { name: 'Later' })
  const search = await call('post', `${api}/steps`, { title: 'Search products', activityId: browse.id, releaseId: mvp.id })
  await call('patch', `${api}/steps/${search.step.id}`, { labelIds: [label.id] })
  await call('post', `${api}/steps`, { title: 'Pay by card', activityId: buy.id, releaseId: later.id })
  await page.goto(`/w/${slug}/maps/${map.id}`)

  const card = page.locator('.step-card', { has: page.getByRole('button', { name: /^ZOO-1 / }) })
  const sizes = page.getByRole('group', { name: 'Card size' })
  const heading = (name: string) => page.getByRole('heading', { name: `Release ${name}` })

  // Card size: compact drops labels and status; titles drops the key too.
  await expect(card.getByText('ux')).toBeVisible()
  await sizes.getByRole('button', { name: 'Compact' }).click()
  await expect(card.getByText('ux')).toBeHidden()
  await expect(card.getByText('ZOO-1')).toBeVisible()
  await sizes.getByRole('button', { name: 'Titles' }).click()
  await expect(card.getByText('ZOO-1')).toBeHidden()
  await expect(card).toContainText('Search products')

  // Collapse an activity: its column narrows to a count; collapse a release: its cards go.
  await page.getByRole('button', { name: 'Actions for activity Browse' }).click()
  await page.getByRole('button', { name: 'Collapse', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Expand activity Browse' })).toBeVisible()
  await expect(card).toHaveCount(0)
  await expect(page.getByRole('group', { name: 'Steps in Browse / MVP' })).toHaveText('1')
  await page.getByRole('button', { name: 'Collapse release Later' }).click()
  await expect(page.getByRole('group', { name: 'Steps in Buy / Later' })).toHaveCount(0)
  await expect(heading('Later')).toContainText('1 step')

  // All of it survives a reload.
  await page.reload()
  await expect(sizes.getByRole('button', { name: 'Titles' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('button', { name: 'Expand activity Browse' })).toBeVisible()
  await expect(page.getByRole('group', { name: 'Steps in Buy / Later' })).toHaveCount(0)

  // Opening them out again.
  await page.getByRole('button', { name: 'Expand activity Browse' }).click()
  await page.getByRole('button', { name: 'Expand release Later' }).click()
  await expect(card).toBeVisible()
  await expect(page.getByRole('group', { name: 'Steps in Buy / Later' })).toContainText('Pay by card')

  // Focus shows one release alone, with a chip to go back.
  await page.getByRole('button', { name: 'Focus release Later' }).click()
  await expect(heading('Later')).toBeVisible()
  await expect(heading('MVP')).toHaveCount(0)
  await expect(heading('Unscheduled')).toHaveCount(0)
  const bar = page.getByRole('region', { name: 'Filter' })
  await expect(bar.getByText('Focus: Later')).toBeVisible()
  await page.reload()
  await expect(heading('MVP')).toHaveCount(0)
  await bar.getByRole('button', { name: 'Show all releases' }).click()
  await expect(heading('MVP')).toBeVisible()
  await expect(heading('Unscheduled')).toBeVisible()
})
