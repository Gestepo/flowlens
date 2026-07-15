import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import type { TrafficPoint } from './api'

interface TrafficChartProps {
  points: TrafficPoint[]
}

export function TrafficChart({ points }: TrafficChartProps) {
  const data = points.map((point) => ({
    ...point,
    label: new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(point.at)),
  }))

  return (
    <div className="traffic-chart" role="img" aria-label="入站和出站流量趋势">
      <div className="chart-legend" aria-hidden="true">
        <span><i className="legend-dot inbound" />入站</span>
        <span><i className="legend-dot outbound" />出站</span>
      </div>
      <ResponsiveContainer width="100%" height={218}>
        <LineChart data={data} margin={{ top: 12, right: 8, left: -8, bottom: 0 }}>
          <CartesianGrid vertical={false} stroke="#e5e9ec" />
          <XAxis dataKey="label" axisLine={false} tickLine={false} minTickGap={44} tick={{ fill: '#7b858d', fontSize: 11 }} />
          <YAxis axisLine={false} tickLine={false} tickFormatter={formatRate} width={54} tick={{ fill: '#7b858d', fontSize: 11 }} />
          <Tooltip formatter={(value) => formatRate(Number(value))} labelStyle={{ color: '#20262d' }} />
          <Line type="monotone" dataKey="inbound_bps" name="入站" stroke="#31856b" strokeWidth={2.2} dot={false} isAnimationActive={false} />
          <Line type="monotone" dataKey="outbound_bps" name="出站" stroke="#4a6fa5" strokeWidth={2.2} dot={false} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
      <div className="sr-only">
        <table>
          <caption>入站和出站流量趋势数据</caption>
          <thead><tr><th>时间</th><th>入站</th><th>出站</th></tr></thead>
          <tbody>{data.map((point) => <tr key={point.at}><td>{point.label}</td><td>{point.inbound_bps}</td><td>{point.outbound_bps}</td></tr>)}</tbody>
        </table>
      </div>
    </div>
  )
}

function formatRate(bytesPerSecond: number): string {
  if (bytesPerSecond >= 1_000_000_000) return `${(bytesPerSecond / 1_000_000_000).toFixed(1)} GB/s`
  if (bytesPerSecond >= 1_000_000) return `${(bytesPerSecond / 1_000_000).toFixed(1)} MB/s`
  if (bytesPerSecond >= 1_000) return `${(bytesPerSecond / 1_000).toFixed(1)} KB/s`
  return `${bytesPerSecond.toFixed(0)} B/s`
}
