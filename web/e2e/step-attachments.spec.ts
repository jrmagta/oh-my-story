import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'files'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

// A 4x4 red PNG.
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAEElEQVR4nGP4z8AARwzEcQCukw/x0F8jngAAAABJRU5ErkJggg==',
  'base64',
)
const pdf = Buffer.from('%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n')

test('files attach to a step, show on its card, open full size, and delete with undo', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Files', slug, keyPrefix: 'ATT' } })).status()).toBe(201)
  const post = async (path: string, data: object) => {
    const r = await page.request.post(path, { data })
    expect(r.ok()).toBe(true)
    return (await r.json()) as { id: string; step: { id: string; key: string } }
  }
  const map = await post(`/api/workspaces/${slug}/maps`, { name: 'Checkout' })
  const api = `/api/workspaces/${slug}/maps/${map.id}`
  const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Buy' })
  const act = await post(`${api}/nodes`, { level: 'activity', title: 'Pay', parentId: obj.id })
  const { step } = await post(`${api}/steps`, { title: 'Card form', activityId: act.id, releaseId: null })
  await page.goto(`/w/${slug}/maps/${map.id}`)

  // A second browser watches the same map.
  const other = await (await browser.newContext()).newPage()
  await signIn(other)
  await other.goto(`/w/${slug}/maps/${map.id}`)
  const otherCard = other.locator(`[data-step="${step.id}"]`)
  await expect(otherCard).toBeVisible()

  const card = page.locator(`[data-step="${step.id}"]`)
  await card.getByRole('button', { name: `${step.key} Card form`, exact: true }).click()
  const files = page.getByRole('region', { name: 'Attachments' })
  await expect(files).toContainText('Drop files here, or use Add files.')

  // Pick two files; both upload, list with their size, and count on the card, here and in the other browser.
  await page.getByLabel('Files to attach').setInputFiles([
    { name: 'mock-up.png', mimeType: 'image/png', buffer: png },
    { name: 'terms.pdf', mimeType: 'application/pdf', buffer: pdf },
  ])
  const list = files.getByRole('list', { name: 'Files' })
  await expect(list.getByRole('listitem')).toHaveCount(2)
  await expect(list.getByRole('link', { name: 'terms.pdf' })).toBeVisible()
  await expect(list).toContainText(`${pdf.length} B · Olive Owner · just now`)
  await expect(card.locator('.comment-count', { hasText: '2' })).toHaveAttribute('title', '2 files')
  await expect(otherCard.locator('.comment-count', { hasText: '2' })).toBeVisible()

  // Dropping a file on the section uploads it too.
  const drop = await page.evaluateHandle(() => {
    const dt = new DataTransfer()
    dt.items.add(new File(['started\n'], 'build.log', { type: 'text/plain' }))
    return dt
  })
  await files.dispatchEvent('dragover', { dataTransfer: drop })
  await expect(files).toHaveClass(/drop-over/)
  await files.dispatchEvent('drop', { dataTransfer: drop })
  await expect(list.getByRole('link', { name: 'build.log' })).toBeVisible()
  await expect(files).not.toHaveClass(/drop-over/)

  // Too large: the server's message shows against the file, and it can be dismissed.
  await page.getByLabel('Files to attach').setInputFiles({ name: 'huge.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(11 << 20) })
  await expect(files.getByRole('alert')).toHaveText('files can be at most 10 MB')
  await files.getByRole('button', { name: 'Dismiss huge.bin' }).click()
  await expect(files.getByRole('alert')).toHaveCount(0)

  // The image opens full size; Escape closes it.
  await list.getByRole('button', { name: 'View mock-up.png' }).click()
  const viewer = page.getByRole('dialog', { name: 'mock-up.png' })
  await expect(viewer).toBeVisible()
  expect(await viewer.getByRole('img', { name: 'mock-up.png' }).evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(4)
  await page.keyboard.press('Escape')
  await expect(viewer).toHaveCount(0)

  // Deleting offers Undo; both browsers follow.
  await list.getByRole('button', { name: 'Delete terms.pdf' }).click()
  await expect(list.getByRole('link', { name: 'terms.pdf' })).toHaveCount(0)
  await expect(otherCard.locator('.comment-count', { hasText: '2' })).toBeVisible()
  await page.getByRole('button', { name: 'Undo' }).click()
  await expect(list.getByRole('link', { name: 'terms.pdf' })).toBeVisible()
  await expect(otherCard.locator('.comment-count', { hasText: '3' })).toBeVisible()

  // The other browser's open panel follows a change made here.
  await otherCard.getByRole('button', { name: `${step.key} Card form`, exact: true }).click()
  const otherList = other.getByRole('region', { name: 'Attachments' }).getByRole('list', { name: 'Files' })
  await expect(otherList.getByRole('listitem')).toHaveCount(3)
  await list.getByRole('button', { name: 'Delete build.log' }).click()
  await expect(otherList.getByRole('listitem')).toHaveCount(2)

  // The step's history names the files.
  await page.getByRole('tab', { name: 'History' }).click()
  await expect(page.getByText(`removed “build.log” from ${step.key}`)).toBeVisible()
  await expect(page.getByText(`attached “mock-up.png” to ${step.key}`)).toBeVisible()
})
