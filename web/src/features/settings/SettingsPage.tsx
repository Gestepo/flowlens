import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Bell, Database, Save, Server, Settings, ShieldCheck, Webhook } from 'lucide-react'
import { useEffect, useState } from 'react'

import { getNodes, getSettings, getWebhook, renameNode, updateRetention, updateRules, AlertRule } from '../operations/api'
import { PasswordForm } from './PasswordForm'
import { WebhookForm } from './WebhookForm'

export function SettingsPage() {
  const client = useQueryClient(), settings = useQuery({ queryKey: ['operations-settings'], queryFn: getSettings }), nodes = useQuery({ queryKey: ['nodes'], queryFn: getNodes }), webhook = useQuery({ queryKey: ['webhook-settings'], queryFn: getWebhook })
  const [dirty, setDirty] = useState(false), [detail, setDetail] = useState(30), [months, setMonths] = useState(12), [rules, setRules] = useState<AlertRule[]>([]), [message, setMessage] = useState('')
  useEffect(() => { if (settings.data) { setDetail(settings.data.detail_retention_days); setMonths(settings.data.aggregate_retention_months); setRules(settings.data.alert_rules) } }, [settings.data])
  useEffect(() => { const warn = (event: BeforeUnloadEvent) => { if (dirty) event.preventDefault() }; window.addEventListener('beforeunload', warn); return () => window.removeEventListener('beforeunload', warn) }, [dirty])
  const rename = useMutation({ mutationFn: ({ id, name }: { id: string; name: string }) => renameNode(id, name), onSuccess: () => client.invalidateQueries({ queryKey: ['nodes'] }) })
  async function saveRetention() { if (detail < 1 || detail > 30) { setMessage('明细保留范围为 1–30 天'); return } if (months < 1 || months > 12) { setMessage('日汇总保留范围为 1–12 个月'); return } await updateRetention(detail, months); setDirty(false); setMessage('保留策略已保存') }
  async function saveRules() { await updateRules(rules); setDirty(false); setMessage('告警阈值已保存') }
  return <div className="detail-page settings-page"><div className="page-heading"><div><h1 className="icon-heading"><Settings size={20} />设置</h1><p>账户、节点和运行策略</p></div></div>
    <SettingsSection icon={<ShieldCheck size={17} />} title="管理员账户"><PasswordForm onDirty={setDirty} /></SettingsSection>
    <SettingsSection icon={<Server size={17} />} title="节点">{nodes.data?.map((node) => <NodeRow key={node.id} node={node} save={(name) => rename.mutate({ id: node.id, name })} />)}</SettingsSection>
    <SettingsSection icon={<Database size={17} />} title="数据保留"><div className="form-grid compact-form"><label>明细保留天数<input aria-label="明细保留天数" type="number" min="1" max="30" value={detail} onChange={(event) => { setDetail(Number(event.target.value)); setDirty(true) }} /></label><label>日汇总保留月数<input type="number" min="1" max="12" value={months} onChange={(event) => { setMonths(Number(event.target.value)); setDirty(true) }} /></label><button className="secondary-command" type="button" onClick={() => void saveRetention()}><Save size={15} />保存保留策略</button></div></SettingsSection>
    <SettingsSection icon={<Bell size={17} />} title="告警阈值"><div className="rule-list">{rules.map((rule, index) => <div className="rule-row" key={rule.id}><label className="switch-row"><input type="checkbox" checked={rule.enabled} onChange={(event) => { const copy = [...rules]; copy[index] = { ...rule, enabled: event.target.checked }; setRules(copy); setDirty(true) }} /><span>{rule.name}</span></label><input aria-label={`${rule.name}阈值`} type="number" value={rule.multiplier || rule.threshold} onChange={(event) => { const value = Number(event.target.value); const copy = [...rules]; copy[index] = rule.multiplier ? { ...rule, multiplier: value } : { ...rule, threshold: value }; setRules(copy); setDirty(true) }} /></div>)}<button className="secondary-command" type="button" onClick={() => void saveRules()}><Save size={15} />保存告警阈值</button></div></SettingsSection>
    <SettingsSection icon={<Webhook size={17} />} title="Webhook">{webhook.data && <WebhookForm settings={webhook.data} onDirty={setDirty} />}</SettingsSection>
    <div className="settings-feedback" role="status">{message}</div>
  </div>
}

function SettingsSection({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) { return <section className="operation-section settings-section"><div className="section-heading"><h2>{icon}{title}</h2></div>{children}</section> }
function NodeRow({ node, save }: { node: Awaited<ReturnType<typeof getNodes>>[number]; save: (name: string) => void }) { const [name, setName] = useState(node.name); return <div className="node-row"><span className={`node-state ${node.status}`}><Activity size={15} /></span><div><strong>{node.name}</strong><small>{node.id} · 最后采集 {formatNodeTime(node.last_seen_at)}</small></div><i>{node.status === 'healthy' ? '采集正常' : node.status === 'offline' ? '采集离线' : node.status === 'partial' ? '部分数据' : node.status === 'delayed' ? '采集延迟' : '采集警告'}</i><input aria-label={`${node.name}节点名称`} value={name} onChange={(event) => setName(event.target.value)} /><button className="icon-command" type="button" onClick={() => save(name)} aria-label={`保存 ${node.name}`} title="保存节点名称"><Save size={15} /></button></div> }
function formatNodeTime(value: string) { return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value)) }
