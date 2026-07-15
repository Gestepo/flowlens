import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, ExternalLink, Send } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'

import { getAlert } from '../operations/api'
import { formatTime, severityLabel } from './AlertsPage'

export function AlertDetail() {
  const { id = '' } = useParams()
  const query = useQuery({ queryKey: ['alert', id], queryFn: () => getAlert(id), enabled: id !== '' })
  const alert = query.data
  if (!alert) return <div className="page-state">正在读取告警</div>
  const traffic = new URLSearchParams(alert.traffic_filter)
  return <div className="detail-page">
    <Link className="back-link" to="/alerts"><ArrowLeft size={13} />返回告警</Link>
    <div className="page-heading"><div><h1>{alert.title}</h1><p>{alert.node_id} · 首次 {formatTime(alert.first_seen_at)}</p></div><i className={`severity ${alert.severity}`}>{alert.status === 'resolved' ? '已恢复' : severityLabel(alert.severity)}</i></div>
    <section className="operation-section evidence-grid">
      <div><span>观测值</span><strong>{alert.observed_value}</strong></div><div><span>比较值</span><strong>{alert.comparison_value ?? '—'}</strong></div><div><span>时间窗口</span><strong>{Math.round(alert.window_seconds / 60)} 分钟</strong></div><div><span>发生次数</span><strong>{alert.occurrence_count}</strong></div>
    </section>
    <section className="operation-section"><div className="section-heading"><h2>证据</h2><Link className="command-link" to={`/flows?${traffic.toString()}`}>查看相关流量<ExternalLink size={14} /></Link></div><div className="evidence-list">{Object.entries(alert.evidence).map(([key, value]) => <div key={key}><span>{key}</span><code>{value}</code></div>)}</div></section>
    <section className="operation-section"><div className="section-heading"><h2><Send size={16} />Webhook 投递</h2></div><div className="delivery-list">{alert.deliveries?.map((delivery) => <div key={delivery.id}><span>{formatTime(delivery.created_at)}</span><strong>{delivery.status}</strong><span>{delivery.attempt} 次尝试</span><code>{delivery.last_error || delivery.response_status || '—'}</code></div>)}</div></section>
  </div>
}

