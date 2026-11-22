import { expect, test, type Locator, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'notes'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

// A 4x4 red PNG.
const png = 'iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAEElEQVR4nGP4z8AARwzEcQCukw/x0F8jngAAAABJRU5ErkJggg=='
const pdf = Buffer.from('%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n')

const width = (img: Locator) => img.evaluate((el: HTMLImageElement) => (el.complete ? el.naturalWidth : 0))

test('a comment carries files: a pasted image inline, others listed, live elsewhere, and edited off', async ({ page, browser }) => {
  await signIn(page)
  expect((await page.request.post('/api/workspaces', { data: { name: 'Notes', slug, keyPrefix: 'NTS' } })).status()).toBe(201)
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
  const stepUrl = `/w/${slug}/maps/${map.id}?step=${step.id}`

  // A second browser watches the step's comments.
  const other = await (await browser.newContext()).newPage()
  await signIn(other)
  await other.goto(stepUrl)
  await other.getByRole('tab', { name: /^Comments/ }).click()
  await expect(other.getByText('No comments yet.')).toBeVisible()

  await page.goto(stepUrl)
  await page.getByRole('tab', { name: /^Comments/ }).click()
  const box = page.getByRole('textbox', { name: 'New comment' })
  await box.fill('It breaks here:\n')

  // A pasted screenshot uploads and goes in the text; it is listed under the box.
  await box.evaluate((el: HTMLTextAreaElement, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
    const dt = new DataTransfer()
    dt.items.add(new File([bytes], 'image.png', { type: 'image/png' }))
    el.setSelectionRange(el.value.length, el.value.length)
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }))
  }, png)
  await expect(box).toHaveValue(/^It breaks here:\n!\[Pasted image [^\]]+\.png\]\(attachment:[0-9a-f-]{36}\)$/)
  const pending = page.getByRole('list', { name: 'Files for New comment' })
  await expect(pending.getByRole('listitem')).toHaveCount(1)

  // Attach picks files too; taking one off takes its link out of the text.
  await page.getByLabel('Files to attach to New comment').setInputFiles([
    { name: 'terms.pdf', mimeType: 'application/pdf', buffer: pdf },
    { name: 'build.log', mimeType: 'text/plain', buffer: Buffer.from('started\n') },
  ])
  await expect(pending.getByRole('listitem')).toHaveCount(3)
  await expect(box).toHaveValue(/\[build\.log\]\(attachment:/)
  await pending.getByRole('button', { name: 'Remove build.log' }).click()
  await expect(pending.getByRole('listitem')).toHaveCount(2)
  await expect(box).not.toHaveValue(/build\.log/)

  // The PDF stays attached but leaves the text, so it is listed under the comment.
  await box.fill((await box.inputValue()).replace(/\n\[terms\.pdf\]\(attachment:[0-9a-f-]+\)/, ''))
  await page.getByRole('button', { name: 'Comment', exact: true }).click()
  await expect(pending).toHaveCount(0)

  for (const p of [page, other]) {
    const comment = p.getByRole('article', { name: 'Comment by Olive Owner' })
    await expect.poll(() => width(comment.getByRole('img', { name: /^Pasted image/ }))).toBe(4)
    const listed = comment.getByRole('list', { name: 'Files of the comment by Olive Owner' })
    await expect(listed.getByRole('listitem')).toHaveCount(1)
    await expect(listed.getByRole('link', { name: 'terms.pdf' })).toBeVisible()
  }
  // A comment's files are not the step's.
  await page.getByRole('tab', { name: 'Details' }).click()
  await expect(page.getByRole('region', { name: 'Attachments' })).toContainText('Drop files here, or use Add files.')
  await page.getByRole('tab', { name: /^Comments/ }).click()

  // Editing takes the PDF off; the other browser follows.
  const comment = page.getByRole('article', { name: 'Comment by Olive Owner' })
  await comment.getByRole('button', { name: 'Edit' }).click()
  await comment.getByRole('list', { name: 'Files for Edit comment' }).getByRole('button', { name: 'Remove terms.pdf' }).click()
  await comment.getByRole('button', { name: 'Save' }).click()
  await expect(comment.getByRole('link', { name: 'terms.pdf' })).toHaveCount(0)
  await expect(other.getByRole('article', { name: 'Comment by Olive Owner' }).getByRole('link', { name: 'terms.pdf' })).toHaveCount(0)
  await expect.poll(() => width(comment.getByRole('img', { name: /^Pasted image/ }))).toBe(4)
})
