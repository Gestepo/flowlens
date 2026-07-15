import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { DomainsPage } from './DomainsPage'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('keeps inbound and outbound domain rankings isolated through URL filters', async () => {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    const inbound = url.includes('direction=inbound')
    return Promise.resolve(new Response(JSON.stringify({
      items: inbound
        ? [{ domain: 'app.example.test', direction: 'inbound', confidence: 'confirmed', bytes: 512, connections: 0, requests: 4, owner_count: 0 }]
        : [{ domain: 'api.example.test', direction: 'outbound', confidence: 'confirmed', bytes: 2048, connections: 3, requests: 0, owner_count: 1 }],
      data_fresh_at: new Date().toISOString(), partial_data: [],
    }), { status: 200 }))
  }))

  renderPage()
  expect(await screen.findByText('api.example.test')).toBeInTheDocument()
  expect(screen.queryByText('app.example.test')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: '入站域名' }))
  expect(await screen.findByText('app.example.test')).toBeInTheDocument()
  expect(screen.queryByText('api.example.test')).not.toBeInTheDocument()
})

it('preserves rapid filter updates in the URL', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], data_fresh_at: new Date().toISOString(), partial_data: [] }), { status: 200 })))
  renderPage()
  await screen.findByText('当前筛选条件下没有数据')

  act(() => {
    fireEvent.click(screen.getByRole('button', { name: '30 天' }))
    fireEvent.change(screen.getByRole('textbox', { name: '筛选域名' }), { target: { value: 'example.com' } })
  })

  expect(screen.getByTestId('location')).toHaveTextContent('range=30d')
  expect(screen.getByTestId('location')).toHaveTextContent('domain=example.com')
})

it('opens inbound domain details with owners, sources, and status distribution', async () => {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('detail=1')) return Promise.resolve(new Response(JSON.stringify({
      domain: 'app.example.test', direction: 'inbound', confidence: 'confirmed', bytes: 512, connections: 0, requests: 4, owner_count: 1,
      statuses: [{ status: 200, requests: 3, bytes: 400 }, { status: 502, requests: 1, bytes: 112 }],
      sources: [{ ip: '198.51.100.10', requests: 4, bytes: 512 }],
      owners: [{ id: 'container:web', name: 'web', requests: 4, bytes: 512 }],
    }), { status: 200 }))
    return Promise.resolve(new Response(JSON.stringify({
      items: [{ domain: 'app.example.test', direction: 'inbound', confidence: 'confirmed', bytes: 512, connections: 0, requests: 4, owner_count: 1 }],
      data_fresh_at: new Date().toISOString(), partial_data: [],
    }), { status: 200 }))
  }))
  renderPage('/domains?range=24h&direction=inbound')
  fireEvent.click(await screen.findByRole('button', { name: /app.example.test/ }))
  expect(await screen.findByText('HTTP 状态')).toBeInTheDocument()
  expect(screen.getByText('2xx')).toBeInTheDocument()
  expect(screen.getByText('5xx')).toBeInTheDocument()
  expect(screen.getByText('198.51.100.10')).toBeInTheDocument()
  expect(screen.getByText('web')).toBeInTheDocument()
  expect(screen.getByText('1 个所有者')).toBeInTheDocument()
})

it('shows remote network metadata for an outbound domain', async () => {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    const detail = String(input).includes('detail=1')
    return Promise.resolve(new Response(JSON.stringify(detail ? {
      domain: 'api.example.test', direction: 'outbound', confidence: 'confirmed', bytes: 2048, connections: 3, requests: 0, owner_count: 1,
      statuses: [], sources: [], owners: [{ id: 'container:web', name: 'web', requests: 3, bytes: 2048 }],
      networks: [{ country_code: 'US', country_name: 'United States', asn: 64500, organization: 'Example Network', classification: 'public', connections: 3, bytes: 2048 }],
    } : {
      items: [{ domain: 'api.example.test', direction: 'outbound', confidence: 'confirmed', bytes: 2048, connections: 3, requests: 0, owner_count: 1 }],
      data_fresh_at: new Date().toISOString(), partial_data: [],
    }), { status: 200 }))
  }))
  renderPage()
  fireEvent.click(await screen.findByRole('button', { name: /api.example.test/ }))
  expect(await screen.findByText('远程网络')).toBeInTheDocument()
  expect(screen.getByText('Example Network')).toBeInTheDocument()
  expect(screen.getByText(/US · AS64500 · 3 条/)).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'DB-IP' })).toHaveAttribute('href', 'https://db-ip.com')
})

function renderPage(route = '/domains?range=24h&direction=outbound') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[route]}><DomainsPage nodeID="flowlens-node-1" /><LocationProbe /></MemoryRouter></QueryClientProvider>)
}

function LocationProbe() {
  return <output data-testid="location">{useLocation().search}</output>
}
