import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: false,
  retries: 0,
  reporter: 'line',
  use: {
    baseURL: process.env.FLOWLENS_E2E_URL ?? 'http://127.0.0.1:8088',
    storageState: 'test-results/.auth.json',
    screenshot: 'only-on-failure',
  },
})
