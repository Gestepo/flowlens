import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { AlertsPage } from './AlertsPage'
import { AlertDetail } from './AlertDetail'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('filters open and resolved alerts and exposes evidence drill-down', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/alerts/7')) return json({ ...alertFixture('open'), traffic_filter: { node: 'node-a', ip: '1.1.1.1' }, deliveries: [{ id: 1, status: 'terminal', attempt: 6, last_error: 'timeout', created_at: '2026-07-15T12:00:00Z' }] })
    return json({ items: [alertFixture(url.includes('resolved') ? 'resolved' : 'open')], limit: 50, offset: 0 })
  }))
  renderAlerts()
  expect(await screen.findByText('流量速率过高')).toBeInTheDocument()
  expect(screen.getByText('警告')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: '已恢复' }))
  expect(await screen.findByText('已恢复')).toBeInTheDocument()
  const alertLinks = await screen.findAllByRole('link', { name: /流量速率过高/ })
  fireEvent.click(alertLinks.find((link) => link.classList.contains('alert-title'))!)
  expect(await screen.findByText('Webhook 投递')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: '查看相关流量' })).toHaveAttribute('href', expect.stringContaining('ip=1.1.1.1'))
  expect(screen.getByText('timeout')).toBeInTheDocument()
})

function renderAlerts() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/alerts']}><Routes><Route path="/alerts" element={<AlertsPage />} /><Route path="/alerts/:id" element={<AlertDetail />} /></Routes></MemoryRouter></QueryClientProvider>)
}
function alertFixture(status: string) { return { id: 7, rule_id: 'rate', status, severity: 'warning', node_id: 'node-a', owner_id: null, title: '流量速率过高', evidence: { node_id: 'node-a' }, traffic_filter: { node: 'node-a' }, observed_value: 120, comparison_value: 100, window_seconds: 300, first_seen_at: '2026-07-15T12:00:00Z', last_seen_at: '2026-07-15T12:05:00Z', resolved_at: status === 'resolved' ? '2026-07-15T12:10:00Z' : null, occurrence_count: 3 } }
function json(value: unknown) { return Promise.resolve(new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' } })) }
