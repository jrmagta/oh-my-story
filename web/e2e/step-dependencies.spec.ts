import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'deps'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('steps block other steps, on the same map or another, shown on cards and as lines', async ({ page }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Deps', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string; key: string } }
  }
  const build = async (name: string) => {
    const map = await post(`${ws}/maps`, { name })
    const api = `${ws}/maps/${map.id}`
    const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Deliver' })
    const a1 = await post(`${api}/nodes`, { level: 'activity', title: 'Plan', parentId: obj.id })
    const a2 = await post(`${api}/nodes`, { level: 'activity', title: 'Make', parentId: obj.id })
    return { id: map.id, api, a1: a1.id, a2: a2.id }
  }
  const plan = await build('Release plan')
  const legal = await build('Legal')
  const design = (await post(`${plan.api}/steps`, { title: 'Design', activityId: plan.a1, releaseId: null })).step
  const make = (await post(`${plan.api}/steps`, { title: 'Build it', activityId: plan.a2, releaseId: null })).step
  const contract = (await post(`${legal.api}/steps`, { title: 'Contract review', activityId: legal.a1, releaseId: null })).step
  const statuses = ((await (await page.request.get(plan.api)).json()) as { workflow: { statuses: { id: string; category: string }[] } })
    .workflow.statuses
  const done = statuses.find((s) => s.category === 'done')!.id
  await page.goto(`/w/${slug}/maps/${plan.id}`)

  // Lines only appear once there is something to draw.
  await expect(page.getByRole('button', { name: 'Lines' })).toHaveCount(0)

  // Open "Build it" and add Design as its blocker by finding it.
  const card = page.locator(`[data-step="${make.id}"]`)
  await card.getByRole('button').first().click()
  const blockedBy = page.getByRole('group', { name: `Blocked by for ${make.key}` })
  await blockedBy.getByRole('button', { name: '+ Add' }).click()
  await page.getByRole('textbox', { name: `Step that blocks ${make.key}` }).fill('Desig')
  await page.getByRole('list', { name: 'Matching steps' }).getByRole('button', { name: new RegExp(design.key) }).click()
  await expect(blockedBy.getByRole('button', { name: `${design.key} Design` })).toBeVisible()
  await expect(card).toContainText(`Blocked by ${design.key}`)
  // The other end lists it under Blocks.
  await blockedBy.getByRole('button', { name: `${design.key} Design` }).click()
  await expect(page.getByRole('group', { name: `Blocks for ${design.key}` })).toContainText(make.key)

  // Lines draw an arrow from Design to Build it; the setting survives a reload.
  await page.getByRole('button', { name: 'Lines' }).click()
  await expect(page.locator('.dep-lines > path.open')).toHaveCount(1)
  await page.reload()
  await expect(page.getByRole('button', { name: 'Lines' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('.dep-lines > path.open')).toHaveCount(1)

  // A loop is refused with a message.
  await page.request.post(`${plan.api}/dependencies`, { data: { blockerId: make.id, blockedId: design.id } }).then((r) => expect(r.status()).toBe(400))

  // Finishing the blocker (here, by someone else) clears the badge live and greys the line.
  await page.request.patch(`${plan.api}/steps/${design.id}`, { data: { statusId: done } })
  await expect(card).not.toContainText('Blocked by')
  await expect(page.locator('.dep-lines > path.done')).toHaveCount(1)

  // A blocker on another map: named with its map, live when it is finished there.
  await card.getByRole('button').first().click()
  await blockedBy.getByRole('button', { name: '+ Add' }).click()
  await page.getByRole('textbox', { name: `Step that blocks ${make.key}` }).fill(contract.key)
  await page.getByRole('list', { name: 'Matching steps' }).getByRole('button', { name: new RegExp(contract.key) }).click()
  await expect(card).toContainText(`Blocked by ${contract.key}`)
  await expect(blockedBy.getByRole('link', { name: /Contract review · Legal/ })).toBeVisible()
  await page.request.patch(`${legal.api}/steps/${contract.id}`, { data: { statusId: done } })
  await expect(card).not.toContainText('Blocked by')

  // Removing a link; the history names it.
  await blockedBy.getByRole('button', { name: `Remove link: ${design.key} blocks ${make.key}` }).click()
  await expect(blockedBy.getByRole('button', { name: `${design.key} Design` })).toHaveCount(0)
  await page.getByRole('tab', { name: 'History' }).click()
  await expect(page.getByText(`unlinked ${design.key} from blocking ${make.key}`)).toBeVisible()
  await expect(page.getByText(`linked ${contract.key} as blocking ${make.key}`)).toBeVisible()

  // The cross-map link opens the step on its own map.
  await page.getByRole('tab', { name: 'Details' }).click()
  await blockedBy.getByRole('link', { name: /Contract review/ }).click()
  await expect(page).toHaveURL(new RegExp(`/maps/${legal.id}`))
  await expect(page.getByRole('complementary', { name: `Step ${contract.key}` })).toBeVisible()
  await expect(page.getByRole('group', { name: `Blocks for ${contract.key}` })).toContainText(make.key)
})
