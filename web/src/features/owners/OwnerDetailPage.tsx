import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Activity, AlertTriangle, ArrowLeft, ArrowRight, ExternalLink, Network, RadioTower } from 'lucide-react'
import { lazy, Suspense } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { DataState } from '../../components/DataState'
import { getAlerts } from '../operations/api'
import { confidenceLabel, fetchOwnerDetail, fetchTraffic, formatBytes, type FlowItem } from '../traffic/api'

const TrafficChart = lazy(() => import('../overview/TrafficChart').then((module) => ({ default: module.TrafficChart })))

export function OwnerDetailPage({ nodeID }: { nodeID: string }) {
  const { id = '' } = useParams()
  const [search] = useSearchParams()
  const querySearch = new URLSearchParams(search); querySearch.set('owner', id)
  const query = useQuery({ queryKey: ['owner', nodeID, id, search.toString()], queryFn: () => fetchOwnerDetail(nodeID, id, search), placeholderData: keepPreviousData })
  const flows = useQuery({ queryKey: ['owner-flows', nodeID, id, search.toString()], queryFn: () => fetchTraffic<FlowItem>('flows', nodeID, querySearch), placeholderData: keepPreviousData })
  const alerts = useQuery({ queryKey: ['owner-alerts', id], queryFn: () => getAlerts('open') })
  const item = query.data
  return <div className="detail-page">
    <div className="page-heading"><div><Link className="back-link" to={`/owners?${search.toString()}`}><ArrowLeft size={14} />返回</Link><h1>{item?.name ?? '所有者详情'}</h1><p>{id}</p></div></div>
    <DataState pending={query.isPending} error={query.isError} retry={() => void query.refetch()} freshAt={query.data?.data_fresh_at} gaps={query.data?.partial_data} empty={!item}>
      {item && <section className="metric-strip owner-metrics"><Metric label="总流量" value={formatBytes(item.bytes)} /><Metric label="入站" value={formatBytes(item.inbound_bytes)} /><Metric label="出站" value={formatBytes(item.outbound_bytes)} /><Metric label="连接" value={String(item.connections)} /></section>}
      {item && <section className="panel trend-panel owner-trend"><div className="panel-heading"><h2>流量趋势</h2><span>按分钟</span></div><Suspense fallback={<div className="chart-loading">正在加载图表</div>}><TrafficChart points={item.series} /></Suspense></section>}
      {item && <section className="operation-section owner-detail-section"><div className="section-heading"><h2><Activity size={16} />当前活跃连接</h2><span>{item.active_connections.length} 条</span></div><div className="active-connection-list">{item.active_connections.map((connection) => <Link key={connection.id} to={`/flows?owner=${encodeURIComponent(id)}&ip=${encodeURIComponent(connection.destination.split(':')[0])}`}><span><strong>{connection.display_name}</strong><small>{connection.protocol.toUpperCase()} · {connection.state}</small></span><code>{connection.source}</code><ArrowRight size={13} /><code>{connection.destination}</code></Link>)}{item.active_connections.length === 0 && <span>当前没有活跃连接</span>}</div></section>}
      {item && <section className="operation-section owner-detail-section"><div className="section-heading"><h2><RadioTower size={16} />监听端口</h2></div><div className="port-list">{item.ports.length ? item.ports.map((port) => <code key={port}>{port}</code>) : <span>未发现监听端口</span>}</div></section>}
      <section className="operation-section owner-detail-section"><div className="section-heading"><h2><Network size={16} />主要目的地</h2></div><div className="destination-list">{flows.data?.items.slice(0, 8).map((flow) => <Link key={`${flow.destination}:${flow.remote_port}:${flow.domain}`} to={`/flows?owner=${encodeURIComponent(id)}&${flow.domain ? `domain=${encodeURIComponent(flow.domain)}` : `ip=${encodeURIComponent(flow.destination)}`}`}><span><strong>{flow.domain || flow.destination}</strong><small>{flow.destination}:{flow.remote_port} · {flow.protocol.toUpperCase()}</small></span><b>{formatBytes(flow.bytes)}</b><ExternalLink size={13} /></Link>)}</div></section>
      <section className="operation-section owner-detail-section"><div className="section-heading"><h2><AlertTriangle size={16} />近期异常</h2></div><div className="owner-alerts">{alerts.data?.filter((alert) => alert.owner_id === id).map((alert) => <Link key={alert.id} to={`/alerts/${alert.id}`}><span>{alert.title}</span><small>{alert.occurrence_count} 次</small></Link>)}{!alerts.data?.some((alert) => alert.owner_id === id) && <span>当前没有关联告警</span>}</div></section>
    </DataState>
  </div>
}

function Metric({ label, value }: { label: string; value: string }) { return <div className="metric"><div className="metric-label">{label}</div><strong>{value}</strong></div> }
