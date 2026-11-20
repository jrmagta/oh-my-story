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
    await page.getByRole('button', { name: `Colour of ${label}` }).click()
    await page.getByRole('group', { name: `Colours for ${label}` }).getByRole('button', { name: colour }).click()
  }
  await pick('activity Browse', 'Blue')
  await expect(activity).toHaveCSS('background-color', 'rgb(59, 130, 196)')
  await expect(activity).toHaveCSS('color', 'rgb(255, 255, 255)')
  await pick('activity Search', 'Amber')
  // Amber is light, so its text is black.
  await expect(page.locator('.map-act', { hasText: 'Search' })).toHaveCSS('color', 'rgb(0, 0, 0)')
  await pick('objective Buy', 'Green')

  await page.reload()
  await expect(activity).toHaveCSS('background-color', 'rgb(59, 130, 196)')
  await page.getByRole('button', { name: 'Colour of activity Browse' }).click()
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
    ]),
  )
})
