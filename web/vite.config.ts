import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    exclude: ['e2e/**', 'screenshot/**', 'node_modules/**'],
    setupFiles: './src/test/setup.ts',
  },
})
