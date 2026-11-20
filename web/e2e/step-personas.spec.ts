import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'personas'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('personas are linked to objectives and shown on them', async ({ page }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Personas', slug } })).status()).toBe(201)

  await page.goto(`/w/${slug}/personas`)
  await page.getByLabel('Persona name').fill('Admin')
  await page.getByLabel('Description').fill('Sets up the workspace')
  await page.getByRole('button', { name: 'Create persona' }).click()
  await expect(page.getByRole('list', { name: 'Personas' })).toContainText('Sets up the workspace')

  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Onboarding' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Grow' })
  await post(`${api}/nodes`, { level: 'activity', title: 'Sign up', parentId: obj.id })
  const mapUrl = `/w/${slug}/maps/${map.id}`
  await page.goto(mapUrl)

  // Only objectives take personas.
  await expect(page.getByRole('button', { name: 'Personas of activity Sign up' })).toHaveCount(0)
  const objective = page.locator('.map-obj', { hasText: 'Grow' })
  await page.getByRole('button', { name: 'Personas of objective Grow' }).click()
  const picker = page.getByRole('group', { name: 'Personas' })
  await picker.getByRole('button', { name: /^Admin/ }).click()
  await expect(objective.getByLabel('Personas: Admin')).toBeVisible()
  await picker.getByLabel('Find or create a persona').fill('Guest')
  await picker.getByLabel('Find or create a persona').press('Enter')
  await expect(objective.getByLabel('Personas: Admin, Guest')).toBeVisible()
  await page.keyboard.press('Escape')

  await page.reload()
  await expect(objective.getByLabel('Personas: Admin, Guest')).toBeVisible()
  await page.getByRole('button', { name: 'Activity', exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'Map activity' })).toContainText('linked “Admin” to objective “Grow”')

  // Deleting a persona unlinks it everywhere.
  await page.goto(`/w/${slug}/personas`)
  await page.getByRole('button', { name: 'Delete Guest' }).click()
  await page.getByRole('button', { name: 'Unlink from every objective: Guest' }).click()
  await expect(page.getByRole('list', { name: 'Personas' }).getByRole('listitem')).toHaveCount(1)
  await page.goto(mapUrl)
  await expect(objective.getByLabel('Personas: Admin')).toBeVisible()
})
