import { afterEach, expect, it, vi } from 'vitest'

import { fetchOverview } from './api'

afterEach(() => vi.unstubAllGlobals())

it('rejects a malformed numeric API field', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
    node_id: 'node',
    range: '24h',
    inbound_bytes: 'many',
    outbound_bytes: 0,
    active_connections: 0,
    domain_coverage: null,
    series: [],
    data_fresh_at: '2026-07-14T12:00:00Z',
  }), { status: 200 })))

  await expect(fetchOverview('node', '24h')).rejects.toThrow('inbound_bytes')
})
