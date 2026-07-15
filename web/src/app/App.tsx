import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { OverviewPage } from '../features/overview/OverviewPage'
import { DomainsPage } from '../features/domains/DomainsPage'
import { FlowsPage } from '../features/flows/FlowsPage'
import { LivePage } from '../features/live/LivePage'
import { OwnerDetailPage } from '../features/owners/OwnerDetailPage'
import { OwnersPage } from '../features/owners/OwnersPage'
import { AlertsPage } from '../features/alerts/AlertsPage'
import { AlertDetail } from '../features/alerts/AlertDetail'
import { SettingsPage } from '../features/settings/SettingsPage'
import { SessionBoundary } from '../features/auth/SessionBoundary'
import { getNodes } from '../features/operations/api'
import { Shell } from './Shell'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 4_000 },
  },
})

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <SessionBoundary>
          <NodeWorkspace />
        </SessionBoundary>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

function NodeWorkspace() {
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: getNodes, refetchInterval: 15_000 })
  const [selected, setSelected] = useState(() => localStorage.getItem('flowlens.selected-node') ?? '')
  useEffect(() => {
    if (!nodes.data?.length) return
    if (!nodes.data.some((node) => node.id === selected)) setSelected(nodes.data[0].id)
  }, [nodes.data, selected])
  if (nodes.isPending || !nodes.data?.length) return <div className="page-state">正在读取节点</div>
  const nodeID = nodes.data.some((node) => node.id === selected) ? selected : nodes.data[0].id
  function changeNode(id: string) { setSelected(id); localStorage.setItem('flowlens.selected-node', id) }
  return <Shell nodeID={nodeID} nodes={nodes.data} onNodeChange={changeNode}>
    <Routes>
      <Route path="/" element={<OverviewPage nodeID={nodeID} />} />
      <Route path="/live" element={<LivePage nodeID={nodeID} />} />
      <Route path="/domains" element={<DomainsPage nodeID={nodeID} />} />
      <Route path="/owners" element={<OwnersPage nodeID={nodeID} />} />
      <Route path="/owners/:id" element={<OwnerDetailPage nodeID={nodeID} />} />
      <Route path="/flows" element={<FlowsPage nodeID={nodeID} />} />
      <Route path="/alerts" element={<AlertsPage />} />
      <Route path="/alerts/:id" element={<AlertDetail />} />
      <Route path="/settings" element={<SettingsPage />} />
      <Route path="*" element={<OverviewPage nodeID={nodeID} />} />
    </Routes>
  </Shell>
}
