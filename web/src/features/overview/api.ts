export type OverviewRange = '1h' | '24h' | '7d' | '30d'

export interface TrafficPoint {
  at: string
  inbound_bytes: number
  outbound_bytes: number
  inbound_bps: number
  outbound_bps: number
}

export interface OverviewData {
  node_id: string
  range: OverviewRange
  inbound_bytes: number
  outbound_bytes: number
  active_connections: number
  domain_coverage: number | null
  series: TrafficPoint[]
  data_fresh_at: string
}

export async function fetchOverview(nodeID: string, range: OverviewRange): Promise<OverviewData> {
  const parameters = new URLSearchParams({ node: nodeID, range })
  const response = await apiFetch(`/api/v1/overview?${parameters.toString()}`)
  if (!response.ok) {
    throw new Error(`overview request failed with status ${response.status}`)
  }
  return parseOverview(await response.json())
}

function parseOverview(value: unknown): OverviewData {
  const object = requireObject(value, 'overview')
  const range = requireString(object.range, 'range')
  if (!['1h', '24h', '7d', '30d'].includes(range)) {
    throw new Error('range is invalid')
  }
  const coverage = object.domain_coverage
  if (coverage !== null && (typeof coverage !== 'number' || !Number.isFinite(coverage))) {
    throw new Error('domain_coverage must be a number or null')
  }
  if (!Array.isArray(object.series)) {
    throw new Error('series must be an array')
  }
  return {
    node_id: requireString(object.node_id, 'node_id'),
    range: range as OverviewRange,
    inbound_bytes: requireNumber(object.inbound_bytes, 'inbound_bytes'),
    outbound_bytes: requireNumber(object.outbound_bytes, 'outbound_bytes'),
    active_connections: requireNumber(object.active_connections, 'active_connections'),
    domain_coverage: coverage,
    series: object.series.map((item, index) => parsePoint(item, index)),
    data_fresh_at: requireDate(object.data_fresh_at, 'data_fresh_at'),
  }
}

function parsePoint(value: unknown, index: number): TrafficPoint {
  const point = requireObject(value, `series[${index}]`)
  return {
    at: requireDate(point.at, `series[${index}].at`),
    inbound_bytes: requireNumber(point.inbound_bytes, `series[${index}].inbound_bytes`),
    outbound_bytes: requireNumber(point.outbound_bytes, `series[${index}].outbound_bytes`),
    inbound_bps: requireNumber(point.inbound_bps, `series[${index}].inbound_bps`),
    outbound_bps: requireNumber(point.outbound_bps, `series[${index}].outbound_bps`),
  }
}

function requireObject(value: unknown, field: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${field} must be an object`)
  }
  return value as Record<string, unknown>
}

function requireString(value: unknown, field: string): string {
  if (typeof value !== 'string' || value === '') {
    throw new Error(`${field} must be a non-empty string`)
  }
  return value
}

function requireNumber(value: unknown, field: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    throw new Error(`${field} must be a non-negative number`)
  }
  return value
}

function requireDate(value: unknown, field: string): string {
  const date = requireString(value, field)
  if (Number.isNaN(Date.parse(date))) {
    throw new Error(`${field} must be an ISO date`)
  }
  return date
}
import { apiFetch } from '../auth/api'
