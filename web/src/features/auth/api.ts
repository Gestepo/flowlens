export interface BrowserSession {
  authenticated: true
  username: string
  csrf_token: string
}

export class APIError extends Error {
  status: number
  code: string
  retryAfter: number

  constructor(status: number, code: string, message: string, retryAfter = 0) {
    super(message)
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

let csrfToken = ''
let unauthorizedDispatched = false

export async function getSession(): Promise<BrowserSession | null> {
  const response = await fetch('/api/v1/session', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
  if (response.status === 401) return null
  if (!response.ok) throw await responseError(response)
  const session = await parseSession(response)
  csrfToken = session.csrf_token
  unauthorizedDispatched = false
  return session
}

export async function login(username: string, password: string): Promise<BrowserSession> {
  const response = await fetch('/api/v1/auth/login', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!response.ok) throw await responseError(response)
  const session = await parseSession(response)
  csrfToken = session.csrf_token
  unauthorizedDispatched = false
  return session
}

export async function apiFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && csrfToken) headers.set('X-CSRF-Token', csrfToken)
  const response = await fetch(input, { ...init, method, headers, credentials: 'same-origin' })
  if (response.status === 401 && !unauthorizedDispatched) {
    unauthorizedDispatched = true
    csrfToken = ''
    window.dispatchEvent(new CustomEvent('flowlens:unauthorized'))
  }
  return response
}

export async function logout(): Promise<void> {
  const response = await apiFetch('/api/v1/auth/logout', { method: 'POST' })
  if (!response.ok) throw await responseError(response)
  csrfToken = ''
  window.dispatchEvent(new CustomEvent('flowlens:unauthorized'))
}

async function parseSession(response: Response): Promise<BrowserSession> {
  const value = await response.json() as Partial<BrowserSession>
  if (value.authenticated !== true || typeof value.username !== 'string' || typeof value.csrf_token !== 'string') {
    throw new APIError(response.status, 'invalid_response', '会话响应格式无效')
  }
  return value as BrowserSession
}

async function responseError(response: Response): Promise<APIError> {
  let code = 'request_failed'
  let message = `请求失败 (${response.status})`
  try {
    const value = await response.json() as { error?: { code?: string; message?: string } }
    if (value.error?.code) code = value.error.code
    if (value.error?.message) message = value.error.message
  } catch {
    // The status still provides a useful error when a proxy returns a non-JSON body.
  }
  const retryAfter = Number.parseInt(response.headers.get('Retry-After') ?? '0', 10)
  return new APIError(response.status, code, message, Number.isFinite(retryAfter) ? retryAfter : 0)
}
