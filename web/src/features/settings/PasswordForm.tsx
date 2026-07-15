import { useState } from 'react'
import { KeyRound } from 'lucide-react'
import { changePassword } from '../operations/api'

export function PasswordForm({ onDirty }: { onDirty: (dirty: boolean) => void }) {
  const [current, setCurrent] = useState(''), [next, setNext] = useState(''), [confirm, setConfirm] = useState(''), [message, setMessage] = useState('')
  async function submit() { if (next !== confirm) { setMessage('两次输入的新密码不一致'); return } if (next.length < 12) { setMessage('新密码至少需要 12 个字符'); return } try { await changePassword(current, next); setCurrent(''); setNext(''); setConfirm(''); onDirty(false); setMessage('密码已更新') } catch { setMessage('密码更新失败') } }
  const change = (setter: (value: string) => void) => (event: React.ChangeEvent<HTMLInputElement>) => { setter(event.target.value); onDirty(true) }
  return <div className="form-grid"><label>当前密码<input type="password" autoComplete="current-password" value={current} onChange={change(setCurrent)} /></label><label>新密码<input aria-label="新密码" type="password" autoComplete="new-password" value={next} onChange={change(setNext)} /></label><label>确认新密码<input aria-label="确认新密码" type="password" autoComplete="new-password" value={confirm} onChange={change(setConfirm)} /></label><button className="secondary-command" type="button" onClick={() => void submit()}><KeyRound size={15} />更新密码</button><span className="form-message">{message}</span></div>
}

