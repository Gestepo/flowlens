export type Direction = 'inbound' | 'outbound' | 'internal' | 'container_to_container'
export type Confidence = 'confirmed' | 'inferred' | 'ip_only'

export interface QueryResponse<T> {
  items: T[]
  next_cursor?: string
  data_fresh_at: string
  partial_data: Array<{ collector: string; code: string; at: string; recovered: boolean }>
  metrics?: LiveMetrics
}

export interface LiveMetrics {
  current_inbound_bps: number
  current_outbound_bps: number
  peak_inbound_bps: number
  peak_outbound_bps: number
  active_connections: number
}

export interface DomainItem {
  domain: string
  direction: Direction
  confidence: Confidence
  bytes: number
  connections: number
  requests: number
  owner_count: number
}

export interface DomainDetail extends DomainItem {
  statuses: Array<{ status: number; requests: number; bytes: number }>
  sources: Array<{ ip: string; requests: number; bytes: number }>
  owners: Array<{ id: string; name: string; requests: number; bytes: number }>
  networks: Array<{ country_code: string; country_name: string; asn: number; organization: string; classification: string; connections: number; bytes: number }>
}

export interface LiveItem {
  id: string
  observed_at: string
  direction: Direction
  owner_id: string
  owner_name: string
  source: string
  destination: string
  display_name: string
  confidence: Confidence
  protocol: string
  state: string
  bytes_sent: number
  bytes_received: number
}

export interface OwnerItem {
  id: string
  kind: 'host' | 'process' | 'container'
  name: string
  bytes: number
  inbound_bytes: number
  outbound_bytes: number
  connections: number
  ports: number[]
}

export interface OwnerDetail extends OwnerItem {
  series: Array<{ at: string; inbound_bytes: number; outbound_bytes: number; inbound_bps: number; outbound_bps: number }>
  active_connections: LiveItem[]
  data_fresh_at: string
  partial_data: Array<{ collector: string; code: string; at: string; recovered: boolean }>
}

export interface FlowItem {
  direction: Direction
  owner_id: string
  owner_name: string
  source: string
  destination: string
  domain: string
  confidence: Confidence
  protocol: string
  remote_port: number
  country_code: string
  country_name: string
  asn: number
  organization: string
  network_classification: string
  bytes: number
  connections: number
  requests: number
}

export async function fetchTraffic<T>(path: string, nodeID: string, search: URLSearchParams, forcedDirection?: Direction): Promise<QueryResponse<T>> {
  const query = buildQuery(nodeID, search, forcedDirection)
  const response = await apiFetch(`/api/v1/${path}?${query.toString()}`)
  if (!response.ok) throw new Error(`${path} request failed with status ${response.status}`)
  const value = await response.json() as QueryResponse<T>
  if (!Array.isArray(value.items) || typeof value.data_fresh_at !== 'string' || !Array.isArray(value.partial_data)) throw new Error(`${path} response is invalid`)
  return value
}

export async function fetchDomainDetail(nodeID: string, search: URLSearchParams, item: DomainItem): Promise<DomainDetail> {
  const query = buildQuery(nodeID, search, item.direction)
  query.set('domain', item.domain)
  query.set('confidence', item.confidence)
  query.set('detail', '1')
  const response = await apiFetch(`/api/v1/domains?${query.toString()}`)
  if (!response.ok) throw new Error(`domain detail request failed with status ${response.status}`)
  return response.json() as Promise<DomainDetail>
}

export async function fetchOwnerDetail(nodeID: string, ownerID: string, search: URLSearchParams): Promise<OwnerDetail> {
  const query = buildQuery(nodeID, search)
  query.set('detail', '1')
  const response = await apiFetch(`/api/v1/owners/${encodeURIComponent(ownerID)}?${query.toString()}`)
  if (!response.ok) throw new Error(`owner detail request failed with status ${response.status}`)
  return response.json() as Promise<OwnerDetail>
}

function buildQuery(nodeID: string, search: URLSearchParams, forcedDirection?: Direction): URLSearchParams {
  const output = new URLSearchParams({ node: nodeID })
  const customStart = search.get('start'), customEnd = search.get('end')
  if (customStart && customEnd) { output.set('start', customStart); output.set('end', customEnd) } else {
    const range = search.get('range') ?? '24h'
    const hours = range === '1h' ? 1 : range === '7d' ? 168 : range === '30d' ? 720 : 24
    const end = new Date(); output.set('start', new Date(end.getTime() - hours * 3_600_000).toISOString()); output.set('end', end.toISOString())
  }
  for (const key of ['owner', 'domain', 'confidence', 'ip', 'port', 'protocol', 'cursor', 'limit', 'sort']) {
    const value = search.get(key)
    if (value) output.set(key, value)
  }
  const direction = forcedDirection ?? search.get('direction')
  if (direction) output.set('direction', direction)
  return output
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1_000_000_000) return `${(bytes / 1_000_000_000).toFixed(1)} GB`
  if (bytes >= 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`
  if (bytes >= 1_000) return `${(bytes / 1_000).toFixed(1)} KB`
  return `${bytes} B`
}

export function confidenceLabel(value: Confidence): string {
  return value === 'confirmed' ? '已确认' : value === 'inferred' ? '推断' : '仅 IP'
}

export function directionLabel(value: Direction): string {
  return value === 'inbound' ? '入站' : value === 'outbound' ? '出站' : value === 'container_to_container' ? '容器间' : '内部'
}
import { apiFetch } from '../auth/api'
