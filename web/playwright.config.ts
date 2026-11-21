import { defineConfig, devices } from '@playwright/test'

// Run through `make e2e`, which builds the binary and resets the oms_e2e database.
// Keycloak and Mailpit come from the compose stack (`make up`). Keycloak's
// realm allows redirects back to localhost:18080.
const port = 18080
export const baseURL = `http://localhost:${port}`

export default defineConfig({
  testDir: 'e2e',
  // The tests share one instance and build on each other's state, in order.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL,
    ...devices['Desktop Chrome'],
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: '../bin/oh-my-story serve',
    url: `${baseURL}/healthz`,
    reuseExistingServer: false,
    stdout: 'pipe',
    env: {
      OMS_DATABASE_URL: process.env.E2E_DATABASE_URL ?? 'postgres://oms:oms@localhost:5432/oms_e2e?sslmode=disable',
      OMS_HTTP_ADDR: `127.0.0.1:${port}`,
      OMS_BASE_URL: baseURL,
      OMS_SMTP_ADDR: 'localhost:1025',
      OMS_MAIL_FROM: 'oh-my-story <no-reply@oh-my-story.local>',
      OMS_OIDC_ISSUER: 'http://localhost:8180/realms/oms',
      OMS_OIDC_CLIENT_ID: 'oh-my-story',
      OMS_OIDC_CLIENT_SECRET: 'dev-only-client-secret',
      OMS_OIDC_NAME: 'Keycloak',
      OMS_OIDC_DIRECTORY: 'keycloak',
    },
  },
})
