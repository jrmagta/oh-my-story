import { expect, test, type Locator, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'inline'

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

test('an image pasted into a description shows inline, after a reload and on a share link', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Inline', slug, keyPrefix: 'INL' } })).status()).toBe(201)
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
  await page.goto(`/w/${slug}/maps/${map.id}`)
  await page.locator(`[data-step="${step.id}"]`).getByRole('button', { name: `${step.key} Card form`, exact: true }).click()

  const description = page.getByRole('region', { name: 'Description' })
  await description.getByRole('button', { name: 'Edit' }).click()
  const editor = description.getByLabel('Description (Markdown)')
  await editor.fill('The form:\n')

  // Paste a screenshot (browsers call it image.png): it uploads and its Markdown lands at the cursor.
  await editor.evaluate((el: HTMLTextAreaElement, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
    const dt = new DataTransfer()
    dt.items.add(new File([bytes], 'image.png', { type: 'image/png' }))
    el.setSelectionRange(el.value.length, el.value.length)
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }))
  }, png)
  await expect(editor).toHaveValue(/^The form:\n!\[Pasted image [\d-]+ [\d.]+\.png\]\(attachment:[0-9a-f-]{36}\)$/)

  // Dropping another kind of file inserts a link to it.
  await editor.evaluate((el: HTMLTextAreaElement) => {
    el.setSelectionRange(el.value.length, el.value.length)
    const dt = new DataTransfer()
    dt.items.add(new File(['started\n'], 'build.log', { type: 'text/plain' }))
    el.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true, cancelable: true }))
  })
  await expect(editor).toHaveValue(/\)\n\[build\.log\]\(attachment:[0-9a-f-]{36}\)$/)
  await description.getByRole('button', { name: 'Save' }).click()

  // Saved, the image shows inline and the log is a link; both are in the step's files.
  const image = description.getByRole('img', { name: /^Pasted image/ })
  await expect.poll(() => width(image)).toBe(4)
  await expect(description.getByRole('link', { name: 'build.log' })).toHaveAttribute('href', new RegExp(`^/api/workspaces/${slug}/attachments/`))
  await expect(page.getByRole('region', { name: 'Attachments' }).getByRole('listitem')).toHaveCount(2)

  await page.reload()
  await expect.poll(() => width(description.getByRole('img', { name: /^Pasted image/ }))).toBe(4)

  // Through a share link, with no session, the image and the files still load.
  const link = await post(`${api}/share-links`, {})
  const anon = await (await browser.newContext()).newPage()
  await anon.goto(new URL(link.url).pathname)
  await anon.locator(`[data-step="${step.id}"]`).getByRole('button', { name: `${step.key} Card form`, exact: true }).click()
  const shared = anon.getByRole('region', { name: 'Description' }).getByRole('img', { name: /^Pasted image/ })
  await expect.poll(() => width(shared)).toBe(4)
  const files = anon.getByRole('region', { name: 'Attachments' })
  await expect(files.getByRole('listitem')).toHaveCount(2)
  await expect(files.getByRole('link', { name: 'build.log' })).toHaveAttribute('href', /^\/api\/shared\//)
  await expect(files).not.toContainText('Olive Owner')
  await expect(files.getByRole('button', { name: /^Delete/ })).toHaveCount(0)
})
