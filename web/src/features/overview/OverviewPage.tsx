import { useQuery } from '@tanstack/react-query'
import { Activity, AlertTriangle, ArrowDownToLine, ArrowUpFromLine, ScanSearch } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { Link } from 'react-router-dom'

import { fetchTraffic, formatBytes, type DomainItem, type FlowItem, type OwnerItem } from '../traffic/api'
import { getAlerts } from '../operations/api'
import { fetchOverview, type OverviewRange } from './api'

const TrafficChart = lazy(() => import('./TrafficChart').then((module) => ({ default: module.TrafficChart })))

interface OverviewPageProps {
  nodeID: string
}

const ranges: Array<{ value: OverviewRange; label: string }> = [
  { value: '1h', label: '1 小时' },
  { value: '24h', label: '24 小时' },
  { value: '7d', label: '7 天' },
  { value: '30d', label: '30 天' },
]

export function OverviewPage({ nodeID }: OverviewPageProps) {
  const [range, setRange] = useState<OverviewRange>('24h')
  const query = useQuery({
    queryKey: ['overview', nodeID, range],
    queryFn: () => fetchOverview(nodeID, range),
    refetchInterval: 5_000,
  })
  const rankingSearch = new URLSearchParams({ range, limit: '5', sort: 'bytes' })
  const outboundDomains = useQuery({ queryKey: ['overview-domains', nodeID, range, 'outbound'], queryFn: () => fetchTraffic<DomainItem>('domains', nodeID, rankingSearch, 'outbound') })
  const inboundDomains = useQuery({ queryKey: ['overview-domains', nodeID, range, 'inbound'], queryFn: () => fetchTraffic<DomainItem>('domains', nodeID, rankingSearch, 'inbound') })
  const owners = useQuery({ queryKey: ['overview-owners', nodeID, range], queryFn: () => fetchTraffic<OwnerItem>('owners', nodeID, rankingSearch) })
  const flows = useQuery({ queryKey: ['overview-flows', nodeID, range], queryFn: () => fetchTraffic<FlowItem>('flows', nodeID, rankingSearch) })
  const alerts = useQuery({ queryKey: ['overview-alerts', nodeID], queryFn: () => getAlerts('open') })

  if (query.isPending) return <div className="page-state">正在加载</div>
  if (query.isError || !query.data) return <div className="page-state error-state">概览数据加载失败</div>

  const data = query.data
  const stale = Date.now() - Date.parse(data.data_fresh_at) > 15_000

  return (
    <div className="overview-page">
      <div className="page-heading">
        <div><h1>流量概览</h1><p>最后更新 {formatTime(data.data_fresh_at)}</p></div>
        <div className="range-control" aria-label="时间范围">
          {ranges.map((item) => (
            <button key={item.value} type="button" className={range === item.value ? 'active' : ''} onClick={() => setRange(item.value)}>{item.label}</button>
          ))}
        </div>
      </div>

      {stale && <div className="status-banner warning" role="status"><AlertTriangle size={17} /><strong>数据延迟</strong><span>采集数据尚未更新</span></div>}

      <section className="metric-strip" aria-label="流量指标">
        <Metric icon={<ArrowDownToLine size={17} />} label="入站流量" value={formatBytes(data.inbound_bytes)} tone="inbound" />
        <Metric icon={<ArrowUpFromLine size={17} />} label="出站流量" value={formatBytes(data.outbound_bytes)} tone="outbound" />
        <Metric icon={<Activity size={17} />} label="活跃连接" value={String(data.active_connections)} />
        <Metric icon={<ScanSearch size={17} />} label="域名识别率" value={data.domain_coverage === null ? '不可用' : `${data.domain_coverage.toFixed(1)}%`} />
      </section>

      <section className="panel trend-panel">
        <div className="panel-heading"><h2>流量趋势</h2><span>按速率</span></div>
        <Suspense fallback={<div className="chart-loading">正在加载图表</div>}>
          <TrafficChart points={data.series} />
        </Suspense>
      </section>

      <section className="ranking-grid">
        <RankingPanel title="出站域名排行" href={`/domains?range=${range}&direction=outbound`} className="domain-ranking">
          {outboundDomains.data?.items.slice(0, 5).map((item) => <RankingRow key={`${item.domain}:${item.confidence}`} href={`/domains?range=${range}&direction=outbound&domain=${encodeURIComponent(item.domain)}&confidence=${item.confidence}`} primary={item.domain} secondary={`${item.connections} 条连接`} value={formatBytes(item.bytes)} />)}
        </RankingPanel>
        <RankingPanel title="入站域名排行" href={`/domains?range=${range}&direction=inbound`} className="domain-ranking">
          {inboundDomains.data?.items.slice(0, 5).map((item) => <RankingRow key={`${item.domain}:${item.confidence}`} href={`/domains?range=${range}&direction=inbound&domain=${encodeURIComponent(item.domain)}&confidence=${item.confidence}`} primary={item.domain} secondary={`${item.requests} 次请求`} value={formatBytes(item.bytes)} />)}
        </RankingPanel>
        <RankingPanel title="容器与进程排行" href={`/owners?range=${range}`}>
          {owners.data?.items.slice(0, 5).map((item) => <RankingRow key={item.id} href={`/owners/${encodeURIComponent(item.id)}?range=${range}`} primary={item.name} secondary={ownerKind(item.kind)} value={formatBytes(item.bytes)} />)}
        </RankingPanel>
        <RankingPanel title="主要连接走向" href={`/flows?range=${range}`}>
          {flows.data?.items.slice(0, 5).map((item) => <RankingRow key={`${item.owner_id}:${item.destination}:${item.domain}`} href={`/flows?range=${range}&owner=${encodeURIComponent(item.owner_id)}&${item.domain ? `domain=${encodeURIComponent(item.domain)}` : `ip=${encodeURIComponent(item.destination)}`}`} primary={item.domain || item.destination} secondary={item.owner_name || item.source} value={formatBytes(item.bytes)} />)}
        </RankingPanel>
        <RankingPanel title="最近异常" href="/alerts?status=open">
          {alerts.data?.filter((alert) => alert.node_id === nodeID).slice(0, 5).map((alert) => <RankingRow key={alert.id} href={`/alerts/${alert.id}`} primary={alert.title} secondary={`${alert.occurrence_count} 次 · ${severityLabel(alert.severity)}`} value={formatAlertTime(alert.last_seen_at)} />)}
        </RankingPanel>
      </section>
    </div>
  )
}

function Metric({ icon, label, value, tone = '' }: { icon: React.ReactNode; label: string; value: string; tone?: string }) {
  return <div className={`metric ${tone}`}><div className="metric-label">{icon}<span>{label}</span></div><strong>{value}</strong></div>
}

function RankingPanel({ title, href, children, className = '' }: { title: string; href: string; children?: React.ReactNode; className?: string }) {
  return <section className={`panel ranking-panel ${className}`}><div className="panel-heading"><h2>{title}</h2><Link to={href}>查看全部</Link></div><div className="ranking-list">{children || <div className="empty-state">暂无数据</div>}</div></section>
}

function RankingRow({ primary, secondary, value, href }: { primary: string; secondary: string; value: string; href: string }) {
  return <Link className="ranking-row" to={href}><div><strong title={primary}>{primary}</strong><span>{secondary}</span></div><b>{value}</b></Link>
}

function ownerKind(kind: OwnerItem['kind']): string {
  return kind === 'container' ? '容器' : kind === 'process' ? '进程' : '主机'
}

function severityLabel(value: string): string { return value === 'critical' ? '严重' : value === 'warning' ? '警告' : '提示' }
function formatAlertTime(value: string): string { return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }

function formatTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value))
}
