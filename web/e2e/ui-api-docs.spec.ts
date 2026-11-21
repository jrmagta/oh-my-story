import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('the API documentation shows every endpoint and tries them out as the signed-in person', async ({ page }) => {
  await signIn(page)
  // The page runs under the app's strict Content-Security-Policy; nothing on it may be refused or break.
  // (Listening starts after sign-in: the login page's own check of /api/me is a normal 401.)
  const problems: string[] = []
  page.on('console', (m) => m.type() === 'error' && problems.push(m.text()))
  page.on('pageerror', (e) => problems.push(e.message))
  await page.goto('/docs/api')
  const docs = page.getByRole('main', { name: 'API documentation' })
  await expect(docs.getByRole('heading', { name: /oh-my-story API/ })).toBeVisible()
  // Grouped by area, every operation listed (collapsed).
  await expect(docs.getByRole('heading', { name: 'Webhooks' })).toBeVisible()
  await expect(docs.locator('.opblock')).toHaveCount(await page.request.get('/api/openapi.yaml').then(async (r) => ((await r.text()).match(/operationId:/g) ?? []).length))

  // Try GET /api/me: it answers as the signed-in owner.
  const me = docs.locator('.opblock', { has: page.locator('.opblock-summary-path', { hasText: /^\/api\/me$/ }) })
  await me.locator('.opblock-summary').click()
  await me.getByRole('button', { name: 'Try it out' }).click()
  await me.getByRole('button', { name: 'Execute' }).click()
  const live = me.locator('.live-responses-table')
  await expect(live.locator('tbody .response-col_status')).toHaveText('200')
  await expect(live.locator('tbody .response-col_description')).toContainText(owner.email)

  // Nothing was refused by the security policy or broke along the way.
  expect(problems).toEqual([])
})
