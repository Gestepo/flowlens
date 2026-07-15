import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const fixedNow = '2026-01-15T10:24:36.000Z'
const nodeID = 'demo-node'

test('renders a privacy-safe public overview concept', async ({ page }) => {
  await page.clock.setFixedTime(new Date(fixedNow))
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const json = syntheticResponse(url)
    await route.fulfill({ json })
  })

  await page.goto('/')
  await expect(page.getByRole('heading', { name: '流量概览' })).toBeVisible()
  await expect(page.getByRole('img', { name: '入站和出站流量趋势' }).locator('.recharts-line-curve')).toHaveCount(2)
  await expect(page.getByText('api.example.com').first()).toBeVisible()

  await page.evaluate(() => {
    const callout = document.createElement('aside')
    callout.setAttribute('aria-label', '归属细节概念标注')
    callout.innerHTML = '<span>归属细节</span><strong>api.example.com</strong><small>web-api · TCP 443 · 18.6 GB</small>'
    Object.assign(callout.style, {
      position: 'fixed', right: '32px', bottom: '26px', zIndex: '20', width: '270px',
      boxSizing: 'border-box', padding: '13px 16px', borderLeft: '4px solid #42c997',
      background: '#19313b', color: '#ffffff', boxShadow: '0 10px 28px rgba(16,36,45,.32)',
      fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif',
    })
    const span = callout.querySelector('span') as HTMLElement
    const strong = callout.querySelector('strong') as HTMLElement
    const small = callout.querySelector('small') as HTMLElement
    Object.assign(span.style, { display: 'block', color: '#8fdcbc', fontSize: '11px', marginBottom: '3px' })
    Object.assign(strong.style, { display: 'block', fontSize: '15px', marginBottom: '4px' })
    Object.assign(small.style, { display: 'block', color: '#c5d4db', fontSize: '12px' })
    document.body.append(callout)
  })

  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0)
  const output = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../docs/images/flowlens-overview-concept.png')
  await mkdir(path.dirname(output), { recursive: true })
  await page.screenshot({ path: output, animations: 'disabled' })
})

function syntheticResponse(url: URL): unknown {
  if (url.pathname === '/api/v1/session') return { authenticated: true, username: 'demo', csrf_token: 'synthetic-csrf-token' }
  if (url.pathname === '/api/v1/nodes') return { items: [{ id: nodeID, name: '演示节点', status: 'healthy', last_seen_at: fixedNow, failed_collectors: [] }] }
  if (url.pathname === '/api/v1/overview') return overview()
  if (url.pathname === '/api/v1/domains') return traffic(url.searchParams.get('direction') === 'inbound' ? inboundDomains : outboundDomains)
  if (url.pathname === '/api/v1/owners') return traffic(owners)
  if (url.pathname === '/api/v1/flows') return traffic(flows)
  if (url.pathname === '/api/v1/alerts') return { items: alerts }
  return { items: [], data_fresh_at: fixedNow, partial_data: [] }
}

function overview() {
  const start = Date.parse('2026-01-14T11:00:00.000Z')
  const inbound = [18, 22, 21, 27, 24, 31, 38, 34, 42, 47, 41, 52, 58, 54, 63, 71, 66, 74, 69, 78, 84, 77, 88, 82]
  const outbound = [9, 12, 11, 15, 14, 18, 21, 19, 24, 26, 23, 29, 31, 28, 34, 38, 35, 40, 37, 43, 46, 42, 49, 45]
  return {
    node_id: nodeID, range: '24h', inbound_bytes: 42_800_000_000, outbound_bytes: 18_600_000_000,
    active_connections: 1284, domain_coverage: 94.7, data_fresh_at: fixedNow,
    series: inbound.map((value, index) => ({
      at: new Date(start + index * 3_600_000).toISOString(), inbound_bytes: value * 100_000_000,
      outbound_bytes: outbound[index] * 100_000_000, inbound_bps: value * 1_000_000, outbound_bps: outbound[index] * 1_000_000,
    })),
  }
}

function traffic(items: unknown[]) { return { items, data_fresh_at: fixedNow, partial_data: [] } }

const outboundDomains = [
  { domain: 'api.example.com', direction: 'outbound', confidence: 'confirmed', bytes: 18_600_000_000, connections: 842, requests: 0, owner_count: 1 },
  { domain: 'cdn.example.net', direction: 'outbound', confidence: 'confirmed', bytes: 12_400_000_000, connections: 516, requests: 0, owner_count: 2 },
  { domain: 'updates.example.com', direction: 'outbound', confidence: 'inferred', bytes: 7_900_000_000, connections: 184, requests: 0, owner_count: 1 },
]

const inboundDomains = [
  { domain: 'dashboard.example.com', direction: 'inbound', confidence: 'confirmed', bytes: 9_200_000_000, connections: 0, requests: 12640, owner_count: 1 },
  { domain: 'files.example.net', direction: 'inbound', confidence: 'confirmed', bytes: 4_800_000_000, connections: 0, requests: 3720, owner_count: 1 },
]

const owners = [
  { id: 'process:web-api', kind: 'process', name: 'web-api', bytes: 14_200_000_000, inbound_bytes: 3_100_000_000, outbound_bytes: 11_100_000_000, connections: 842, ports: [8080] },
  { id: 'container:postgres', kind: 'container', name: 'postgres', bytes: 9_800_000_000, inbound_bytes: 5_300_000_000, outbound_bytes: 4_500_000_000, connections: 316, ports: [5432] },
  { id: 'process:backup-worker', kind: 'process', name: 'backup-worker', bytes: 5_300_000_000, inbound_bytes: 400_000_000, outbound_bytes: 4_900_000_000, connections: 92, ports: [] },
]

const flows = [
  { direction: 'outbound', owner_id: 'process:web-api', owner_name: 'web-api', source: '192.0.2.10:48124', destination: '203.0.113.10:443', domain: 'api.example.com', confidence: 'confirmed', protocol: 'tcp', remote_port: 443, country_code: 'EX', country_name: 'Example', asn: 64500, organization: 'Example Network', network_classification: 'public', bytes: 18_600_000_000, connections: 842, requests: 0 },
  { direction: 'outbound', owner_id: 'process:backup-worker', owner_name: 'backup-worker', source: '192.0.2.10:49220', destination: '203.0.113.20:443', domain: 'backup.example.net', confidence: 'confirmed', protocol: 'tcp', remote_port: 443, country_code: 'EX', country_name: 'Example', asn: 64501, organization: 'Example Backup Network', network_classification: 'public', bytes: 5_300_000_000, connections: 92, requests: 0 },
]

const alerts = [
  { id: 1, rule_id: 'rate', status: 'open', severity: 'warning', node_id: nodeID, owner_id: 'process:web-api', title: '出站速率高于基线', evidence: {}, traffic_filter: {}, observed_value: 88, comparison_value: 62, window_seconds: 300, first_seen_at: fixedNow, last_seen_at: fixedNow, resolved_at: null, occurrence_count: 3 },
]
