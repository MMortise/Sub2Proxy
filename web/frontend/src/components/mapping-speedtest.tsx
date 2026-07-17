import { useState } from 'react'
import { Gauge, Loader2 } from 'lucide-react'
import { api } from '@/lib/api'
import { SOURCE_MANUAL, type Mapping, type Node } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

type Result = { loading: boolean; ok?: boolean; delay?: number }

// resolveNodes returns the live nodes a mapping currently targets: its explicit
// list (present ones, in order) or the regex-matched set for a filter mapping.
function resolveNodes(m: Mapping, all: Node[]): Node[] {
  if (m.node_filter) {
    try {
      const re = new RegExp(m.node_filter)
      return all.filter((n) => re.test(n.name))
    } catch {
      return []
    }
  }
  const byId = new Map(all.map((n) => [n.id, n]))
  return (m.nodes ?? []).map((r) => byId.get(r.id)).filter((n): n is Node => !!n)
}

// MappingSpeedtest is the actions-column speedtest button: clicking opens a
// popover that tests every node the mapping targets and shows subscription,
// node name, and latency per row.
export function MappingSpeedtest({
  mapping,
  nodes,
  subName,
}: {
  mapping: Mapping
  nodes: Node[]
  subName: Record<string, string>
}) {
  const [open, setOpen] = useState(false)
  const [results, setResults] = useState<Record<string, Result>>({})
  const resolved = resolveNodes(mapping, nodes)

  const sourceLabel = (n: Node) => {
    const s = n.sources[0]
    if (!s) return '—'
    return s === SOURCE_MANUAL ? '手动' : (subName[s] ?? s)
  }

  const runTests = () => {
    setResults(Object.fromEntries(resolved.map((n) => [n.id, { loading: true }])))
    for (const n of resolved) {
      api
        .testNode(n.id)
        .then((r) =>
          setResults((p) => ({ ...p, [n.id]: { loading: false, ok: r.ok, delay: r.delay_ms } })),
        )
        .catch(() => setResults((p) => ({ ...p, [n.id]: { loading: false, ok: false } })))
    }
  }

  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (o) runTests()
  }

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="测速">
          <Gauge className="h-4 w-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        <div className="border-b border-border px-3 py-2 text-xs font-medium text-muted-foreground">
          节点测速 · 端口 {mapping.port}
        </div>
        {resolved.length === 0 ? (
          <div className="px-3 py-4 text-center text-sm text-muted-foreground">无可用节点</div>
        ) : (
          <ul className="max-h-64 divide-y divide-border overflow-auto">
            {resolved.map((n) => (
              <li key={n.id} className="flex items-center gap-2 px-3 py-2 text-sm">
                <Badge variant="muted" className="max-w-[6rem] shrink-0 truncate">
                  {sourceLabel(n)}
                </Badge>
                <span className="flex-1 truncate">{n.name}</span>
                <ResultView r={results[n.id]} />
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  )
}

function ResultView({ r }: { r?: Result }) {
  if (!r || r.loading) {
    return <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" />
  }
  if (!r.ok) return <span className="shrink-0 text-xs text-destructive">超时</span>
  const d = r.delay ?? 0
  const color = d < 200 ? 'text-success' : d < 500 ? 'text-warning' : 'text-destructive'
  return <span className={cn('shrink-0 text-xs tabular-nums', color)}>{d} ms</span>
}
