import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { SettingsPage } from './SettingsPage'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('renders account nodes retention thresholds and masked webhook settings', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.endsWith('/nodes')) return response({ items: [
      { id: 'node-a', name: 'Main VPS', status: 'healthy', last_seen_at: new Date().toISOString(), failed_collectors: [] },
      { id: 'node-b', name: 'Edge VPS', status: 'offline', last_seen_at: '2026-07-14T08:30:00Z', failed_collectors: [] },
    ] })
    if (url.endsWith('/settings/webhook')) return response({ enabled: true, endpoint: 'https://hooks.example.test/flowlens', configured: true })
    return response({ detail_retention_days: 30, aggregate_retention_months: 12, alert_rules: [{ id: 'rate', name: '传输速率过高', enabled: true, severity: 'warning', threshold: 100, multiplier: 0 }] })
  }))
  renderSettings()
  expect(await screen.findByText('Main VPS')).toBeInTheDocument()
  expect(screen.getByText('采集正常')).toBeInTheDocument()
  expect(screen.getByText('采集离线')).toBeInTheDocument()
  expect(screen.getByText(/node-b · 最后采集/)).toBeInTheDocument()
  expect(screen.getByLabelText('明细保留天数')).toHaveValue(30)
  expect(screen.getByText('传输速率过高')).toBeInTheDocument()
  expect(screen.getByText('密钥已配置')).toBeInTheDocument()
  expect(screen.queryByDisplayValue(/secret/i)).not.toBeInTheDocument()
})

it('validates password confirmation and retention bounds before saving', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.endsWith('/nodes')) return response({ items: [] })
    if (url.endsWith('/settings/webhook')) return response({ enabled: false, endpoint: '', configured: false })
    return response({ detail_retention_days: 30, aggregate_retention_months: 12, alert_rules: [] })
  }))
  renderSettings()
  await screen.findByLabelText('明细保留天数')
  fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'a secure new password' } })
  fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'does not match' } })
  fireEvent.click(screen.getByRole('button', { name: '更新密码' }))
  expect(await screen.findByText('两次输入的新密码不一致')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('明细保留天数'), { target: { value: '31' } })
  fireEvent.click(screen.getByRole('button', { name: '保存保留策略' }))
  expect(await screen.findByText('明细保留范围为 1–30 天')).toBeInTheDocument()
})

it('submits password changes using the server route method', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
    const url = String(input)
    if (url.endsWith('/nodes')) return response({ items: [] })
    if (url.endsWith('/settings/webhook')) return response({ enabled: false, endpoint: '', configured: false })
    return response({ detail_retention_days: 30, aggregate_retention_months: 12, alert_rules: [] })
  })
  vi.stubGlobal('fetch', fetchMock)
  renderSettings()
  await screen.findByLabelText('明细保留天数')
  fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'current password value' } })
  fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'new password value' } })
  fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'new password value' } })
  fireEvent.click(screen.getByRole('button', { name: '更新密码' }))
  expect(await screen.findByText('密码已更新')).toBeInTheDocument()
  const passwordCall = fetchMock.mock.calls.find(([input]) => String(input).endsWith('/api/v1/auth/password'))
  expect(passwordCall?.[1]).toMatchObject({ method: 'POST' })
})

it('generates a token-free Agent install command from the current panel address', async () => {
  vi.stubGlobal('fetch', settingsFetch())
  renderSettings()
  fireEvent.change(await screen.findByLabelText('节点 ID'), { target: { value: 'hk-vps-1' } })
  const command = screen.getByLabelText('VPS 安装命令') as HTMLTextAreaElement
  expect(command.value).toContain('--node-id hk-vps-1')
  expect(command.value).toContain(`${window.location.origin}/api/v1/agent/batches`)
  expect(command.value).not.toContain('FLOWLENS_AGENT_TOKEN')
  expect(screen.getByRole('button', { name: '复制 VPS 安装命令' })).toBeEnabled()
})

it('blocks invalid node IDs and reports clipboard results', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  vi.stubGlobal('fetch', settingsFetch())
  renderSettings()
  const nodeID = await screen.findByLabelText('节点 ID')
  const copy = screen.getByRole('button', { name: '复制 VPS 安装命令' })
  fireEvent.change(nodeID, { target: { value: 'bad node id' } })
  expect(copy).toBeDisabled()
  fireEvent.change(nodeID, { target: { value: 'edge-vps-1' } })
  fireEvent.click(copy)
  expect(await screen.findByText('安装命令已复制')).toBeInTheDocument()
  expect(writeText).toHaveBeenCalledWith(expect.stringContaining('--node-id edge-vps-1'))

  writeText.mockRejectedValueOnce(new Error('clipboard unavailable'))
  fireEvent.click(copy)
  expect(await screen.findByText('复制失败，请选择命令后手动复制')).toBeInTheDocument()
})

function renderSettings() { const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); return render(<QueryClientProvider client={client}><MemoryRouter><SettingsPage /></MemoryRouter></QueryClientProvider>) }
function response(value: unknown) { return Promise.resolve(new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' } })) }
function settingsFetch() { return vi.fn(async (input: RequestInfo | URL) => {
  const url = String(input)
  if (url.endsWith('/nodes')) return response({ items: [] })
  if (url.endsWith('/settings/webhook')) return response({ enabled: false, endpoint: '', configured: false })
  return response({ detail_retention_days: 30, aggregate_retention_months: 12, alert_rules: [] })
}) }
