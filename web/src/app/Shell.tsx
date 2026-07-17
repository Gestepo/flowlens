import { Activity, Bell, Boxes, Gauge, Globe2, LayoutDashboard, LogOut, Route, Server, Settings } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { BrandMark } from '../components/BrandMark'
import { logout } from '../features/auth/api'

interface ShellProps {
  nodeID: string
  nodes?: Array<{ id: string; name: string; status: string }>
  onNodeChange?: (id: string) => void
  children: ReactNode
}

const monitoring = [
  { label: '概览', icon: LayoutDashboard, path: '/' },
  { label: '实时流量', icon: Gauge, path: '/live' },
  { label: '域名分析', icon: Globe2, path: '/domains' },
  { label: '容器与进程', icon: Boxes, path: '/owners' },
  { label: '连接走向', icon: Route, path: '/flows' },
  { label: '异常与告警', icon: Bell, path: '/alerts' },
]

export function Shell({ nodeID, nodes, onNodeChange, children }: ShellProps) {
  const choices = nodes?.length ? nodes : [{ id: nodeID, name: nodeID, status: 'healthy' }]
  const current = choices.find((node) => node.id === nodeID) ?? choices[0]
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand"><BrandMark className="brand-mark" size={29} /><strong>FlowLens</strong></div>
        <div className="node-summary"><span>当前节点</span><strong><i className={current.status} />{current.name}</strong></div>
        <p className="nav-label">监控</p>
        <nav className="side-nav" aria-label="主导航">
          {monitoring.map(({ label, icon: Icon, path }) => (
            <NavLink key={label} to={path} end={path === '/'} className={({ isActive }) => isActive ? 'active' : undefined}>
              <Icon size={18} /><span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <p className="nav-label system-label">系统</p>
        <div className="side-nav"><NavLink to="/settings" className={({ isActive }) => isActive ? 'active' : undefined}><Settings size={18} /><span>设置</span></NavLink></div>
        <div className={`collector-state ${current.status}`}><Activity size={14} /><span>{collectorStatus(current.status)}</span></div>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <label className="topbar-node"><Server size={16} /><span className="sr-only">选择节点</span><select aria-label="选择节点" value={nodeID} onChange={(event) => onNodeChange?.(event.target.value)}>{choices.map((node) => <option key={node.id} value={node.id}>{node.name} · {nodeStatus(node.status)}</option>)}</select><i className={current.status}>{nodeStatus(current.status)}</i></label>
          <Link className="icon-button" to="/alerts" aria-label="打开告警" title="告警"><Bell size={18} /></Link>
          <button className="icon-button" type="button" onClick={() => void logout()} aria-label="退出登录" title="退出登录"><LogOut size={17} /></button>
        </header>
        <main className="workspace-content">{children}</main>
      </div>

      <nav className="mobile-nav" aria-label="移动端导航">
        {monitoring.slice(0, 5).map(({ label, icon: Icon, path }) => (
          <NavLink key={label} to={path} end={path === '/'} className={({ isActive }) => isActive ? 'active' : undefined}>
            <Icon size={19} /><span>{label}</span>
          </NavLink>
        ))}
      </nav>
    </div>
  )
}

function nodeStatus(status: string) { return status === 'healthy' ? '在线' : status === 'offline' ? '离线' : status === 'partial' ? '部分数据' : status === 'delayed' ? '延迟' : '警告' }
function collectorStatus(status: string) { return status === 'healthy' ? '采集正常' : status === 'offline' ? '采集离线' : status === 'partial' ? '部分数据' : status === 'delayed' ? '采集延迟' : '采集警告' }
