import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'

import { OverviewPage } from './OverviewPage'

const fixture = {
  node_id: 'flowlens-node-1',
  range: '24h',
  inbound_bytes: 128_600_000_000,
  outbound_bytes: 342_100_000_000,
  active_connections: 186,
  domain_coverage: 87.3,
  data_fresh_at: new Date(Date.now() - 20_000).toISOString(),
  series: [
    { at: '2026-07-14T11:00:00Z', inbound_bytes: 60_000_000, outbound_bytes: 90_000_000, inbound_bps: 2_000_000, outbound_bps: 3_000_000 },
    { at: '2026-07-14T11:30:00Z', inbound_bytes: 90_000_000, outbound_bytes: 120_000_000, inbound_bps: 3_000_000, outbound_bps: 4_000_000 },
  ],
}

afterEach(() => {
	cleanup()
  vi.unstubAllGlobals()
})

describe('OverviewPage', () => {
  it('renders real summary data, chart labels, and traffic rankings', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      const body = url.includes('/domains') ? trafficResponse(url.includes('direction=inbound')
        ? [{ domain: 'app.example.com', bytes: 4_800_000, connections: 0, requests: 18, owner_count: 1, direction: 'inbound', confidence: 'confirmed' }]
        : [{ domain: 'api.example.com', bytes: 9_800_000, connections: 21, requests: 0, owner_count: 1, direction: 'outbound', confidence: 'confirmed' }])
        : url.includes('/owners') ? trafficResponse([{ id: 'container:proxy', kind: 'container', name: 'edge-proxy', bytes: 7_200_000, inbound_bytes: 2_000_000, outbound_bytes: 5_200_000, connections: 14 }])
          : url.includes('/flows') ? trafficResponse([{ direction: 'outbound', owner_id: 'container:proxy', owner_name: 'edge-proxy', source: '10.0.0.2:43120', destination: '1.1.1.1:443', domain: 'cloudflare.com', confidence: 'confirmed', protocol: 'tcp', remote_port: 443, country_code: 'US', country_name: 'United States', asn: 13335, organization: 'Cloudflare', network_classification: 'public', bytes: 6_400_000, connections: 8 }])
            : url.includes('/alerts') ? { items: [{ id: 7, rule_id: 'new-destination', status: 'open', severity: 'warning', node_id: 'flowlens-node-1', owner_id: 'container:proxy', title: '发现新的远程目标', evidence: {}, traffic_filter: {}, observed_value: 1, comparison_value: null, window_seconds: 300, first_seen_at: new Date().toISOString(), last_seen_at: new Date().toISOString(), resolved_at: null, occurrence_count: 1 }] }
            : fixture
      return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }))

    renderOverview()

    expect(await screen.findByText('128.6 GB')).toBeInTheDocument()
    expect(screen.getByText('342.1 GB')).toBeInTheDocument()
    expect(screen.getByText('186')).toBeInTheDocument()
    expect(screen.getByText('87.3%')).toBeInTheDocument()
    expect(await screen.findByRole('img', { name: '入站和出站流量趋势' })).toBeInTheDocument()
    expect(screen.getByText('数据延迟')).toBeInTheDocument()
    expect(await screen.findByText('api.example.com')).toBeInTheDocument()
    expect(await screen.findByText('app.example.com')).toBeInTheDocument()
    expect(screen.getByText('出站域名排行')).toBeInTheDocument()
    expect(screen.getByText('入站域名排行')).toBeInTheDocument()
    expect(screen.getByText('最近异常')).toBeInTheDocument()
    expect(screen.getByText('发现新的远程目标')).toBeInTheDocument()
    expect(screen.getAllByText('edge-proxy')).toHaveLength(2)
    expect(screen.getByText('cloudflare.com')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: '查看全部' })).toHaveLength(5)
  })

  it('does not show a delay warning for fresh data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...fixture, data_fresh_at: new Date().toISOString() }), { status: 200 })))

    renderOverview()

    expect(await screen.findByText('128.6 GB')).toBeInTheDocument()
    expect(screen.queryByText('数据延迟')).not.toBeInTheDocument()
  })
})

function renderOverview() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter><OverviewPage nodeID="flowlens-node-1" /></MemoryRouter>
    </QueryClientProvider>,
  )
}

function trafficResponse(items: unknown[]) {
  return { items, data_fresh_at: new Date().toISOString(), partial_data: [] }
}
