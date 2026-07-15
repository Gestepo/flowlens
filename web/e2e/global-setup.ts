import { request } from '@playwright/test'
import { mkdir } from 'node:fs/promises'

export default async function globalSetup() {
  const baseURL = process.env.FLOWLENS_E2E_URL ?? 'http://127.0.0.1:8088'
  const username = process.env.FLOWLENS_E2E_USERNAME
  const password = process.env.FLOWLENS_E2E_PASSWORD
  if (!username || !password) throw new Error('FLOWLENS_E2E_USERNAME and FLOWLENS_E2E_PASSWORD are required')
  const context = await request.newContext({ baseURL })
  const response = await context.post('/api/v1/auth/login', { data: { username, password } })
  if (!response.ok()) throw new Error(`FlowLens E2E login failed with ${response.status()}`)
  await mkdir('test-results', { recursive: true })
  await context.storageState({ path: 'test-results/.auth.json' })
  await context.dispose()
}

