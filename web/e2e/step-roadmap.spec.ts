import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'roadmap'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('the roadmap lays releases across, flags late ones, follows live changes, and moves steps between releases', async ({ page, browser }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Roadmap', slug } })).status()).toBe(201)
  const call = async (method: 'post' | 'patch', path: string, data: object) => {
    const r = await page.request[method](path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string }; url: string }
  }
  const map = await call('post', `${ws}/maps`, { name: 'Launch' })
  const api = `${ws}/maps/${map.id}`
  const obj = await call('post', `${api}/nodes`, { level: 'objective', title: 'Grow' })
  const signUp = await call('post', `${api}/nodes`, { level: 'activity', title: 'Sign up', parentId: obj.id })
  const pay = await call('post', `${api}/nodes`, { level: 'activity', title: 'Pay', parentId: obj.id })
  const past = await call('post', `${api}/releases`, { name: 'Past', targetDate: '2020-01-31' })
  const next = await call('post', `${api}/releases`, { name: 'Next', targetDate: '2099-06-30' })
  const s1 = await call('post', `${api}/steps`, { title: 'Email sign up', activityId: signUp.id, releaseId: past.id })
  const s2 = await call('post', `${api}/steps`, { title: 'Card payment', activityId: pay.id, releaseId: past.id })
  await call('post', `${api}/steps`, { title: 'Social sign up', activityId: signUp.id, releaseId: next.id })
  const view = (await (await page.request.get(api)).json()) as { workflow: { statuses: { id: string; name: string }[] } }
  const done = view.workflow.statuses.find((s) => s.name === 'Done')!.id

  await page.goto(`/w/${slug}/maps/${map.id}`)
  await expect(page.getByRole('status').filter({ hasText: 'Live' })).toBeVisible()
  await page.getByRole('group', { name: 'View' }).getByRole('button', { name: 'Roadmap' }).click()
  await expect(page).toHaveURL(/view=roadmap/)
  const roadmap = page.getByRole('region', { name: 'Roadmap' })

  // Releases across with their dates; the past release with open steps is late.
  await expect(roadmap.getByRole('heading', { name: 'Release Past, late' })).toContainText('2 steps')
  await expect(roadmap.getByRole('heading', { name: 'Release Next' })).toBeVisible()
  await expect(roadmap.getByRole('group', { name: 'Steps in Sign up / Past' })).toContainText('Email sign up')
  await expect(roadmap.getByRole('group', { name: 'Steps in Pay / Past' })).toContainText('Card payment')
  await expect(roadmap.getByRole('group', { name: 'Steps in Sign up / Next' })).toContainText('Social sign up')

  // Someone else finishes both steps: the roadmap follows without a reload, and Past is no longer late.
  await call('patch', `${api}/steps/${s1.step.id}`, { statusId: done })
  await call('patch', `${api}/steps/${s2.step.id}`, { statusId: done })
  await expect(roadmap.getByRole('heading', { name: 'Release Past' })).toBeVisible()
  await expect(roadmap.getByRole('heading', { name: 'Release Past' }).getByRole('img')).toHaveAccessibleName(/^2 of 2 done/)

  // Moving a step to the earlier release on the roadmap moves it on the map too.
  await roadmap.getByRole('button', { name: /^ROA-3 / }).focus()
  await page.keyboard.press('Space')
  await expect(page.locator('.drag-ghost')).toBeVisible()
  await page.keyboard.press('ArrowLeft')
  // The highlight lands on that cell or on the card already in it ("next to it"); either is the Past release.
  const pastCell = roadmap.getByRole('group', { name: 'Steps in Sign up / Past' })
  await expect(pastCell.locator('.drop').or(pastCell.and(page.locator('.drop')))).toHaveCount(1)
  await page.keyboard.press('Space')
  await expect(roadmap.getByRole('group', { name: 'Steps in Sign up / Past' })).toContainText('Social sign up')
  await page.getByRole('group', { name: 'View' }).getByRole('button', { name: 'Map', exact: true }).click()
  await expect(page.getByRole('group', { name: 'Steps in Sign up / Past' })).toContainText('Social sign up')
  await expect(page).not.toHaveURL(/view=roadmap/)

  // The view is in the URL, and a share link can show the roadmap.
  await page.goto(`/w/${slug}/maps/${map.id}?view=roadmap`)
  await expect(page.getByRole('region', { name: 'Roadmap' })).toBeVisible()
  const link = await call('post', `${api}/share-links`, {})
  const ctx = await browser.newContext()
  const anon = await ctx.newPage()
  await anon.goto(`${new URL(link.url).pathname}?view=roadmap`)
  await expect(anon.getByRole('region', { name: 'Roadmap' }).getByRole('heading', { name: 'Release Past' })).toBeVisible()
  await ctx.close()
})
