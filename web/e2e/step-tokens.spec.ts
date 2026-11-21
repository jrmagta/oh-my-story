import { expect, request, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'tokens'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('an admin makes an API token, a script uses it, history names it, and revoking stops it', async ({ page, baseURL }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Tokens', slug } })).status()).toBe(201)
  const map = (await (await page.request.post(`${ws}/maps`, { data: { name: 'Pipeline' } })).json()) as { id: string }
  const api = `${ws}/maps/${map.id}`
  const obj = (await (await page.request.post(`${api}/nodes`, { data: { level: 'objective', title: 'Ship' } })).json()) as { id: string }
  const act = (await (await page.request.post(`${api}/nodes`, { data: { level: 'activity', title: 'Deploy', parentId: obj.id } })).json()) as { id: string }

  // Make a write token in Settings; it is shown once.
  await page.goto(`/w/${slug}/settings`)
  const section = page.getByRole('region', { name: 'API tokens' })
  await section.getByLabel('Name').fill('CI bot')
  await section.getByLabel('Access').selectOption('write')
  await section.getByRole('button', { name: 'Make a token' }).click()
  const token = await section.getByRole('textbox', { name: 'New token' }).inputValue()
  expect(token).toMatch(/^oms_/)
  const listed = section.getByRole('list', { name: 'API tokens' }).getByRole('listitem')
  await expect(listed).toHaveCount(1)
  await expect(listed).toContainText('CI bot')
  await expect(listed).toContainText('Read and write')
  await expect(listed).toContainText('not used yet')
  await expect(listed).not.toContainText(token)

  // A script with no session uses it.
  const script = await request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  const made = await script.post(`${api}/steps`, { data: { title: 'Deployed build 42', activityId: act.id, releaseId: null } })
  expect(made.status()).toBe(201)
  expect((await script.get('/api/me')).status()).toBe(403)

  // The map shows the step, and its history names the token.
  await page.goto(`/w/${slug}/maps/${map.id}`)
  await expect(page.getByRole('button', { name: /Deployed build 42/ })).toBeVisible()
  await page.getByRole('button', { name: /^Actions for map/ }).click()
  await page.getByRole('button', { name: 'Activity', exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'Map activity' })).toContainText('CI bot (token)')

  // Last use shows; revoking stops the token.
  await page.goto(`/w/${slug}/settings`)
  await expect(listed).toContainText('used just now')
  await section.getByRole('button', { name: 'Revoke token CI bot' }).click()
  await expect(section.getByText('No tokens yet.')).toBeVisible()
  expect((await script.get(api)).status()).toBe(401)
  await script.dispose()
})
