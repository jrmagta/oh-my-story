import { createHmac } from 'node:crypto'
import { createServer, type IncomingHttpHeaders } from 'node:http'
import type { AddressInfo } from 'node:net'
import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner. Uses its own workspace.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }
const slug = 'hooks'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('an admin adds a webhook, a change is delivered signed, and the delivery shows as sent', async ({ page }) => {
  // A receiver on this machine, as a self-hosted tool would be.
  const got: { headers: IncomingHttpHeaders; body: string }[] = []
  const server = createServer((req, res) => {
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      got.push({ headers: req.headers, body })
      res.writeHead(204).end()
    })
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  const url = `http://127.0.0.1:${(server.address() as AddressInfo).port}/hook`

  try {
    await signIn(page)
    const ws = `/api/workspaces/${slug}`
    expect((await page.request.post('/api/workspaces', { data: { name: 'Hooks', slug } })).status()).toBe(201)
    const post = async (path: string, data: object) => (await (await page.request.post(path, { data })).json()) as { id: string; step: { id: string } }
    const map = await post(`${ws}/maps`, { name: 'Pipeline' })
    const api = `${ws}/maps/${map.id}`
    const obj = await post(`${api}/nodes`, { level: 'objective', title: 'Ship' })
    const act = await post(`${api}/nodes`, { level: 'activity', title: 'Deploy', parentId: obj.id })

    // Add a webhook for step changes only; its secret is shown once.
    await page.goto(`/w/${slug}/settings`)
    const section = page.getByRole('region', { name: 'Webhooks' })
    await section.getByLabel('Payload URL').fill(url)
    await section.getByLabel('Only these').check()
    await section.getByRole('group', { name: 'Steps' }).getByLabel('step.create').check()
    await section.getByRole('button', { name: 'Add webhook' }).click()
    const secret = await section.getByRole('textbox', { name: 'Signing secret' }).inputValue()
    expect(secret).toMatch(/^whsec_/)
    const row = section.getByRole('listitem', { name: `Webhook ${url}` })
    await expect(row).toContainText('step.create, step.update')

    // A new step is delivered, signed with the secret.
    await post(`${api}/steps`, { title: 'Deploy to staging', activityId: act.id, releaseId: null })
    await expect.poll(() => got.length, { timeout: 15_000 }).toBe(1)
    const sig = 'sha256=' + createHmac('sha256', secret).update(got[0]!.body).digest('hex')
    expect(got[0]!.headers['x-oms-signature']).toBe(sig)
    expect(got[0]!.headers['x-oms-event']).toBe('step.create')
    const body = JSON.parse(got[0]!.body) as { type: string; map: { name: string }; event: { after: { title: string } } }
    expect(body.type).toBe('step.create')
    expect(body.map.name).toBe('Pipeline')
    expect(body.event.after.title).toBe('Deploy to staging')

    // Its deliveries list it as sent; a test ping is sent too.
    await row.getByRole('button', { name: 'Deliveries' }).click()
    const table = row.getByRole('table', { name: `Deliveries to ${url}` })
    await expect(table.getByRole('row').filter({ hasText: 'step.create' })).toContainText('sent')
    await expect(table.getByRole('row').filter({ hasText: 'step.create' })).toContainText('204')
    await row.getByRole('button', { name: 'Send test' }).click()
    await expect(table.getByRole('row').filter({ hasText: 'ping' })).toContainText('sent', { timeout: 15_000 })
    expect(got.some((g) => g.headers['x-oms-event'] === 'ping')).toBe(true)

    // Paused, it sends nothing; deleting it removes it.
    await row.getByLabel('Active').uncheck()
    await expect(row.getByLabel('Active')).not.toBeChecked()
    const before = got.length
    await post(`${api}/steps`, { title: 'Quiet', activityId: act.id, releaseId: null })
    await page.waitForTimeout(3000)
    expect(got.length).toBe(before)
    await row.getByRole('button', { name: `Delete webhook ${url}` }).click()
    await row.getByRole('button', { name: `Confirm delete: ${url}` }).click()
    await expect(section.getByText('No webhooks yet.')).toBeVisible()
  } finally {
    server.close()
  }
})
