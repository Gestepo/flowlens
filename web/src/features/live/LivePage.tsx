import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Activity, ArrowDownToLine, ArrowRight, ArrowUpFromLine, Gauge } from 'lucide-react'
import { useRef } from 'react'
import { useSearchParams } from 'react-router-dom'

import { DataState } from '../../components/DataState'
import { FilterBar } from '../../components/FilterBar'
import { PageAdvance } from '../../components/PageAdvance'
import { confidenceLabel, directionLabel, fetchTraffic, formatBytes, type LiveItem, type LiveMetrics } from '../traffic/api'

export function LivePage({ nodeID }: { nodeID: string }) {
  const [search] = useSearchParams()
  const query = useQuery({ queryKey: ['live', nodeID, search.toString()], queryFn: () => fetchTraffic<LiveItem>('live', nodeID, search), refetchInterval: 5_000, placeholderData: keepPreviousData })
  return <div className="detail-page">
    <div className="page-heading"><div><h1>实时流量</h1><p>最后一个采集窗口中的进程、端点与字节增量</p></div><span className="live-indicator"><i />5 秒刷新</span></div>
    <FilterBar searchLabel="筛选域名或 IP" />
    <DataState pending={query.isPending} error={query.isError} retry={() => void query.refetch()} freshAt={query.data?.data_fresh_at} gaps={query.data?.partial_data} empty={!query.data?.items.length}>
      {query.data?.metrics && <LiveMetricStrip metrics={query.data.metrics} />}
      <LiveTable items={query.data?.items ?? []} />
      <PageAdvance cursor={query.data?.next_cursor} />
    </DataState>
  </div>
}

function LiveMetricStrip({ metrics }: { metrics: LiveMetrics }) {
  return <section className="metric-strip live-metrics" aria-label="实时速率指标">
    <LiveMetric icon={<ArrowDownToLine size={15} />} label="当前入站" value={`${formatBytes(metrics.current_inbound_bps)}/s`} tone="inbound" />
    <LiveMetric icon={<ArrowUpFromLine size={15} />} label="当前出站" value={`${formatBytes(metrics.current_outbound_bps)}/s`} tone="outbound" />
    <LiveMetric icon={<Gauge size={15} />} label="峰值入站" value={`${formatBytes(metrics.peak_inbound_bps)}/s`} />
    <LiveMetric icon={<Gauge size={15} />} label="峰值出站" value={`${formatBytes(metrics.peak_outbound_bps)}/s`} />
    <LiveMetric icon={<Activity size={15} />} label="活跃连接" value={String(metrics.active_connections)} />
  </section>
}

function LiveMetric({ icon, label, value, tone = '' }: { icon: React.ReactNode; label: string; value: string; tone?: string }) {
  return <div className={`metric ${tone}`}><div className="metric-label">{icon}<span>{label}</span></div><strong>{value}</strong></div>
}

function LiveTable({ items }: { items: LiveItem[] }) {
  const viewport = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => viewport.current,
    getItemKey: (index) => items[index]?.id ?? index,
    estimateSize: () => 62,
    measureElement: (element) => element.getBoundingClientRect().height || 62,
    overscan: 6,
    initialRect: { width: 1_000, height: 520 },
    observeElementRect: (instance, callback) => {
      const element = instance.scrollElement
      if (!element) return
      const update = () => {
        const rect = element.getBoundingClientRect()
        callback({ width: rect.width || 1_000, height: rect.height || 520 })
      }
      update()
      if (typeof ResizeObserver === 'undefined') return
      const observer = new ResizeObserver(update)
      observer.observe(element)
      return () => observer.disconnect()
    },
  })
  return <section className="panel data-panel live-table">
    <div className="data-header live-grid"><span>时间 / 方向</span><span>所有者</span><span>连接</span><span>域名证据</span><span>流量</span></div>
    <div ref={viewport} className="live-viewport" role="region" aria-label="实时连接列表" tabIndex={0}>
      <div className="live-virtual-space" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((virtualRow) => {
          const item = items[virtualRow.index]
          return <div className="data-row live-grid live-virtual-row" data-index={virtualRow.index} data-live-row key={item.id} ref={virtualizer.measureElement} style={{ transform: `translateY(${virtualRow.start}px)` }}>
            <span data-label="时间 / 方向"><b>{new Date(item.observed_at).toLocaleTimeString('zh-CN', { hour12: false })}</b><small>{directionLabel(item.direction)} · {item.protocol.toUpperCase()}</small></span>
            <strong data-label="所有者">{item.owner_name}</strong>
            <span className="endpoint-cell" data-label="连接"><code>{item.source}</code><ArrowRight size={13} /><code>{item.destination}</code></span>
            <span data-label="域名证据"><b>{item.display_name}</b><i className={`confidence ${item.confidence}`}>{confidenceLabel(item.confidence)}</i></span>
            <span data-label="流量"><b>↑ {formatBytes(item.bytes_sent)}</b><small>↓ {formatBytes(item.bytes_received)}</small></span>
          </div>
        })}
      </div>
    </div>
  </section>
}
