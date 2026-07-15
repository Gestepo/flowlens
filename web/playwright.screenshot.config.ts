import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './screenshot',
  testMatch: 'public-overview.spec.ts',
  reporter: 'line',
  use: {
    baseURL: 'http://127.0.0.1:4174',
    viewport: { width: 1440, height: 900 },
    screenshot: 'off',
  },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4174',
    url: 'http://127.0.0.1:4174',
    reuseExistingServer: false,
  },
})
