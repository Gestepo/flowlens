import { Eye, EyeOff, LogIn } from 'lucide-react'
import { FormEvent, useState } from 'react'

import { BrandMark } from '../../components/BrandMark'
import { APIError, BrowserSession, login } from './api'

export function LoginPage({ onAuthenticated }: { onAuthenticated: (session: BrowserSession) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: FormEvent) {
    event.preventDefault()
    setPending(true)
    setError('')
    try {
      const session = await login(username, password)
      setPassword('')
      onAuthenticated(session)
    } catch (reason) {
      setPassword('')
      if (reason instanceof APIError && reason.status === 429) {
        setError(`请在 ${Math.max(1, Math.ceil(reason.retryAfter / 60))} 分钟后重试`)
      } else {
        setError(reason instanceof Error ? reason.message : '登录失败')
      }
    } finally {
      setPending(false)
    }
  }

  return <main className="login-page">
    <section className="login-tool" aria-labelledby="login-title">
      <div className="login-brand"><BrandMark className="brand-symbol" size={32} /><strong>FlowLens</strong></div>
      <div className="login-heading"><h1 id="login-title">登录 FlowLens</h1><span>管理员访问</span></div>
      <form onSubmit={submit}>
        <label htmlFor="login-username">用户名</label>
        <input id="login-username" name="username" autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} required maxLength={128} />
        <label htmlFor="login-password">密码</label>
        <div className="password-field">
          <input id="login-password" name="password" type={showPassword ? 'text' : 'password'} autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
          <button type="button" className="field-icon" onClick={() => setShowPassword((value) => !value)} aria-label={showPassword ? '隐藏密码' : '显示密码'} title={showPassword ? '隐藏密码' : '显示密码'}>
            {showPassword ? <EyeOff size={17} /> : <Eye size={17} />}
          </button>
        </div>
        <div className="login-message" role="alert">{error}</div>
        <button className="primary-command" type="submit" disabled={pending}>
          <LogIn size={16} /><span>{pending ? '正在登录' : '登录'}</span>
        </button>
      </form>
    </section>
  </main>
}
