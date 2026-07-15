import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ReactNode, useEffect, useState } from 'react'

import { BrowserSession, getSession } from './api'
import { LoginPage } from './LoginPage'

export function SessionBoundary({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [forcedLoggedOut, setForcedLoggedOut] = useState(false)
  const session = useQuery({ queryKey: ['browser-session'], queryFn: getSession, retry: false, staleTime: 30_000 })

  useEffect(() => {
    const unauthorized = () => {
      queryClient.clear()
      setForcedLoggedOut(true)
    }
    window.addEventListener('flowlens:unauthorized', unauthorized)
    return () => window.removeEventListener('flowlens:unauthorized', unauthorized)
  }, [queryClient])

  function authenticated(value: BrowserSession) {
    setForcedLoggedOut(false)
    queryClient.setQueryData(['browser-session'], value)
  }

  if (session.isPending && !forcedLoggedOut) return <div className="session-loading" aria-label="正在验证会话" />
  if (forcedLoggedOut || !session.data) return <LoginPage onAuthenticated={authenticated} />
  return children
}

