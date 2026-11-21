import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'linked'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('one step on two maps: added from its panel, edited once, live on both, taken off one', async ({ page }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Linked', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string; key: string } }
  }
  const build = async (name: string, workflowId?: string) => {
    const map = await post(`${ws}/maps`, { name, workflowId })
    const api = `${ws}/maps/${map.id}`
    const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Account' })
    const act = await post(`${api}/nodes`, { level: 'activity', title: `Sign in on ${name}`, parentId: obj.id })
    await post(`${api}/releases`, { name: 'Beta' })
    return { id: map.id, api, act: act.id }
  }
  const web = await build('Web')
  const mobile = await build('Mobile')
  const flow = await post(`${ws}/workflows`, { name: 'Bugs' })
  await build('Bugs', flow.id)
  const step = (await post(`${web.api}/steps`, { title: 'Password login', activityId: web.act, releaseId: null })).step

  // Open it on Web and add it to Mobile, in that map's Beta release. Maps with another workflow are not offered.
  await page.goto(`/w/${slug}/maps/${web.id}`)
  const card = page.locator(`[data-step="${step.id}"]`)
  await card.getByRole('button').first().click()
  const maps = page.getByRole('region', { name: 'Maps' })
  await maps.getByRole('button', { name: '+ Add to a map' }).click()
  const pick = page.getByRole('group', { name: `Add ${step.key} to a map` })
  await expect(pick.getByRole('combobox', { name: 'Map' }).locator('option')).toHaveText(['Choose a map…', 'Mobile'])
  await pick.getByRole('combobox', { name: 'Map' }).selectOption({ label: 'Mobile' })
  await pick.getByRole('combobox', { name: 'Activity' }).selectOption({ label: 'Account › Sign in on Mobile' })
  await pick.getByRole('combobox', { name: 'Release' }).selectOption({ label: 'Beta' })
  await pick.getByRole('button', { name: 'Add', exact: true }).click()
  await expect(page.getByText(`Added ${step.key} to Mobile`)).toBeVisible()
  await expect(maps.getByRole('link', { name: 'Mobile' })).toBeVisible()
  await expect(card.locator('.linked-mark')).toHaveAttribute('title', 'Also on Mobile')

  // Mobile shows the same step, in Beta; an edit on Web reaches it live.
  const other = await page.context().newPage()
  await other.goto(`/w/${slug}/maps/${mobile.id}`)
  const there = other.locator(`[data-step="${step.id}"]`)
  await expect(there).toContainText('Password login')
  await expect(other.getByRole('group', { name: 'Steps in Sign in on Mobile / Beta' })).toContainText(step.key)
  await expect(other.getByRole('status', { name: /^Live/ })).toBeVisible()
  await page.getByRole('button', { name: 'Rename step' }).click()
  await page.getByRole('textbox', { name: 'New name for step' }).fill('Password or passkey login')
  await page.keyboard.press('Enter')
  await expect(there).toContainText('Password or passkey login')

  // Delete says it reaches both maps.
  await page.getByRole('button', { name: `Delete step ${step.key}` }).click()
  await expect(page.getByRole('button', { name: `Delete from all 2 maps: ${step.key}` })).toBeVisible()

  // Take it off Web: Mobile keeps it, and loses its "also on" mark live. Undo puts it back.
  await maps.getByRole('button', { name: 'Remove from this map' }).click()
  await expect(card).toHaveCount(0)
  await expect(there).toBeVisible()
  await expect(there.locator('.linked-mark')).toHaveCount(0)
  await page.getByRole('button', { name: 'Undo' }).click()
  await expect(card).toBeVisible()
  await expect(there.locator('.linked-mark')).toHaveAttribute('title', 'Also on Web')

  // History names the move between maps.
  await page.getByRole('button', { name: /^Actions for map/ }).click()
  await page.getByRole('button', { name: 'Activity', exact: true }).click()
  await expect(page.getByText(`put ${step.key} on “Mobile”`)).toBeVisible()
  await expect(page.getByText(`took ${step.key} off “Web”`)).toBeVisible()

  // The other map's entry opens the step there.
  await card.getByRole('button').first().click()
  await maps.getByRole('link', { name: 'Mobile' }).click()
  await expect(page).toHaveURL(new RegExp(`/maps/${mobile.id}`))
  await expect(page.getByRole('complementary', { name: `Step ${step.key}` })).toBeVisible()
})
