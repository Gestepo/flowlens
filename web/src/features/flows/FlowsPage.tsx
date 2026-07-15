import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { ArrowRight } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

import { DataState } from '../../components/DataState'
import { FilterBar } from '../../components/FilterBar'
import { PageAdvance } from '../../components/PageAdvance'
import { confidenceLabel, directionLabel, fetchTraffic, formatBytes, type FlowItem } from '../traffic/api'

export function FlowsPage({ nodeID }: { nodeID: string }) {
  const [search] = useSearchParams()
  const query = useQuery({ queryKey: ['flows', nodeID, search.toString()], queryFn: () => fetchTraffic<FlowItem>('flows', nodeID, search), placeholderData: keepPreviousData })
  return <div className="detail-page">
    <div className="page-heading"><div><h1>连接走向</h1><p>精确的所有者、源、目标和网络归属</p></div></div>
    <FilterBar searchLabel="筛选域名或 IP" />
    <DataState pending={query.isPending} error={query.isError} retry={() => void query.refetch()} freshAt={query.data?.data_fresh_at} gaps={query.data?.partial_data} empty={!query.data?.items.length}>
      <section className="panel data-panel"><div className="data-header flow-grid"><span>所有者 / 方向</span><span>源 → 目标</span><span>域名</span><span>网络</span><span>流量</span></div>
        {query.data?.items.map((item, index) => <div className="data-row flow-grid" key={`${item.owner_id}:${item.destination}:${item.remote_port}:${index}`}>
          <span data-label="所有者 / 方向"><b>{item.owner_name}</b><small>{directionLabel(item.direction)}</small></span>
          <span className="endpoint-cell" data-label="源 → 目标"><code>{item.source}</code><ArrowRight size={13} /><code>{formatEndpoint(item.destination, item.remote_port)}</code></span>
          <span data-label="域名"><b>{item.domain}</b><i className={`confidence ${item.confidence}`}>{confidenceLabel(item.confidence)}</i></span>
          <span data-label="网络"><b>{item.country_code || item.network_classification}</b><small>{item.asn ? `AS${item.asn}` : item.organization || '未知网络'}</small></span>
          <span data-label="流量"><b>{formatBytes(item.bytes)}</b><small>{item.protocol.toUpperCase()} · {item.requests > 0 ? `${item.requests} 次请求` : `${item.connections} 条连接`}</small></span>
        </div>)}
      </section>
      <PageAdvance cursor={query.data?.next_cursor} />
      <p className="attribution">IP Geolocation by <a href="https://db-ip.com" target="_blank" rel="noreferrer">DB-IP</a></p>
    </DataState>
  </div>
}

function formatEndpoint(destination: string, port: number) {
  if (port <= 0) return destination
  return destination.includes(':') && !destination.startsWith('[') ? `[${destination}]:${port}` : `${destination}:${port}`
}
