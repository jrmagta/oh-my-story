import { expect, test, type Page } from '@playwright/test'

// Runs after step-map.spec.ts and uses its workspace and map.
test.describe.configure({ mode: 'serial' })

const slug = 'mapping'

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill('owner@example.com')
  await page.getByLabel('Password').fill('owner-password-123')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

const statuses = (page: Page, workflow: string) =>
  page.getByRole('list', { name: `Statuses in ${workflow}` }).getByRole('button', { name: /^Rename status / })

async function openStep(page: Page) {
  await page.goto(`/w/${slug}/maps`)
  await page.getByRole('link', { name: 'Open Onboarding' }).click()
  await page.getByRole('button', { name: /^MAPPING-1 / }).click()
}

test('an admin adds a status, a step uses it, and deleting it moves the step on', async ({ page }) => {
  await signIn(page)
  await page.goto(`/w/${slug}`)
  await page.getByRole('link', { name: 'Workflows' }).click()
  await expect(page.getByRole('heading', { name: 'Workflows', exact: true })).toBeVisible()
  await expect(statuses(page, 'Default')).toHaveText(['To do', 'In progress', 'Blocked', 'Done'])

  const add = page.getByRole('form', { name: 'Add a status to Default' })
  await add.getByLabel('New status').fill('Review')
  await add.getByLabel('Category').selectOption({ label: 'In progress' })
  await add.getByRole('button', { name: 'Add status' }).click()
  await expect(statuses(page, 'Default')).toHaveText(['To do', 'In progress', 'Blocked', 'Done', 'Review'])

  await page.getByRole('button', { name: 'Move Review up' }).click()
  await expect(statuses(page, 'Default')).toHaveText(['To do', 'In progress', 'Blocked', 'Review', 'Done'])
  await page.getByRole('button', { name: 'Rename status Review' }).click()
  await page.getByRole('textbox', { name: 'New name for status Review' }).fill('In review')
  await page.keyboard.press('Enter')
  await expect(statuses(page, 'Default')).toHaveText(['To do', 'In progress', 'Blocked', 'In review', 'Done'])

  // The map offers the new status, in workflow order, and the card shows it.
  await openStep(page)
  const status = page.getByRole('complementary', { name: 'Step MAPPING-1' }).getByLabel('Status')
  await expect(status.locator('option')).toHaveText(['To do', 'In progress', 'Blocked', 'In review', 'Done'])
  await status.selectOption({ label: 'In review' })
  await expect(page.getByRole('button', { name: /^MAPPING-1 / })).toContainText('In review')

  // Deleting it asks where its steps go.
  await page.getByRole('link', { name: 'Workflows' }).click()
  await page.getByRole('button', { name: 'Delete In review' }).click()
  const confirm = page.getByRole('form', { name: 'Delete In review' })
  await confirm.getByLabel('Move its steps to').selectOption({ label: 'Done' })
  await confirm.getByRole('button', { name: 'Delete In review' }).click()
  await expect(statuses(page, 'Default')).toHaveText(['To do', 'In progress', 'Blocked', 'Done'])

  await openStep(page)
  await expect(page.getByRole('button', { name: /^MAPPING-1 / })).toContainText('Done')
})

test('a new workflow can back a new map', async ({ page }) => {
  await signIn(page)
  await page.goto(`/w/${slug}/workflows`)
  await page.getByLabel('Workflow name').fill('Design')
  await page.getByRole('button', { name: 'Create workflow' }).click()
  await expect(statuses(page, 'Design')).toHaveText(['To do', 'In progress', 'Blocked', 'Done'])
  await page.getByRole('button', { name: 'Rename status To do', exact: true }).last().click()
  await page.getByRole('textbox', { name: 'New name for status To do' }).fill('Ideas')
  await page.keyboard.press('Enter')
  await expect(statuses(page, 'Design')).toHaveText(['Ideas', 'In progress', 'Blocked', 'Done'])
  await expect(statuses(page, 'Default')).toHaveText(['To do', 'In progress', 'Blocked', 'Done'])

  await page.goto(`/w/${slug}/maps`)
  await page.getByLabel('Map name').fill('Design system')
  await page.getByLabel('Workflow').selectOption({ label: 'Design' })
  await page.getByRole('button', { name: 'Create map' }).click()
  await expect(page.getByRole('heading', { name: 'Design system' })).toBeVisible()

  await page.getByRole('button', { name: 'Add objective' }).click()
  await page.getByRole('textbox', { name: 'New objective' }).fill('Tokens')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Add activity to Tokens' }).click()
  await page.getByRole('textbox', { name: 'New activity in Tokens' }).fill('Colour')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Add step to Colour / Unscheduled' }).click()
  await page.getByRole('textbox', { name: 'New step in Colour / Unscheduled' }).fill('Brand colours')
  await page.keyboard.press('Enter')
  // New steps start in the workflow's first status.
  await expect(page.getByRole('group', { name: 'Steps in Colour / Unscheduled' })).toContainText('Ideas')
})
