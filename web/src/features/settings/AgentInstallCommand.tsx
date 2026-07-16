import { Copy } from 'lucide-react'
import { useState } from 'react'

const nodeIDPattern = /^[A-Za-z0-9_.:-]{1,128}$/
const installerURL = 'https://raw.githubusercontent.com/Gestepo/flowlens/main/scripts/install-agent-remote.sh'

export function buildAgentInstallCommand(nodeID: string, origin: string) {
  if (!nodeIDPattern.test(nodeID)) return ''
  const endpoint = `${origin}/api/v1/agent/batches`
  return `curl -fsSLo /tmp/flowlens-agent-install.sh ${installerURL} && sudo sh /tmp/flowlens-agent-install.sh --node-id ${nodeID} --endpoint ${endpoint}; rm -f /tmp/flowlens-agent-install.sh`
}

export function AgentInstallCommand() {
  const [nodeID, setNodeID] = useState('')
  const [message, setMessage] = useState('')
  const origin = window.location.origin
  const endpoint = `${origin}/api/v1/agent/batches`
  const command = buildAgentInstallCommand(nodeID, origin)
  const valid = command !== ''

  async function copyCommand() {
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(command)
      setMessage('安装命令已复制')
    } catch {
      setMessage('复制失败，请选择命令后手动复制')
    }
  }

  return <div className="agent-install-command">
    <div className="agent-install-heading"><div><h3>添加 VPS Agent</h3><p>命令使用当前面板地址；令牌将在新 VPS 终端中隐藏输入。</p></div></div>
    <div className="form-grid agent-install-fields">
      <label>节点 ID<input aria-label="节点 ID" value={nodeID} onChange={(event) => { setNodeID(event.target.value); setMessage('') }} placeholder="hk-vps-1" autoCapitalize="none" autoCorrect="off" spellCheck={false} /></label>
      <label>接收地址<input aria-label="Agent 接收地址" value={endpoint} readOnly /></label>
      <label className="agent-command-field">安装命令<textarea aria-label="VPS 安装命令" value={command} readOnly placeholder="输入节点 ID 后生成命令" /></label>
      <div className="agent-install-actions"><span className="form-message" role="status">{nodeID && !valid ? '节点 ID 仅支持字母、数字、点、下划线、冒号和连字符，最长 128 位' : message}</span><button className="icon-command" type="button" onClick={() => void copyCommand()} disabled={!valid} aria-label="复制 VPS 安装命令" title="复制 VPS 安装命令"><Copy size={15} /></button></div>
    </div>
  </div>
}
