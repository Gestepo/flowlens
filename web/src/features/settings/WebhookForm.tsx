import { useEffect, useState } from 'react'
import { Send, Save } from 'lucide-react'
import { testWebhook, updateWebhook, WebhookSettings } from '../operations/api'

export function WebhookForm({ settings, onDirty }: { settings: WebhookSettings; onDirty: (dirty: boolean) => void }) {
  const [enabled, setEnabled] = useState(settings.enabled), [endpoint, setEndpoint] = useState(settings.endpoint), [secret, setSecret] = useState(''), [message, setMessage] = useState('')
  useEffect(() => { setEnabled(settings.enabled); setEndpoint(settings.endpoint) }, [settings])
  async function save() { try { await updateWebhook({ enabled, endpoint, secret }); setSecret(''); onDirty(false); setMessage('Webhook 设置已保存') } catch { setMessage('Webhook 设置保存失败') } }
  async function test() { try { await testWebhook(); setMessage('测试投递已加入队列') } catch { setMessage('测试投递失败') } }
  return <div className="form-grid webhook-form"><label className="switch-row"><input type="checkbox" checked={enabled} onChange={(event) => { setEnabled(event.target.checked); onDirty(true) }} /><span>启用 Webhook</span></label><label>HTTPS 地址<input value={endpoint} onChange={(event) => { setEndpoint(event.target.value); onDirty(true) }} placeholder="https://" /></label><label>签名密钥<input type="password" value={secret} onChange={(event) => { setSecret(event.target.value); onDirty(true) }} placeholder={settings.configured ? '密钥已配置' : '输入签名密钥'} /></label><div className="command-row"><button className="secondary-command" type="button" onClick={() => void save()}><Save size={15} />保存 Webhook</button><button className="icon-command" type="button" onClick={() => void test()} aria-label="发送测试 Webhook" title="发送测试 Webhook"><Send size={16} /></button></div><span className="form-message">{settings.configured && !secret ? '密钥已配置' : message}</span></div>
}

