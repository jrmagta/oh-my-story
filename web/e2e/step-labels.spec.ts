import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
test.describe.configure({ mode: 'serial' })

const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'labels'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('labels are created from a step, shown on its card, and renamed in settings', async ({ page }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Labels', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.status()).toBe(201)
    return (await r.json()) as { id: string }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Shop' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Buy' })
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Sign in', parentId: obj.id })
  await post(`${api}/steps`, { title: 'Enter password', activityId: act.id, releaseId: null })
  const mapUrl = `/w/${slug}/maps/${map.id}`
  await page.goto(mapUrl)

  const card = page.getByRole('button', { name: 'LABELS-1 Enter password', exact: true })
  await card.click()
  const panel = page.getByRole('complementary', { name: 'Step LABELS-1' })
  const picker = panel.getByRole('group', { name: 'Labels' })

  // Enter creates a label that does not exist yet; the Create button does the same.
  await panel.getByRole('button', { name: '+ Label' }).click()
  await picker.getByLabel('Find or create a label').fill('auth')
  await picker.getByLabel('Find or create a label').press('Enter')
  await expect(card).toContainText('auth')
  await picker.getByLabel('Find or create a label').fill('email')
  await picker.getByRole('button', { name: 'Create “email”' }).click()
  await expect(card).toContainText('email')
  await expect(picker.getByRole('button', { name: /^auth/ })).toHaveAttribute('aria-pressed', 'true')

  // Toggling takes a label off; typing its name and Enter puts it back.
  await picker.getByRole('button', { name: /^auth/ }).click()
  await expect(card).not.toContainText('auth')
  await picker.getByLabel('Find or create a label').fill('AUTH')
  await picker.getByLabel('Find or create a label').press('Enter')
  await expect(card).toContainText('auth')
  await page.keyboard.press('Escape')
  await expect(picker).toHaveCount(0)

  // The chip's ✕ removes it too.
  await panel.getByRole('button', { name: 'Remove label email' }).click()
  await expect(card).not.toContainText('email')

  await panel.getByRole('tab', { name: 'History' }).click()
  await expect(panel).toContainText('labelled LABELS-1 “auth”')
  await expect(panel).toContainText('removed “email” from LABELS-1')

  // Renaming a label in settings renames it on every card.
  await page.goto(`/w/${slug}/labels`)
  await expect(page.getByRole('list', { name: 'Labels' }).getByRole('listitem')).toHaveCount(2)
  await page.getByRole('button', { name: 'Rename label auth' }).click()
  await page.getByRole('textbox', { name: 'New name for label auth' }).fill('login')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('button', { name: 'Rename label login' })).toBeVisible()
  await page.goto(mapUrl)
  await expect(card).toContainText('login')
  await expect(card).not.toContainText('auth')
})
