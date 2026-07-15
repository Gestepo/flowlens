import { ChevronRight } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

export function PageAdvance({ cursor }: { cursor?: string }) {
  const [search, setSearch] = useSearchParams()
  if (!cursor) return null
  return <div className="page-advance"><button type="button" onClick={() => { const next = new URLSearchParams(search); next.set('cursor', cursor); setSearch(next) }}>下一页<ChevronRight size={14} /></button></div>
}

