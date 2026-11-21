import { readFile } from 'node:fs/promises'
import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'nav'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

const scroller = (page: Page) =>
  page.evaluate(() => {
    const s = document.querySelector('.map-scroll')!
    return { left: s.scrollLeft, top: s.scrollTop, w: s.scrollWidth, cw: s.clientWidth, h: s.scrollHeight, ch: s.clientHeight }
  })

test('a big map zooms, fits, pans, keeps its backbone pinned, and has a minimap', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Nav', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => (await (await page.request.post(path, { data })).json()) as { id: string }
  const map = await post(`${ws}/maps`, { name: 'Journey' })
  const api = `${ws}/maps/${map.id}`
  const acts: string[] = []
  for (const o of ['Discover', 'Buy']) {
    const obj = await post(`${api}/nodes`, { level: 'objective', title: o })
    for (let i = 1; i <= 5; i++) acts.push((await post(`${api}/nodes`, { level: 'activity', title: `${o} ${i}`, parentId: obj.id })).id)
  }
  const rel = await post(`${api}/releases`, { name: 'MVP' })
  for (let i = 1; i <= 14; i++) await post(`${api}/steps`, { title: `Step ${i}`, activityId: acts[0], releaseId: rel.id })
  await post(`${api}/steps`, { title: 'Far step', activityId: acts[2], releaseId: null })
  await page.goto(`/w/${slug}/maps/${map.id}`)
  await expect(page.getByRole('button', { name: /Step 14$/ })).toBeVisible()
  const zoomLabel = page.getByRole('group', { name: 'Zoom and overview' }).getByRole('button', { name: /^Zoom \d+%/ })
  await expect(zoomLabel).toHaveText('100%')

  // Scrolling down keeps the objective and activity rows pinned at the top of the map.
  const top = (await page.locator('.map-scroll').boundingBox())!.y
  await page.locator('.map-scroll').evaluate((s) => (s.scrollTop = 600))
  const act = (await page.locator('.map-act').first().boundingBox())!
  const obj = (await page.locator('.map-obj').first().boundingBox())!
  expect(obj.y).toBeGreaterThanOrEqual(top)
  expect(obj.y - top).toBeLessThan(16)
  // Nothing shows above the pinned rows: the strip over the objectives is the page's own background.
  const above = await page.evaluate((y) => {
    const s = document.querySelector('.map-scroll')!.getBoundingClientRect()
    return document.elementFromPoint(s.left + 300, y + 3)?.closest('.backbone') !== null
  }, top)
  expect(above).toBe(true)
  expect(act.y).toBeGreaterThan(obj.y)
  await expect(page.locator('.map-act').first()).toBeInViewport()
  // An objective names itself on hover, even when its title is scrolled out of sight.
  await expect(page.locator('.map-obj').first()).toHaveAttribute('title', 'Discover')

  // Release bands fit the part of the map in view, whatever the scroll or zoom: Focus is always on screen.
  const bandInView = async () => {
    const band = (await page.getByRole('heading', { name: 'Release MVP' }).boundingBox())!
    const pane = (await page.locator('.map-scroll').boundingBox())!
    return band.x >= pane.x && band.x + band.width <= pane.x + pane.width
  }
  await page.locator('.map-scroll').evaluate((s) => s.scrollTo(600, 0))
  expect(await bandInView()).toBe(true)
  await expect(page.getByRole('button', { name: 'Focus release MVP' })).toBeInViewport()
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await expect.poll(bandInView).toBe(true)
  await page.keyboard.press('Control+0')
  await expect(zoomLabel).toHaveText('100%')

  // The minimap shows the map is bigger than the view; clicking its far corner moves there.
  const mini = page.getByRole('img', { name: /^Map overview/ })
  await expect(mini).toBeVisible()
  const mb = (await mini.boundingBox())!
  await page.mouse.click(mb.x + mb.width - 3, mb.y + 3)
  await expect.poll(async () => (await scroller(page)).left).toBeGreaterThan(0)
  await page.getByRole('button', { name: 'Minimap' }).click()
  await expect(mini).toHaveCount(0)
  await page.getByRole('button', { name: 'Minimap' }).click()
  await expect(mini).toBeVisible()

  // Dragging empty space pans.
  await page.locator('.map-scroll').evaluate((s) => s.scrollTo(0, 0))
  const sb = (await page.locator('.map-scroll').boundingBox())!
  const empty = (await page.getByRole('group', { name: 'Steps in Discover 5 / MVP' }).boundingBox())!
  // Below the cell's "+ Step" button (across its top), which does not pan.
  const at = { x: empty.x + empty.width / 2, y: empty.y + 50 }
  await page.mouse.move(at.x, at.y)
  await page.mouse.down()
  await page.mouse.move(at.x - 200, at.y + 5, { steps: 8 })
  await page.mouse.up()
  expect((await scroller(page)).left).toBeGreaterThan(150)

  // Zoom with the buttons, Ctrl+scroll and Ctrl+0; it is kept after a reload.
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await expect(zoomLabel).toHaveText('90%')
  await expect(page.locator('.map-zoom')).toHaveCSS('zoom', '0.9')
  await page.reload()
  await expect(zoomLabel).toHaveText('90%')
  await page.mouse.move(sb.x + 300, sb.y + 300)
  await page.keyboard.down('Control')
  await page.mouse.wheel(0, -100)
  await page.keyboard.up('Control')
  await expect(zoomLabel).toHaveText('100%')
  await page.keyboard.press('Control+-')
  await expect(zoomLabel).toHaveText('90%')
  await page.keyboard.press('Control+0')
  await expect(zoomLabel).toHaveText('100%')

  // Fit shrinks the map to the room it has (down to 50%), from the top left.
  await page.getByRole('button', { name: 'Fit', exact: true }).click()
  await expect(zoomLabel).not.toHaveText('100%')
  const fitted = await scroller(page)
  expect(fitted.w).toBeLessThanOrEqual(fitted.cw + 1)
  expect(fitted.left).toBe(0)

  // Pinned rows stay flush at the top when zoomed too.
  await page.keyboard.press('Control+0')
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await page.locator('.map-scroll').evaluate((s) => (s.scrollTop = 400))
  expect(
    await page.evaluate((y) => {
      const s = document.querySelector('.map-scroll')!.getBoundingClientRect()
      return document.elementFromPoint(s.left + 300, y + 2)?.closest('.backbone') !== null
    }, top),
  ).toBe(true)

  // Dragging a step still lands where it is dropped when zoomed (75%).
  await page.keyboard.press('Control+0')
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await page.getByRole('button', { name: 'Zoom out' }).click()
  await expect(zoomLabel).toHaveText('75%')
  // Centre the target cell: dragging near the map's edge would auto-scroll it away.
  await page.getByRole('group', { name: 'Steps in Discover 2 / Unscheduled' }).scrollIntoViewIfNeeded()
  const area = (await page.locator('.map-scroll').boundingBox())!
  const cell = (await page.getByRole('group', { name: 'Steps in Discover 2 / Unscheduled' }).boundingBox())!
  await page.locator('.map-scroll').evaluate((s, dx) => (s.scrollLeft += dx), cell.x + cell.width / 2 - (area.x + area.width / 2))
  await expect(page.getByRole('button', { name: /Far step$/ })).toBeInViewport()
  const from = (await page.getByRole('button', { name: /Far step$/ }).boundingBox())!
  // A cell to the left, clear of the zoom controls and minimap in the bottom-right corner.
  const target = (await page.getByRole('group', { name: 'Steps in Discover 2 / Unscheduled' }).boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 - 12, from.y + from.height / 2 + 12, { steps: 4 })
  await page.mouse.move(target.x + target.width / 2, target.y + 20, { steps: 12 })
  await page.mouse.up()
  await expect(page.getByRole('group', { name: 'Steps in Discover 2 / Unscheduled' })).toContainText('Far step')

  // Exports are at 100% whatever the zoom.
  const exportWidth = async () => {
    await page.getByRole('button', { name: /^Actions for map/ }).click()
    const download = page.waitForEvent('download')
    await page.getByRole('button', { name: 'Export as PNG' }).click()
    const b = await readFile((await (await download).path())!)
    return b.readUInt32BE(16)
  }
  const zoomed = await exportWidth()
  await page.keyboard.press('Control+0')
  await expect(zoomLabel).toHaveText('100%')
  expect(await exportWidth()).toBe(zoomed)
})
