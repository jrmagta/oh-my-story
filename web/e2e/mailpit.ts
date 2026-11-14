import { expect } from '@playwright/test'

const mailpit = process.env.E2E_MAILPIT_URL ?? 'http://localhost:8025'

/** Delete earlier mail to addr, so the next invite link found is the new one. */
export async function clearMail(addr: string): Promise<void> {
  const res = await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${addr}"`)}`, { method: 'DELETE' })
  expect(res.ok, `clear Mailpit for ${addr}`).toBe(true)
}

/** Wait for an invite email to addr and return the link in it. */
export async function inviteLink(addr: string): Promise<string> {
  let link = ''
  await expect
    .poll(
      async () => {
        const res = await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${addr}"`)}`)
        const { messages } = (await res.json()) as { messages: { ID: string }[] }
        const first = messages[0]
        if (!first) return ''
        const msg = (await (await fetch(`${mailpit}/api/v1/message/${first.ID}`)).json()) as { Text: string }
        link = /http:\/\/localhost:18080\/invites\/[A-Za-z0-9_-]{43}/.exec(msg.Text)?.[0] ?? ''
        return link
      },
      { message: `invite email to ${addr}`, timeout: 10_000 },
    )
    .not.toBe('')
  return link
}
