import { Copy } from 'lucide-react'
import { useState } from 'react'

import { createAgentEnrollment } from '../operations/api'

const installerURL = 'https://raw.githubusercontent.com/Gestepo/flowlens/main/scripts/install-agent-remote.sh'

function validNodeID(value: string) {
  return value !== '' && !/[\u0000-\u001f\u007f]/.test(value) && new TextEncoder().encode(value).length <= 128
}

function shellQuote(value: string) {
  return `'${value.replaceAll("'", "'\"'\"'")}'`
}

export function buildAgentInstallCommand(nodeID: string, origin: string, enrollmentToken: string) {
	if (!validNodeID(nodeID)) return ''
	const endpoint = `${origin}/api/v1/agent/batches`
	return `curl -fsSLo /tmp/flowlens-agent-install.sh ${installerURL} && sudo sh /tmp/flowlens-agent-install.sh --node-id ${shellQuote(nodeID)} --endpoint ${shellQuote(endpoint)} --enrollment-token ${shellQuote(enrollmentToken)}; rm -f /tmp/flowlens-agent-install.sh`
}

export function AgentInstallCommand() {
  const [nodeID, setNodeID] = useState('')
	const [message, setMessage] = useState(''), [command, setCommand] = useState('')
  const origin = window.location.origin
  const endpoint = `${origin}/api/v1/agent/batches`
	const valid = validNodeID(nodeID)

  async function copyCommand() {
    try {
		const enrollment = await createAgentEnrollment()
		const nextCommand = buildAgentInstallCommand(nodeID, origin, enrollment.enrollment_token)
		setCommand(nextCommand)
		if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
		await navigator.clipboard.writeText(nextCommand)
		setMessage('一次性安装命令已复制，10 分钟内使用一次')
    } catch {
      setMessage('复制失败，请选择命令后手动复制')
    }
  }

  return <div className="agent-install-command">
    <div className="agent-install-heading"><div><h3>添加 VPS Agent</h3><p>生成一次性安装命令；登记码仅可使用一次，10 分钟后失效。</p></div></div>
    <div className="form-grid agent-install-fields">
      <label>节点 ID<input aria-label="节点 ID" value={nodeID} onChange={(event) => { setNodeID(event.target.value); setCommand(''); setMessage('') }} placeholder="hk-vps-1" autoCapitalize="none" autoCorrect="off" spellCheck={false} /></label>
      <label>接收地址<input aria-label="Agent 接收地址" value={endpoint} readOnly /></label>
		<label className="agent-command-field">一次性安装命令<textarea aria-label="VPS 安装命令" value={command} readOnly placeholder="输入节点 ID 后生成并复制命令" /></label>
		<div className="agent-install-actions"><span className="form-message" role="status">{nodeID && !valid ? '节点 ID 最长 128 字节，可使用中文、空格和特殊符号；不支持换行或控制字符' : message || '生成的命令仅可使用一次，10 分钟后失效'}</span><button className="icon-command" type="button" onClick={() => void copyCommand()} disabled={!valid} aria-label="生成并复制 VPS 安装命令" title="生成并复制 VPS 安装命令"><Copy size={15} /></button></div>
    </div>
  </div>
}
