import { readFile } from 'node:fs/promises'
import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'export'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

/** Picks an export from the map's ⋯ menu and returns the downloaded file's name and bytes. */
async function exportAs(page: Page, mapName: string, kind: 'PNG' | 'PDF') {
  await page.getByRole('button', { name: `Actions for map ${mapName}` }).click()
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: `Export as ${kind}` }).click()
  const d = await download
  const bytes = await readFile((await d.path())!)
  if (process.env.EXPORT_SAVE_DIR) await d.saveAs(`${process.env.EXPORT_SAVE_DIR}/${d.suggestedFilename()}`)
  return { name: d.suggestedFilename(), bytes }
}

/** A PNG's width and height, from its IHDR chunk. */
function pngSize(b: Buffer) {
  expect(b.subarray(1, 4).toString()).toBe('PNG')
  return { width: b.readUInt32BE(16), height: b.readUInt32BE(20) }
}

test('the map and the roadmap export as PNG and PDF, as they are shown', async ({ page, browser }) => {
  await signIn(page)
  const ws = `/api/workspaces/${slug}`
  expect((await page.request.post('/api/workspaces', { data: { name: 'Export', slug } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; url: string }
  }
  const map = await post(`${ws}/maps`, { name: 'Launch plan' })
  const api = `${ws}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Launch', colour: '#BA3636' })
  const a1 = await post(`${api}/nodes`, { level: 'activity', title: 'Announce', parentId: obj.id, colour: '#7E57C2' })
  const a2 = await post(`${api}/nodes`, { level: 'activity', title: 'Support', parentId: obj.id })
  const mvp = await post(`${api}/releases`, { name: 'MVP', targetDate: '2027-01-31' })
  await post(`${api}/steps`, { title: 'Blog post', activityId: a1.id, releaseId: mvp.id })
  await post(`${api}/steps`, { title: 'Help centre', activityId: a2.id, releaseId: null })
  await page.goto(`/w/${slug}/maps/${map.id}`)

  // PNG of the map: a real image, named after the map and the date, at twice the screen size.
  const png = await exportAs(page, 'Launch plan', 'PNG')
  expect(png.name).toMatch(/^launch-plan-\d{4}-\d{2}-\d{2}\.png$/)
  const full = pngSize(png.bytes)
  expect(full.width).toBeGreaterThan(800)
  expect(full.height).toBeGreaterThan(300)
  // While exporting, the page gets its caption and padding back afterwards.
  await expect(page.locator('.map-export')).not.toHaveClass(/exporting/)

  // PDF of the map: one page.
  const pdf = await exportAs(page, 'Launch plan', 'PDF')
  expect(pdf.name).toMatch(/^launch-plan-\d{4}-\d{2}-\d{2}\.pdf$/)
  expect(pdf.bytes.subarray(0, 5).toString()).toBe('%PDF-')
  expect(pdf.bytes.toString('latin1').match(/\/Type\s*\/Page\b/g)).toHaveLength(1)

  expect(pdf.bytes.length).toBeLessThan(1_000_000)

  // What is exported is what is shown: focused on one release, the picture is shorter.
  await page.getByRole('button', { name: 'Focus release MVP' }).click()
  const focused = pngSize((await exportAs(page, 'Launch plan', 'PNG')).bytes)
  expect(focused.height).toBeLessThan(full.height)
  await page.getByRole('button', { name: 'Show all releases' }).first().click()

  // The roadmap exports too, named as such.
  await page.getByRole('group', { name: 'View' }).getByRole('button', { name: 'Roadmap' }).click()
  const roadmap = await exportAs(page, 'Launch plan', 'PNG')
  expect(roadmap.name).toMatch(/^launch-plan-roadmap-\d{4}-\d{2}-\d{2}\.png$/)
  pngSize(roadmap.bytes)

  // And so does a share link, without an account.
  const link = await post(`${api}/share-links`, {})
  const ctx = await browser.newContext({ acceptDownloads: true })
  const anon = await ctx.newPage()
  await anon.goto(new URL(link.url).pathname)
  const shared = await exportAs(anon, 'Launch plan', 'PDF')
  expect(shared.bytes.subarray(0, 5).toString()).toBe('%PDF-')
  await ctx.close()
})
