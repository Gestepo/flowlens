import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, ChevronRight } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'

import { getAlerts } from '../operations/api'

export function AlertsPage() {
  const [search, setSearch] = useSearchParams()
  const status = search.get('status') === 'resolved' ? 'resolved' : 'open'
  const query = useQuery({ queryKey: ['alerts', status], queryFn: () => getAlerts(status) })
  return <div className="detail-page">
    <div className="page-heading"><div><h1 className="icon-heading"><AlertTriangle size={20} />异常与告警</h1><p>最近发生的运行异常和投递状态</p></div></div>
    <div className="tab-row" role="tablist">
      <button role="tab" aria-selected={status === 'open'} className={status === 'open' ? 'active' : ''} onClick={() => setSearch({ status: 'open' })}>开放告警</button>
      <button role="tab" aria-selected={status === 'resolved'} className={status === 'resolved' ? 'active' : ''} onClick={() => setSearch({ status: 'resolved' })}>已恢复</button>
    </div>
    <section className="panel data-panel alerts-table">
      <div className="data-header alert-grid"><span>告警</span><span>节点</span><span>状态</span><span>最近发生</span><span /></div>
      {query.data?.map((alert) => <div className="data-row alert-grid" key={alert.id}>
        <Link className="alert-title" to={`/alerts/${alert.id}`}><strong>{alert.title}</strong><small>{alert.occurrence_count} 次 · {Math.round(alert.window_seconds / 60)} 分钟窗口</small></Link>
        <span data-label="节点">{alert.node_id}</span>
        <span data-label="状态"><i className={`severity ${alert.severity}`}>{alert.status === 'resolved' ? '已恢复' : severityLabel(alert.severity)}</i></span>
        <span data-label="最近发生">{formatTime(alert.last_seen_at)}</span>
        <Link className="row-action" to={`/alerts/${alert.id}`} aria-label={`查看 ${alert.title}`}><ChevronRight size={16} /></Link>
      </div>)}
      {!query.isPending && !query.data?.length && <div className="empty-table"><CheckCircle2 size={18} />当前没有{status === 'open' ? '开放' : '已恢复'}告警</div>}
    </section>
  </div>
}

export function severityLabel(value: string) { return value === 'critical' ? '严重' : value === 'warning' ? '警告' : '提示' }
export function formatTime(value: string) { return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }

