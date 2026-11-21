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
  // The map's live connection shows as an icon in the top bar, and only while a map is open.
  await expect(page.locator('header.topnav').getByRole('status', { name: /^Live/ })).toBeVisible()
  await page.locator('header.topnav').getByRole('link', { name: 'Maps' }).click()
  await expect(page.locator('header.topnav').getByRole('status')).toHaveCount(0)
  await page.goBack()
  await page.locator('.step-card').first().getByRole('button').first().click()
  await expect(page.getByRole('complementary', { name: /^Step / })).toBeVisible()
  await page.getByRole('button', { name: /^Actions for map/ }).click()
  expect(await offenders(page), 'map').toEqual([])
})
