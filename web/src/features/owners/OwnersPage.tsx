import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Box, Cpu, Server } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'

import { DataState } from '../../components/DataState'
import { FilterBar } from '../../components/FilterBar'
import { PageAdvance } from '../../components/PageAdvance'
import { fetchTraffic, formatBytes, type OwnerItem } from '../traffic/api'

export function OwnersPage({ nodeID }: { nodeID: string }) {
  const [search] = useSearchParams()
  const query = useQuery({ queryKey: ['owners', nodeID, search.toString()], queryFn: () => fetchTraffic<OwnerItem>('owners', nodeID, search), placeholderData: keepPreviousData })
  const maxBytes = Math.max(1, ...(query.data?.items.map((item) => item.bytes) ?? []))
  return <div className="detail-page">
    <div className="page-heading"><div><h1>容器与进程</h1><p>按所有者汇总入站、出站与连接数量</p></div></div>
    <FilterBar searchLabel="筛选所有者" searchKey="owner" />
    <DataState pending={query.isPending} error={query.isError} retry={() => void query.refetch()} freshAt={query.data?.data_fresh_at} gaps={query.data?.partial_data} empty={!query.data?.items.length}>
      <section className="owner-list">{query.data?.items.map((item) => <Link className="panel owner-row" to={`/owners/${encodeURIComponent(item.id)}?${search.toString()}`} key={item.id}>
        <span className={`owner-icon ${item.kind}`}>{item.kind === 'container' ? <Box size={18} /> : item.kind === 'process' ? <Cpu size={18} /> : <Server size={18} />}</span>
        <span className="owner-name"><strong>{item.name}</strong><small>{item.kind === 'container' ? '容器' : item.kind === 'process' ? '进程' : '宿主机'}</small></span>
        <span className="owner-bar"><i style={{ width: `${Math.max(3, item.bytes / maxBytes * 100)}%` }} /></span>
        <span data-label="入站">↓ {formatBytes(item.inbound_bytes)}</span><span data-label="出站">↑ {formatBytes(item.outbound_bytes)}</span><strong data-label="总流量">{formatBytes(item.bytes)}</strong><span data-label="连接">{item.connections} 条</span>
      </Link>)}</section>
      <PageAdvance cursor={query.data?.next_cursor} />
    </DataState>
  </div>
}
