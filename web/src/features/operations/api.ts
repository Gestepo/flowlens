import { apiFetch } from '../auth/api'

export interface AlertItem { id: number; rule_id: string; status: string; severity: string; node_id: string; owner_id: string | null; title: string; evidence: Record<string, string>; traffic_filter: Record<string, string>; observed_value: number; comparison_value: number | null; window_seconds: number; first_seen_at: string; last_seen_at: string; resolved_at: string | null; occurrence_count: number; deliveries?: Delivery[] }
export interface Delivery { id: number; status: string; attempt: number; response_status: number | null; last_error: string; created_at: string; delivered_at: string | null }
export interface NodeItem { id: string; name: string; status: string; last_seen_at: string; failed_collectors: string[] }
export interface AlertRule { id: string; name: string; enabled: boolean; severity: string; threshold: number; multiplier: number }
export interface OperationsSettings { detail_retention_days: number; aggregate_retention_months: number; alert_rules: AlertRule[] }
export interface WebhookSettings { enabled: boolean; endpoint: string; configured: boolean }
export interface AgentEnrollment { enrollment_token: string; expires_at: string }

export async function getAlerts(status: string): Promise<AlertItem[]> { return (await requestJSON<{ items: AlertItem[] }>(`/api/v1/alerts?status=${encodeURIComponent(status)}`)).items }
export async function getAlert(id: string): Promise<AlertItem> { return requestJSON(`/api/v1/alerts/${encodeURIComponent(id)}`) }
export async function getSettings(): Promise<OperationsSettings> { return requestJSON('/api/v1/settings') }
export async function getNodes(): Promise<NodeItem[]> { return (await requestJSON<{ items: NodeItem[] }>('/api/v1/nodes')).items }
export async function getWebhook(): Promise<WebhookSettings> { return requestJSON('/api/v1/settings/webhook') }
export async function updateRetention(detail_days: number, aggregate_months: number): Promise<void> { await command('/api/v1/settings/retention', { detail_days, aggregate_months }) }
export async function updateRules(rules: AlertRule[]): Promise<void> { await command('/api/v1/settings/alerts', { rules }) }
export async function renameNode(id: string, name: string): Promise<void> { await command(`/api/v1/nodes/${encodeURIComponent(id)}`, { name }) }
export async function updateWebhook(value: { enabled: boolean; endpoint: string; secret: string }): Promise<WebhookSettings> { return commandJSON('/api/v1/settings/webhook', value) }
export async function testWebhook(): Promise<void> { const response = await apiFetch('/api/v1/settings/webhook/test', { method: 'POST' }); if (!response.ok) throw new Error('Webhook 测试投递失败') }
export async function changePassword(current_password: string, new_password: string): Promise<void> { await command('/api/v1/auth/password', { current_password, new_password }, 'POST') }
export async function createAgentEnrollment(): Promise<AgentEnrollment> { return commandJSON('/api/v1/settings/agent-enrollment', {}, 'POST') }

async function requestJSON<T>(url: string): Promise<T> { const response = await apiFetch(url); if (!response.ok) throw new Error(`请求失败 (${response.status})`); return response.json() as Promise<T> }
async function command(url: string, value: unknown, method = 'PUT'): Promise<void> { const response = await apiFetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) }); if (!response.ok) throw new Error(`保存失败 (${response.status})`) }
async function commandJSON<T>(url: string, value: unknown, method = 'PUT'): Promise<T> { const response = await apiFetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) }); if (!response.ok) throw new Error(`保存失败 (${response.status})`); return response.json() as Promise<T> }
