import { expect, test, type Page } from '@playwright/test'

// Runs after the other specs, so the owner and their workspaces exist.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

/** Clickable things on the page whose cursor is not the hand (drag handles may show grab). */
async function offenders(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const sel = 'button, a[href], [role="button"], [role="tab"], [role="option"], [role="menuitem"], select, summary'
    const out: string[] = []
    for (const el of Array.from(document.querySelectorAll<HTMLElement>(sel))) {
      const r = el.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      const cursor = getComputedStyle(el).cursor
      const disabled = (el as HTMLButtonElement).disabled || el.getAttribute('aria-disabled') === 'true'
      const ok = disabled ? cursor === 'not-allowed' : cursor === 'pointer' || (cursor === 'grab' && el.matches('.grab, .grab *'))
      if (!ok) out.push(`${el.tagName.toLowerCase()} "${(el.getAttribute('aria-label') ?? el.textContent ?? '').trim().slice(0, 40)}": ${cursor}`)
    }
    return out
  })
}

test('a big map scrolls inside the window, so its scrollbars are always in view', async ({ page }) => {
  await signIn(page)
  const ws = '/api/workspaces/deps'
  const map = (await (await page.request.post(`${ws}/maps`, { data: { name: 'Big' } })).json()) as { id: string }
  const api = `${ws}/maps/${map.id}`
  const obj = (await (await page.request.post(`${api}/nodes`, { data: { level: 'objective', title: 'Everything' } })).json()) as { id: string }
  let act = ''
  for (let i = 1; i <= 12; i++) {
    act = ((await (await page.request.post(`${api}/nodes`, { data: { level: 'activity', title: `Activity ${i}`, parentId: obj.id } })).json()) as { id: string }).id
  }
  for (let i = 1; i <= 25; i++) await page.request.post(`${api}/steps`, { data: { title: `Step ${i}`, activityId: act, releaseId: null } })
  await page.goto(`/w/deps/maps/${map.id}`)
  await expect(page.getByRole('button', { name: /Step 25$/ })).toBeAttached()
  const m = await page.evaluate(() => {
    const s = document.querySelector('.map-scroll')!
    const r = s.getBoundingClientRect()
    return { page: document.documentElement.scrollHeight, win: innerHeight, bottom: r.bottom, wide: s.scrollWidth > s.clientWidth, tall: s.scrollHeight > s.clientHeight }
  })
  // The page itself does not scroll; the map does, and its box ends at the window's bottom edge.
  expect(m.page).toBeLessThanOrEqual(m.win)
  expect(Math.round(m.bottom)).toBe(m.win)
  expect(m.wide && m.tall).toBe(true)
})

test('everything clickable shows the hand cursor', async ({ page }) => {
  await signIn(page)
  const slug = 'deps' // from step-dependencies.spec.ts: a map with steps, links and a panel to open
  const maps = (await (await page.request.get(`/api/workspaces/${slug}/maps`)).json()) as { id: string; name: string }[]
  const map = maps.find((m) => m.name === 'Release plan')!
  for (const path of [`/w/${slug}/maps`, `/w/${slug}/members`, `/w/${slug}/workflows`, `/w/${slug}/labels`, `/w/${slug}/settings`]) {
    await page.goto(path)
    await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible()
    expect(await offenders(page), path).toEqual([])
  }
  await page.goto(`/w/${slug}/maps/${map.id}`)
  // The map's live connection shows as an icon beside who is on the map.
  await expect(page.getByLabel('On this map now').getByRole('status', { name: /^Live/ })).toBeVisible()
  await expect(page.locator('header.topnav').getByRole('status')).toHaveCount(0)
  await page.locator('.step-card').first().getByRole('button').first().click()
  await expect(page.getByRole('complementary', { name: /^Step / })).toBeVisible()
  await page.getByRole('button', { name: /^Actions for map/ }).click()
  expect(await offenders(page), 'map').toEqual([])
})
