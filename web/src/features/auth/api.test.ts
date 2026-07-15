import { afterEach, expect, it, vi } from 'vitest'

import { apiFetch, login } from './api'

afterEach(() => vi.unstubAllGlobals())

it('attaches same-origin credentials and CSRF to unsafe requests', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ authenticated: true, username: 'admin', csrf_token: 'csrf-value' }), { status: 200 }))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
  vi.stubGlobal('fetch', fetchMock)

  await login('admin', 'correct horse battery staple')
  await apiFetch('/api/v1/settings', { method: 'PUT', body: '{}' })

  const init = fetchMock.mock.calls[1][1] as RequestInit
  expect(init.credentials).toBe('same-origin')
  expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('csrf-value')
})

it('dispatches one unauthorized event for repeated 401 responses', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 401 })))
  const listener = vi.fn()
  window.addEventListener('flowlens:unauthorized', listener)
  await apiFetch('/api/v1/overview')
  await apiFetch('/api/v1/domains')
  window.removeEventListener('flowlens:unauthorized', listener)
  expect(listener).toHaveBeenCalledTimes(1)
})

