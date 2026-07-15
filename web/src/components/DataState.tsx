import { AlertTriangle, History, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'

type CollectorGap = { collector: string; code: string; at: string; recovered: boolean }

export function DataState({ pending, error, retry, freshAt, gaps = [], empty, children }: { pending: boolean; error: boolean; retry: () => void; freshAt?: string; gaps?: CollectorGap[]; empty: boolean; children: ReactNode }) {
  if (pending) return <div className="page-state">正在加载</div>
  if (error) return <div className="page-state error-state"><span>数据加载失败</span><button type="button" onClick={retry}><RefreshCw size={15} />重试</button></div>
  const currentGaps = gaps.filter((gap) => !gap.recovered)
  const recoveredGaps = gaps.filter((gap) => gap.recovered)
  return <>
    {freshAt && Date.now() - Date.parse(freshAt) > 15_000 && <div className="status-banner warning"><AlertTriangle size={16} /><strong>数据延迟</strong><span>最后更新 {new Date(freshAt).toLocaleTimeString('zh-CN', { hour12: false })}</span></div>}
    {currentGaps.length > 0 && <div className="status-banner warning"><AlertTriangle size={16} /><strong>部分数据缺失</strong><span>{currentGaps.map((gap) => gap.collector).join('、')}</span></div>}
    {recoveredGaps.length > 0 && <div className="status-banner history"><History size={16} /><strong>历史数据缺口</strong>{recoveredGaps.map((gap) => <span key={`${gap.collector}-${gap.at}`}>{gap.collector} <time dateTime={gap.at}>{new Date(gap.at).toLocaleString('zh-CN', { hour12: false })}</time></span>)}</div>}
    {empty ? <div className="panel empty-table">当前筛选条件下没有数据</div> : children}
  </>
}
