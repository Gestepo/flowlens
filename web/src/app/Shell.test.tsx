import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'

import { Shell } from './Shell'

afterEach(cleanup)

it('renders real navigation links and marks the current route', () => {
  render(<MemoryRouter initialEntries={['/domains']}><Shell nodeID="flowlens-node-1"><div>page content</div></Shell></MemoryRouter>)

  expect(screen.getByText('FlowLens')).toBeInTheDocument()
  expect(screen.getByTestId('flowlens-brand-mark')).toHaveAttribute('viewBox', '0 0 32 32')
  expect(screen.getAllByText('flowlens-node-1').length).toBeGreaterThan(0)
  for (const label of ['概览', '实时流量', '域名分析', '容器与进程', '连接走向', '异常与告警']) {
    expect(screen.getAllByRole('link', { name: label }).length).toBeGreaterThan(0)
  }
  expect(screen.getAllByRole('link', { name: '域名分析' })[0]).toHaveAttribute('aria-current', 'page')
  expect(screen.getByRole('link', { name: '设置' })).toHaveAttribute('href', '/settings')
  expect(screen.getByRole('link', { name: '打开告警' })).toHaveAttribute('href', '/alerts')
  expect(screen.getByRole('button', { name: '退出登录' })).toBeInTheDocument()
  expect(screen.getByText('page content')).toBeInTheDocument()
})

it('switches between real monitoring nodes', () => {
  const change = vi.fn()
  render(<MemoryRouter><Shell nodeID="node-a" nodes={[{ id: 'node-a', name: 'Main VPS', status: 'healthy' }, { id: 'node-b', name: 'Edge VPS', status: 'offline' }]} onNodeChange={change}><div /></Shell></MemoryRouter>)
  fireEvent.change(screen.getByRole('combobox', { name: '选择节点' }), { target: { value: 'node-b' } })
  expect(change).toHaveBeenCalledWith('node-b')
  expect(screen.getByRole('option', { name: 'Edge VPS · 离线' })).toBeInTheDocument()
})

it('never labels an offline selected node as healthy collection', () => {
  render(<MemoryRouter><Shell nodeID="node-b" nodes={[{ id: 'node-a', name: 'Main VPS', status: 'healthy' }, { id: 'node-b', name: 'Edge VPS', status: 'offline' }]}><div /></Shell></MemoryRouter>)
  expect(screen.getByText('采集离线')).toBeInTheDocument()
  expect(screen.queryByText('采集正常')).not.toBeInTheDocument()
})
