import { Search, SlidersHorizontal, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

const ranges = [{ value: '1h', label: '1 小时' }, { value: '24h', label: '24 小时' }, { value: '7d', label: '7 天' }, { value: '30d', label: '30 天' }]

export function FilterBar({ showDirection = true, searchLabel = '筛选域名或 IP', searchKey = 'domain' }: { showDirection?: boolean; searchLabel?: string; searchKey?: 'domain' | 'owner' }) {
  const [search, setSearch] = useSearchParams()
  const [advanced, setAdvanced] = useState(() => ['confidence', 'ip', 'port', 'protocol', 'sort'].some((key) => search.has(key)))
  const latestSearch = useRef(search)
  if (latestSearch.current.toString() !== search.toString()) latestSearch.current = search
  const update = (key: string, value: string) => {
    const next = new URLSearchParams(latestSearch.current)
    if (value) next.set(key, value); else next.delete(key)
    next.delete('cursor')
    latestSearch.current = next
    setSearch(next)
  }
  const setRange = (value: string) => { const next = new URLSearchParams(latestSearch.current); next.set('range', value); next.delete('start'); next.delete('end'); next.delete('cursor'); latestSearch.current = next; setSearch(next) }
  const setDate = (key: 'start' | 'end', value: string) => update(key, value ? new Date(value).toISOString() : '')
  return <div className={`filter-bar ${advanced ? 'expanded' : ''}`}>
    <div className="filter-main">
    <div className="range-control compact" aria-label="时间范围">
      {ranges.map((item) => <button key={item.value} type="button" className={!search.has('start') && (search.get('range') ?? '24h') === item.value ? 'active' : ''} onClick={() => setRange(item.value)}>{item.label}</button>)}
    </div>
    {showDirection && <select aria-label="流量方向" value={search.get('direction') ?? ''} onChange={(event) => update('direction', event.target.value)}>
      <option value="">全部方向</option><option value="inbound">入站</option><option value="outbound">出站</option><option value="internal">内部</option><option value="container_to_container">容器间</option>
    </select>}
    <label className="filter-search"><Search size={15} /><span className="sr-only">{searchLabel}</span><input aria-label={searchLabel} value={search.get(searchKey) ?? ''} placeholder={searchLabel} onChange={(event) => update(searchKey, event.target.value)} /></label>
    <button className="icon-command filter-toggle" type="button" onClick={() => setAdvanced((value) => !value)} aria-label={advanced ? '收起高级筛选' : '打开高级筛选'} title={advanced ? '收起筛选' : '高级筛选'}>{advanced ? <X size={15} /> : <SlidersHorizontal size={15} />}</button>
    </div>
    {advanced && <div className="filter-advanced">
      <select aria-label="域名置信度" value={search.get('confidence') ?? ''} onChange={(event) => update('confidence', event.target.value)}><option value="">全部证据</option><option value="confirmed">已确认</option><option value="inferred">推断</option><option value="ip_only">仅 IP</option></select>
      <label><span>IP</span><input aria-label="筛选 IP" value={search.get('ip') ?? ''} onChange={(event) => update('ip', event.target.value)} placeholder="1.1.1.1" /></label>
      <label><span>端口</span><input aria-label="筛选端口" type="number" min="1" max="65535" value={search.get('port') ?? ''} onChange={(event) => update('port', event.target.value)} placeholder="443" /></label>
      <select aria-label="网络协议" value={search.get('protocol') ?? ''} onChange={(event) => update('protocol', event.target.value)}><option value="">全部协议</option><option value="tcp">TCP</option><option value="udp">UDP</option></select>
      <select aria-label="排序方式" value={search.get('sort') ?? 'bytes'} onChange={(event) => update('sort', event.target.value)}><option value="bytes">按流量</option><option value="connections">按连接</option><option value="requests">按请求</option><option value="time">按时间</option></select>
      <label className="date-filter"><span>开始</span><input aria-label="开始时间" type="datetime-local" value={localDate(search.get('start'))} onChange={(event) => setDate('start', event.target.value)} /></label>
      <label className="date-filter"><span>结束</span><input aria-label="结束时间" type="datetime-local" value={localDate(search.get('end'))} onChange={(event) => setDate('end', event.target.value)} /></label>
    </div>}
  </div>
}

function localDate(value: string | null) { if (!value) return ''; const date = new Date(value); const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000); return local.toISOString().slice(0, 16) }
