import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { OwnerDetailPage } from './OwnerDetailPage'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('shows owner trend, active connections, ports, destinations, and anomalies', async () => {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/owners/container%3Aweb')) return response({
      id: 'container:web', kind: 'container', name: 'web', bytes: 300, inbound_bytes: 100, outbound_bytes: 200, connections: 1, ports: [8080],
      series: [{ at: '2026-07-15T00:00:00Z', inbound_bytes: 100, outbound_bytes: 200, inbound_bps: 1.7, outbound_bps: 3.3 }],
      active_connections: [{ id: 'c1', observed_at: new Date().toISOString(), direction: 'outbound', owner_id: 'container:web', owner_name: 'web', source: '10.0.0.2:42000', destination: '203.0.113.10:443', display_name: 'api.example.test', confidence: 'confirmed', protocol: 'tcp', state: 'established', bytes_sent: 100, bytes_received: 200 }],
      data_fresh_at: new Date().toISOString(), partial_data: [],
    })
    if (url.includes('/flows')) return response({ items: [{ direction: 'outbound', owner_id: 'container:web', owner_name: 'web', source: '10.0.0.2', destination: '203.0.113.10', domain: 'api.example.test', confidence: 'confirmed', protocol: 'tcp', remote_port: 443, bytes: 300, connections: 1 }], data_fresh_at: new Date().toISOString(), partial_data: [] })
    if (url.includes('/alerts')) return response({ items: [{ id: 7, owner_id: 'container:web', title: '发现新的远程目标', occurrence_count: 1 }] })
    return response({})
  }))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/owners/container%3Aweb?range=24h']}><Routes><Route path="/owners/:id" element={<OwnerDetailPage nodeID="flowlens-node-1" />} /></Routes></MemoryRouter></QueryClientProvider>)

  expect(await screen.findByRole('img', { name: '入站和出站流量趋势' })).toBeInTheDocument()
  expect(screen.getByText('当前活跃连接')).toBeInTheDocument()
  expect(screen.getAllByText('api.example.test').length).toBeGreaterThanOrEqual(2)
  expect(screen.getByText('8080')).toBeInTheDocument()
  expect(screen.getByText('发现新的远程目标')).toBeInTheDocument()
})

function response(body: unknown) { return Promise.resolve(new Response(JSON.stringify(body), { status: 200 })) }
