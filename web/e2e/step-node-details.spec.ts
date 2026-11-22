import { expect, test, type Locator, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'details'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

// A 4x4 red PNG.
const png = 'iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAEElEQVR4nGP4z8AARwzEcQCukw/x0F8jngAAAABJRU5ErkJggg=='

const width = (img: Locator) => img.evaluate((el: HTMLImageElement) => (el.complete ? el.naturalWidth : 0))

test('an activity has details: a description with an inline image and files, counted on its card', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Details', slug, keyPrefix: 'DTL' } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; url: string; step: { id: string; key: string } }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Checkout' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Buy' })
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Pay', parentId: obj.id })
  const { step } = await post(`${api}/steps`, { title: 'Card form', activityId: act.id, releaseId: null })
  const mapUrl = `/w/${slug}/maps/${map.id}`

  // A second browser watches the map.
  const other = await (await browser.newContext()).newPage()
  await signIn(other)
  await other.goto(mapUrl)
  await expect(other.getByRole('button', { name: 'Actions for activity Pay' })).toBeVisible()

  // The ⋯ menu opens the activity's details, kept in the URL.
  await page.goto(mapUrl)
  await page.getByRole('button', { name: 'Actions for activity Pay' }).click()
  await page.getByRole('button', { name: 'Details' }).click()
  const panel = page.getByRole('complementary', { name: 'Activity Pay' })
  await expect(panel).toBeVisible()
  await expect(panel).toContainText('Buy')
  await expect(page).toHaveURL(new RegExp(`node=${act.id}`))

  // A file attaches, and the card counts it, here and live in the other browser.
  await panel.getByLabel('Files to attach').setInputFiles({ name: 'flow.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4\n%%EOF\n') })
  await expect(panel.getByRole('list', { name: 'Files' }).getByRole('link', { name: 'flow.pdf' })).toBeVisible()
  await expect(page.getByRole('button', { name: '1 file on activity Pay' })).toBeVisible()
  await expect(other.getByRole('button', { name: '1 file on activity Pay' })).toBeVisible()

  // The description takes a pasted image, which shows after saving and after a reload.
  const description = panel.getByRole('region', { name: 'Description' })
  await description.getByRole('button', { name: 'Edit' }).click()
  const editor = description.getByLabel('Description (Markdown)')
  await editor.fill('How paying works:\n')
  await editor.evaluate((el: HTMLTextAreaElement, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
    const dt = new DataTransfer()
    dt.items.add(new File([bytes], 'image.png', { type: 'image/png' }))
    el.setSelectionRange(el.value.length, el.value.length)
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }))
  }, png)
  await expect(editor).toHaveValue(/\(attachment:[0-9a-f-]{36}\)$/)
  await description.getByRole('button', { name: 'Save' }).click()
  await expect.poll(() => width(description.getByRole('img', { name: /^Pasted image/ }))).toBe(4)
  await expect(page.getByRole('button', { name: '2 files on activity Pay' })).toBeVisible()
  await expect(other.getByRole('button', { name: '2 files on activity Pay' })).toBeVisible()

  await page.reload()
  await expect.poll(() => width(panel.getByRole('region', { name: 'Description' }).getByRole('img', { name: /^Pasted image/ }))).toBe(4)

  // Opening a step swaps the panel; the paperclip on the card brings the details back.
  await page.locator(`[data-step="${step.id}"]`).getByRole('button', { name: `${step.key} Card form`, exact: true }).click()
  await expect(panel).toHaveCount(0)
  await page.getByRole('button', { name: '2 files on activity Pay' }).click()
  await expect(panel).toBeVisible()
  await expect(page).not.toHaveURL(/step=/)

  // A file deleted from the details goes from the count, with Undo.
  await panel.getByRole('button', { name: 'Delete flow.pdf' }).click()
  await expect(page.getByRole('button', { name: '1 file on activity Pay' })).toBeVisible()
  await page.getByRole('button', { name: 'Undo' }).click()
  await expect(page.getByRole('button', { name: '2 files on activity Pay' })).toBeVisible()

  // A share link shows the details read-only, image and files included.
  const link = await post(`${api}/share-links`, {})
  const anon = await (await browser.newContext()).newPage()
  await anon.goto(new URL(link.url).pathname)
  await anon.getByRole('button', { name: '2 files on activity Pay' }).click()
  const shared = anon.getByRole('complementary', { name: 'Activity Pay' })
  await expect.poll(() => width(shared.getByRole('img', { name: /^Pasted image/ }))).toBe(4)
  await expect(shared.getByRole('list', { name: 'Files' }).getByRole('listitem')).toHaveCount(2)
  await expect(shared.getByRole('button', { name: 'Edit' })).toHaveCount(0)
})
