import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { FlowsPage } from '../flows/FlowsPage'
import { LivePage } from '../live/LivePage'
import { OwnersPage } from '../owners/OwnersPage'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('renders exact live connection fields', async () => {
  stub([{ id: 'c1', observed_at: new Date().toISOString(), direction: 'outbound', owner_id: 'container:web', owner_name: 'web', source: '10.0.0.2:42000', destination: '203.0.113.10:443', display_name: 'api.example.test', confidence: 'confirmed', protocol: 'tcp', state: 'established', bytes_sent: 100, bytes_received: 200 }], { current_inbound_bps: 1000, current_outbound_bps: 2000, peak_inbound_bps: 3000, peak_outbound_bps: 4000, active_connections: 12 })
  renderPage(<LivePage nodeID="flowlens-node-1" />, '/live')
  expect(await screen.findByText('api.example.test')).toBeInTheDocument()
  expect(screen.getByText('10.0.0.2:42000')).toBeInTheDocument()
  expect(screen.getByText('web')).toBeInTheDocument()
  expect(screen.getByText('当前入站')).toBeInTheDocument()
  expect(screen.getByText('1.0 KB/s')).toBeInTheDocument()
  expect(screen.getByText('活跃连接')).toBeInTheDocument()
  expect(screen.getByText('12')).toBeInTheDocument()
})

it('virtualizes a thousand live connections within a stable viewport', async () => {
	stub(Array.from({ length: 1000 }, (_, index) => ({
		id: `c${index}`, observed_at: new Date().toISOString(), direction: 'outbound', owner_id: 'container:web', owner_name: 'web',
		source: `10.0.0.2:${42000 + index}`, destination: '203.0.113.10:443', display_name: `api-${index}.example.test`,
		confidence: 'confirmed', protocol: 'tcp', state: 'established', bytes_sent: 100, bytes_received: 200,
	})))
	renderPage(<LivePage nodeID="flowlens-node-1" />, '/live')
	expect(await screen.findByText('api-0.example.test')).toBeInTheDocument()
	const viewport = screen.getByRole('region', { name: '实时连接列表' })
	expect(viewport).toHaveClass('live-viewport')
	expect(viewport.querySelectorAll('[data-live-row]').length).toBeGreaterThan(0)
	expect(viewport.querySelectorAll('[data-live-row]').length).toBeLessThan(40)
})

it('renders owner rankings', async () => {
  stub([{ id: 'container:web', kind: 'container', name: 'web', bytes: 300, inbound_bytes: 0, outbound_bytes: 300, connections: 1 }])
  renderPage(<OwnersPage nodeID="flowlens-node-1" />, '/owners')
  expect(await screen.findByText('web')).toBeInTheDocument()
  expect(screen.getByText('容器')).toBeInTheDocument()
})

it('renders flow network enrichment and attribution', async () => {
  stub([{ direction: 'outbound', owner_id: 'container:web', owner_name: 'web', source: '10.0.0.2', destination: '203.0.113.10', domain: 'api.example.test', confidence: 'confirmed', protocol: 'tcp', remote_port: 443, country_code: 'US', country_name: 'United States', asn: 64500, organization: 'Example Network', network_classification: 'public', bytes: 300, connections: 1, requests: 0 }])
  renderPage(<FlowsPage nodeID="flowlens-node-1" />, '/flows')
  expect(await screen.findByText('AS64500')).toBeInTheDocument()
  expect(screen.getByText('US')).toBeInTheDocument()
  expect(screen.getByText('api.example.test')).toBeInTheDocument()
  expect(screen.getByText('TCP · 1 条连接')).toBeInTheDocument()
})

it('renders inbound proxy flows as requests without a fictitious port', async () => {
  stub([{ direction: 'inbound', owner_id: 'container:web', owner_name: 'web', source: '198.51.100.10', destination: 'web', domain: 'app.example.test', confidence: 'confirmed', protocol: 'tcp', remote_port: 0, country_code: 'US', country_name: 'United States', asn: 64501, organization: 'Visitor Network', network_classification: 'public', bytes: 512, connections: 0, requests: 4 }])
  renderPage(<FlowsPage nodeID="flowlens-node-1" />, '/flows')
  expect((await screen.findAllByText('web')).length).toBeGreaterThanOrEqual(2)
  expect(screen.queryByText('web:0')).not.toBeInTheDocument()
  expect(screen.getByText('TCP · 4 次请求')).toBeInTheDocument()
})

it('renders an unrecovered collector gap as partial data loss', async () => {
  stub([], undefined, [{ collector: 'npm_logs', code: 'malformed_lines', at: '2026-07-15T06:00:00Z', recovered: false }])
  renderPage(<LivePage nodeID="flowlens-node-1" />, '/live')
  expect(await screen.findByText('部分数据缺失')).toBeInTheDocument()
  expect(screen.getByText('npm_logs')).toBeInTheDocument()
  expect(screen.getByText('部分数据缺失').closest('.status-banner')).toHaveClass('warning')
})

it('renders a recovered collector gap as neutral historical evidence', async () => {
  const at = '2026-07-15T06:00:00Z'
  stub([], undefined, [{ collector: 'npm_logs', code: 'malformed_lines', at, recovered: true }])
  renderPage(<LivePage nodeID="flowlens-node-1" />, '/live')
  const heading = await screen.findByText('历史数据缺口')
  expect(screen.queryByText('部分数据缺失')).not.toBeInTheDocument()
  expect(heading.closest('.status-banner')).toHaveClass('history')
  expect(heading.closest('.status-banner')).not.toHaveClass('warning')
  expect(screen.getByText(new Date(at).toLocaleString('zh-CN', { hour12: false }))).toBeInTheDocument()
})

function stub(items: unknown[], metrics?: unknown, partialData: unknown[] = []) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ items, metrics, data_fresh_at: new Date().toISOString(), partial_data: partialData }), { status: 200 })))
}

function renderPage(page: React.ReactNode, route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`${route}?range=24h`]}>{page}</MemoryRouter></QueryClientProvider>)
}
