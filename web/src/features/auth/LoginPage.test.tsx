import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { SessionBoundary } from './SessionBoundary'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

it('shows only login until authentication succeeds and then restores the page', async () => {
  let authenticated = false
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.endsWith('/api/v1/session')) {
      return new Response(authenticated ? JSON.stringify(sessionFixture()) : JSON.stringify({ error: { code: 'unauthorized' } }), { status: authenticated ? 200 : 401 })
    }
    if (url.endsWith('/api/v1/auth/login')) {
      authenticated = true
      return new Response(JSON.stringify(sessionFixture()), { status: 200 })
    }
    return new Response(null, { status: 404 })
  }))

  renderBoundary()
  expect(await screen.findByRole('heading', { name: '登录 FlowLens' })).toBeInTheDocument()
  expect(screen.queryByText('受保护页面')).not.toBeInTheDocument()

  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'correct horse battery staple' } })
  fireEvent.click(screen.getByRole('button', { name: '登录' }))

  expect(await screen.findByText('受保护页面')).toBeInTheDocument()
  expect(localStorage.length).toBe(0)
  expect(sessionStorage.length).toBe(0)
})

it('shows retry timing after login is rate limited', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (String(input).endsWith('/api/v1/session')) return new Response('{}', { status: 401 })
    return new Response(JSON.stringify({ error: { code: 'rate_limited', message: '登录尝试过多' } }), { status: 429, headers: { 'Retry-After': '120' } })
  }))
  renderBoundary()
  await screen.findByRole('heading', { name: '登录 FlowLens' })
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'wrong password value' } })
  fireEvent.click(screen.getByRole('button', { name: '登录' }))
  expect(await screen.findByText('请在 2 分钟后重试')).toBeInTheDocument()
})

it('returns to login when an API reports an expired session', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(sessionFixture()), { status: 200 })))
  renderBoundary()
  expect(await screen.findByText('受保护页面')).toBeInTheDocument()
  window.dispatchEvent(new CustomEvent('flowlens:unauthorized'))
  await waitFor(() => expect(screen.getByRole('heading', { name: '登录 FlowLens' })).toBeInTheDocument())
  expect(screen.queryByText('受保护页面')).not.toBeInTheDocument()
})

function renderBoundary() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><SessionBoundary><div>受保护页面</div></SessionBoundary></QueryClientProvider>)
}

function sessionFixture() {
  return { authenticated: true, username: 'admin', csrf_token: 'csrf-value' }
}

