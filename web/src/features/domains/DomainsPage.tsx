import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { ArrowDownToLine, ArrowUpFromLine, X } from 'lucide-react'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { DataState } from '../../components/DataState'
import { FilterBar } from '../../components/FilterBar'
import { PageAdvance } from '../../components/PageAdvance'
import { confidenceLabel, fetchDomainDetail, fetchTraffic, formatBytes, type Direction, type DomainDetail, type DomainItem } from '../traffic/api'

export function DomainsPage({ nodeID }: { nodeID: string }) {
  const [search, setSearch] = useSearchParams()
  const [selected, setSelected] = useState<DomainItem | null>(null)
  const direction: Direction = search.get('direction') === 'inbound' ? 'inbound' : 'outbound'
  const query = useQuery({ queryKey: ['domains', nodeID, search.toString(), direction], queryFn: () => fetchTraffic<DomainItem>('domains', nodeID, search, direction), placeholderData: keepPreviousData })
  const detail = useQuery({ queryKey: ['domain-detail', nodeID, search.toString(), selected], queryFn: () => fetchDomainDetail(nodeID, search, selected!), enabled: selected !== null })
  const setDirection = (value: Direction) => { const next = new URLSearchParams(search); next.set('direction', value); next.delete('cursor'); setSearch(next) }
  const maxBytes = Math.max(1, ...(query.data?.items.map((item) => item.bytes) ?? []))
  return <div className="detail-page">
    <div className="page-heading"><div><h1>域名分析</h1><p>按方向分离的域名流量与请求排名</p></div></div>
    <div className="tab-row" role="tablist" aria-label="域名方向">
      <button type="button" role="tab" aria-selected={direction === 'outbound'} className={direction === 'outbound' ? 'active' : ''} onClick={() => setDirection('outbound')}><ArrowUpFromLine size={15} />出站域名</button>
      <button type="button" role="tab" aria-selected={direction === 'inbound'} className={direction === 'inbound' ? 'active' : ''} onClick={() => setDirection('inbound')}><ArrowDownToLine size={15} />入站域名</button>
    </div>
    <FilterBar showDirection={false} searchLabel="筛选域名" />
    <DataState pending={query.isPending} error={query.isError} retry={() => void query.refetch()} freshAt={query.data?.data_fresh_at} gaps={query.data?.partial_data} empty={!query.data?.items.length}>
      <section className="panel data-panel domain-table"><div className="data-header"><span>域名</span><span>证据</span><span>{direction === 'inbound' ? '请求' : '连接'}</span><span>所有者</span><span>流量</span></div>
        {query.data?.items.map((item) => <button type="button" className="data-row domain-row" aria-label={`查看 ${item.domain} 详情`} onClick={() => setSelected(item)} key={`${item.direction}:${item.domain}:${item.confidence}`}>
          <div className="rank-cell"><strong>{item.domain}</strong><span className="traffic-bar"><i style={{ width: `${Math.max(3, item.bytes / maxBytes * 100)}%` }} /></span></div>
          <span data-label="证据"><i className={`confidence ${item.confidence}`}>{confidenceLabel(item.confidence)}</i></span>
          <span data-label={direction === 'inbound' ? '请求' : '连接'}>{direction === 'inbound' ? item.requests : item.connections}</span>
          <span data-label="所有者">{item.owner_count} 个</span>
          <strong data-label="流量">{formatBytes(item.bytes)}</strong>
        </button>)}
      </section>
      <PageAdvance cursor={query.data?.next_cursor} />
    </DataState>
    {selected && <DomainDrawer item={selected} detail={detail.data} pending={detail.isPending} error={detail.isError} close={() => setSelected(null)} />}
  </div>
}

function DomainDrawer({ item, detail, pending, error, close }: { item: DomainItem; detail?: DomainDetail; pending: boolean; error: boolean; close: () => void }) {
  const families = statusFamilies(detail?.statuses ?? [])
  return <aside className="domain-drawer" role="dialog" aria-label={`${item.domain} 详情`}>
    <header><div><span>{item.direction === 'inbound' ? '入站域名' : '出站域名'}</span><h2>{item.domain}</h2></div><button type="button" className="icon-command" onClick={close} aria-label="关闭域名详情" title="关闭"><X size={16} /></button></header>
    {pending && <div className="drawer-state">正在读取详情</div>}
    {error && <div className="drawer-state">域名详情加载失败</div>}
    {detail && <>
      <div className="domain-summary"><div><span>流量</span><strong>{formatBytes(detail.bytes)}</strong></div><div><span>{item.direction === 'inbound' ? '请求' : '连接'}</span><strong>{item.direction === 'inbound' ? detail.requests : detail.connections}</strong></div><div><span>所有者</span><strong>{detail.owner_count} 个所有者</strong></div></div>
      {item.direction === 'inbound' && <DrawerSection title="HTTP 状态"><div className="status-distribution">{families.map((family) => <div key={family.label}><span>{family.label}</span><strong>{family.requests}</strong><small>{formatBytes(family.bytes)}</small></div>)}</div></DrawerSection>}
      {item.direction === 'inbound' && <DrawerSection title="主要来源"><div className="drawer-list">{detail.sources.map((source) => <div key={source.ip}><code>{source.ip}</code><span>{source.requests} 次</span><strong>{formatBytes(source.bytes)}</strong></div>)}{detail.sources.length === 0 && <p>暂无来源记录</p>}</div></DrawerSection>}
      {item.direction === 'outbound' && <DrawerSection title="远程网络"><div className="drawer-list">{(detail.networks ?? []).map((network) => <div key={`${network.country_code}:${network.asn}:${network.classification}`}><strong>{network.organization || network.country_name || '未知网络'}</strong><span>{network.country_code || '—'} · AS{network.asn || '—'} · {network.connections} 条</span><b>{formatBytes(network.bytes)}</b></div>)}{!(detail.networks ?? []).length && <p>暂无网络归属</p>}</div><p className="attribution">IP Geolocation by <a href="https://db-ip.com" target="_blank" rel="noreferrer">DB-IP</a></p></DrawerSection>}
      <DrawerSection title="关联所有者"><div className="drawer-list">{detail.owners.map((owner) => <div key={owner.id}><strong>{owner.name}</strong><span>{item.direction === 'inbound' ? `${owner.requests} 次` : `${owner.requests} 条`}</span><b>{formatBytes(owner.bytes)}</b></div>)}{detail.owners.length === 0 && <p>未关联所有者</p>}</div></DrawerSection>
    </>}
  </aside>
}

function DrawerSection({ title, children }: { title: string; children: React.ReactNode }) { return <section><h3>{title}</h3>{children}</section> }

function statusFamilies(statuses: DomainDetail['statuses']) {
  const groups = new Map<string, { label: string; requests: number; bytes: number }>()
  for (const status of statuses) {
    const label = `${Math.floor(status.status / 100)}xx`
    const current = groups.get(label) ?? { label, requests: 0, bytes: 0 }
    current.requests += status.requests; current.bytes += status.bytes; groups.set(label, current)
  }
  return [...groups.values()].sort((left, right) => left.label.localeCompare(right.label))
}
