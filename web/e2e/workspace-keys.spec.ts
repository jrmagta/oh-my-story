import { expect, test, type Page } from '@playwright/test'

// Runs after shell.spec.ts, which creates the owner.
const owner = { email: 'owner@example.com', password: 'owner-password-123' }

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('a new workspace gets a three-letter project key, and a taken one is flagged', async ({ page }) => {
  await signIn(page)
  await page.goto('/')
  const form = page.getByRole('region', { name: 'New workspace' })
  const key = form.getByLabel('Project key')
  const create = form.getByRole('button', { name: 'Create workspace' })

  await form.getByLabel('Workspace name').fill('Cheshire Cat')
  await expect(form.getByLabel('URL slug')).toHaveValue('cheshire-cat')
  await expect(key).toHaveValue('CHC')
  await expect(form).toContainText('Step IDs look like CHC-1')
  await create.click()
  await expect(page).toHaveURL(/\/w\/cheshire-cat\//)

  // The same name again: the suggestion skips the taken key.
  await page.goto('/')
  await form.getByLabel('Workspace name').fill('Cheshire Cat')
  await form.getByLabel('URL slug').fill('cheshire-cat-2')
  await expect(key).toHaveValue('CHE')

  // Typing a taken key flags it and blocks creating; the offered key fixes it.
  await key.fill('chc')
  await expect(key).toHaveValue('CHC')
  await expect(form.getByRole('status')).toContainText('CHC is taken.')
  await expect(create).toBeDisabled()
  await form.getByRole('button', { name: 'Use CHE' }).click()
  await expect(key).toHaveValue('CHE')
  await expect(create).toBeEnabled()
  await create.click()
  await expect(page).toHaveURL(/\/w\/cheshire-cat-2\//)
  await page.goto('/w/cheshire-cat-2/settings')
  await expect(page.getByLabel('Project key')).toHaveValue('CHE')
})
