import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:18765',
    browserName: 'chromium',
    headless: true,
  },
  workers: 1,
  webServer: {
    command: 'node e2e/server.mjs',
    port: 18765,
    reuseExistingServer: false,
    timeout: 120_000,
  },
})
