import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'colours'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('backbone cards take a colour, keep it after a reload, and go back to default', async ({ page }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Colours', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Shop' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Buy' })
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Browse', parentId: obj.id })
  await post(`${api}/nodes`, { level: 'activity', title: 'Search', parentId: obj.id })
  await post(`${api}/steps`, { title: 'Search by name', activityId: act.id, releaseId: null })
  await page.goto(`/w/${slug}/maps/${map.id}`)

  const activity = page.locator('.map-act', { hasText: 'Browse' })
  const pick = async (label: string, colour: string) => {
    await page.getByRole('button', { name: `Actions for ${label}` }).click()
    await page.getByRole('button', { name: 'Colour…' }).click()
    await page.getByRole('group', { name: `Colours for ${label}` }).getByRole('button', { name: colour }).click()
  }
  await pick('activity Browse', 'Blue')
  await expect(activity).toHaveCSS('background-color', 'rgb(59, 130, 196)')
  await expect(activity).toHaveCSS('color', 'rgb(255, 255, 255)')
  await pick('activity Search', 'Amber')
  // Amber is light, so its text is black.
  await expect(page.locator('.map-act', { hasText: 'Search' })).toHaveCSS('color', 'rgb(0, 0, 0)')
  await pick('objective Buy', 'Green')

  // The + on an activity's right edge opens a field in a new column right after it; entry stays
  // open for the next one (after the one just made), and Escape ends it.
  await page.locator('.map-act', { hasText: 'Search' }).hover()
  await page.getByRole('button', { name: 'Add activity after Search' }).click()
  const field = page.getByRole('textbox', { name: 'New activity in Buy' })
  await field.fill('Checkout')
  await field.press('Enter')
  await expect(page.getByRole('button', { name: 'Move activity Checkout' })).toBeVisible()
  await field.fill('Receipt')
  await field.press('Enter')
  await expect(page.getByRole('button', { name: 'Move activity Receipt' })).toBeVisible()
  await field.press('Escape')
  await expect(field).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Move activity / })).toHaveCount(4)

  await page.reload()
  await expect(activity).toHaveCSS('background-color', 'rgb(59, 130, 196)')
  await page.getByRole('button', { name: 'Actions for activity Browse' }).click()
  await page.getByRole('button', { name: 'Colour…' }).click()
  await expect(page.getByRole('group', { name: 'Colours for activity Browse' }).getByRole('button', { name: 'Blue' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await page.getByRole('group', { name: 'Colours for activity Browse' }).getByRole('button', { name: 'Default' }).click()
  await expect(activity).not.toHaveClass(/coloured/)
  const view = (await (await page.request.get(api)).json()) as { nodes: { title: string; colour: string | null }[] }
  expect(view.nodes.map((n) => [n.title, n.colour])).toEqual(
    expect.arrayContaining([
      ['Buy', '#549334'],
      ['Browse', null],
      ['Search', '#FCB131'],
      // Added from the menu after Search, so they took its amber.
      ['Checkout', '#FCB131'],
      ['Receipt', '#FCB131'],
    ]),
  )

  // An objective spans its activities, from the first one's left edge to the last one's right.
  const buy = (await page.locator('.map-obj', { hasText: 'Buy' }).boundingBox())!
  const first = (await page.locator('.map-act', { hasText: 'Browse' }).boundingBox())!
  const last = (await page.locator('.map-act', { hasText: 'Receipt' }).boundingBox())!
  expect(buy.x).toBeCloseTo(first.x, 0)
  expect(buy.x + buy.width).toBeCloseTo(last.x + last.width, 0)

  // A new objective takes the colour of the one to its left; its first activity, the activity to its left.
  await page.locator('.map-obj', { hasText: 'Buy' }).hover()
  await page.getByRole('button', { name: 'Add objective after Buy' }).click()
  await page.getByRole('textbox', { name: 'New objective' }).fill('Return')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect(page.locator('.map-obj', { hasText: 'Return' })).toHaveCSS('background-color', 'rgb(84, 147, 52)')
  await page.getByRole('button', { name: 'Add activity to Return' }).click()
  await page.getByRole('textbox', { name: 'New activity in Return' }).fill('Send back')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect(page.locator('.map-act', { hasText: 'Send back' })).toHaveCSS('background-color', 'rgb(252, 177, 49)')

  // The + after an objective's last activity adds another, in its colour.
  await page.locator('.map-act', { hasText: 'Receipt' }).hover()
  await page.getByRole('button', { name: 'Add activity after Receipt' }).click()
  await page.getByRole('textbox', { name: 'New activity in Buy' }).fill('Wish list')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect(page.locator('.map-act', { hasText: 'Wish list' })).toHaveCSS('background-color', 'rgb(252, 177, 49)')
  await expect(page.getByRole('button', { name: /^Move activity / })).toHaveCount(6)

  // The + on a card's left edge adds before it.
  await page.locator('.map-act', { hasText: 'Browse' }).hover()
  await page.getByRole('button', { name: 'Add activity before Browse' }).click()
  await page.getByRole('textbox', { name: 'New activity in Buy' }).fill('Look around')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect(page.locator('.map-act').first()).toContainText('Look around')
  await page.locator('.map-obj', { hasText: 'Buy' }).hover()
  await page.getByRole('button', { name: 'Add objective before Buy' }).click()
  await page.getByRole('textbox', { name: 'New objective' }).fill('Discover')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect(page.locator('.map-obj').first()).toContainText('Discover')
  // Nothing to its left, so it starts with the default look.
  await expect(page.locator('.map-obj').first()).not.toHaveClass(/coloured/)
})

test('steps take the colour of their activity until given their own', async ({ page }) => {
  await signIn(page)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string; step: { id: string; key: string } }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Steps' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Buy' })
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Browse', parentId: obj.id, colour: '#3B82C4' })
  const plain = await post(`${api}/nodes`, { level: 'activity', title: 'Pay', parentId: obj.id })
  const step = (await post(`${api}/steps`, { title: 'Filter by size', activityId: act.id, releaseId: null })).step
  const other = (await post(`${api}/steps`, { title: 'Card details', activityId: plain.id, releaseId: null })).step
  await page.goto(`/w/${slug}/maps/${map.id}`)

  const card = page.locator(`[data-step="${step.id}"]`)
  await expect(card).toHaveCSS('background-color', 'rgb(59, 130, 196)')
  await expect(card).toHaveCSS('color', 'rgb(255, 255, 255)')
  // Under an activity with no colour, a step keeps the default look.
  await expect(page.locator(`[data-step="${other.id}"]`)).not.toHaveClass(/coloured/)

  const label = `${step.key} Filter by size`
  await card.hover()
  await page.getByRole('button', { name: `Actions for ${label}` }).click()
  await page.getByRole('button', { name: 'Colour…' }).click()
  await page.getByRole('group', { name: `Colours for ${label}` }).getByRole('button', { name: 'Amber' }).click()
  await expect(card).toHaveCSS('background-color', 'rgb(252, 177, 49)')
  await expect(card).toHaveCSS('color', 'rgb(0, 0, 0)')

  await page.reload()
  await expect(card).toHaveCSS('background-color', 'rgb(252, 177, 49)')
  await card.hover()
  await page.getByRole('button', { name: `Actions for ${label}` }).click()
  await page.getByRole('button', { name: 'Colour…' }).click()
  await page.getByRole('group', { name: `Colours for ${label}` }).getByRole('button', { name: 'Activity colour' }).click()
  await expect(card).toHaveCSS('background-color', 'rgb(59, 130, 196)')
  const view = (await (await page.request.get(api)).json()) as { steps: { id: string; colour: string | null }[] }
  expect(view.steps.find((s) => s.id === step.id)?.colour).toBeNull()
})
