import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Gauge, Plus, Server, Trash2, Zap } from 'lucide-react'
import { toast } from 'sonner'
import { api, ApiError, errMessage } from '@/lib/api'
import type { Node, Subscription } from '@/lib/types'
import { SOURCE_ALL, SOURCE_MANUAL, hasManualNode } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { CenteredSpinner, Spinner } from '@/components/ui/spinner'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { PageHeader } from '@/components/page-header'

function DelayCell({ node }: { node: Node }) {
  if (!node.tested) return <span className="text-muted-foreground">-</span>
  if (!node.alive) return <span className="text-destructive">不可用</span>
  const d = node.delay_ms ?? 0
  const color =
    d < 200 ? 'text-success' : d < 500 ? 'text-warning' : 'text-destructive'
  return <span className={cn('tabular-nums', color)}>{d} ms</span>
}

function isManualOnly(node: Node) {
  return node.sources.length === 1 && node.sources[0] === 'manual'
}

function AddNodeDialog({
  open,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const [link, setLink] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (open) setLink('')
  }, [open])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!link.trim()) return
    setSaving(true)
    try {
      const node = await api.addNode(link.trim())
      toast.success(`已添加节点「${node.name}」`)
      onOpenChange(false)
      onSaved()
    } catch (err) {
      toast.error(errMessage(err, '解析失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>添加节点</DialogTitle>
          <DialogDescription>
            粘贴分享链接（vmess:// vless:// ss:// trojan:// 等），解析成功后加入节点池。
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="node-link">分享链接</Label>
            <Textarea
              id="node-link"
              value={link}
              onChange={(e) => setLink(e.target.value)}
              placeholder="vmess://..."
              className="font-mono text-xs"
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" loading={saving} disabled={!link.trim()}>
              添加
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function NodesPage() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [subs, setSubs] = useState<Subscription[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [source, setSource] = useState<string>(SOURCE_ALL)
  const [addOpen, setAddOpen] = useState(false)
  const [testing, setTesting] = useState<Record<string, boolean>>({})
  const [testingAll, setTestingAll] = useState(false)
  const [deleting, setDeleting] = useState<Node | null>(null)
  const [deleteLoading, setDeleteLoading] = useState(false)
  const pollRef = useRef<number | null>(null)

  const load = useCallback(async () => {
    try {
      const data = await api.listNodes()
      setNodes(data)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      setError(errMessage(err, '加载失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  // Load subscriptions once for source labels + the filter dropdown.
  useEffect(() => {
    api.listSubscriptions().then(setSubs).catch(() => {})
  }, [])

  useEffect(() => {
    load()
  }, [load])

  // Cleanup polling on unmount.
  useEffect(() => {
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current)
    }
  }, [])

  const subName = useMemo(() => {
    const m: Record<string, string> = {}
    for (const s of subs) m[s.id] = s.name
    return m
  }, [subs])
  const sourceLabel = (src: string) => (src === SOURCE_MANUAL ? '手动' : subName[src] ?? src)

  const hasManual = useMemo(() => hasManualNode(nodes), [nodes])

  // Client-side filter by name + selected source.
  const visibleNodes = useMemo(() => {
    const q = query.trim().toLowerCase()
    return nodes.filter((n) => {
      if (q && !n.name.toLowerCase().includes(q)) return false
      if (source !== SOURCE_ALL && !n.sources.includes(source)) return false
      return true
    })
  }, [nodes, query, source])

  const testedTotal = useMemo(() => nodes.filter((n) => n.tested).length, [nodes])

  const handleTestAll = async () => {
    try {
      await api.testAllNodes(source === SOURCE_ALL ? undefined : source)
      const scope = source === SOURCE_ALL ? '全部节点' : `「${sourceLabel(source)}」`
      toast.success(`已开始为${scope}测速，结果将陆续更新`)
      setTestingAll(true)
      if (pollRef.current) window.clearInterval(pollRef.current)
      let ticks = 0
      pollRef.current = window.setInterval(async () => {
        ticks++
        await load()
        if (ticks >= 15) {
          if (pollRef.current) window.clearInterval(pollRef.current)
          pollRef.current = null
          setTestingAll(false)
        }
      }, 2000)
    } catch (err) {
      toast.error(errMessage(err, '测速失败'))
    }
  }

  const handleTest = async (node: Node) => {
    setTesting((t) => ({ ...t, [node.id]: true }))
    try {
      const res = await api.testNode(node.id)
      setNodes((prev) =>
        prev.map((n) =>
          n.id === node.id
            ? { ...n, tested: true, alive: res.ok, delay_ms: res.ok ? res.delay_ms : undefined }
            : n,
        ),
      )
      toast[res.ok ? 'success' : 'error'](
        res.ok ? `「${node.name}」延迟 ${res.delay_ms} ms` : `「${node.name}」不可用`,
      )
    } catch (err) {
      toast.error(errMessage(err, '测速失败'))
    } finally {
      setTesting((t) => ({ ...t, [node.id]: false }))
    }
  }

  const handleDelete = async () => {
    if (!deleting) return
    setDeleteLoading(true)
    try {
      await api.deleteNode(deleting.id)
      toast.success(`已删除「${deleting.name}」`)
      setDeleting(null)
      await load()
    } catch (err) {
      toast.error(errMessage(err, '删除失败'))
    } finally {
      setDeleteLoading(false)
    }
  }

  const filtering = query.trim() !== '' || source !== SOURCE_ALL

  return (
    <div>
      <PageHeader
        title="节点列表"
        description={`共 ${subs.length} 个订阅，合计 ${nodes.length} 个节点${
          testedTotal ? `，已测速 ${testedTotal} 个` : ''
        }`}
        actions={
          <>
            <Button variant="outline" onClick={handleTestAll} loading={testingAll}>
              {!testingAll && <Zap className="h-4 w-4" />}
              全部测速
            </Button>
            <Button onClick={() => setAddOpen(true)}>
              <Plus className="h-4 w-4" />
              添加节点
            </Button>
          </>
        }
      />

      <div className="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center">
        <Select value={source} onValueChange={setSource}>
          <SelectTrigger className="w-full sm:w-56">
            <SelectValue placeholder="按订阅筛选" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={SOURCE_ALL}>全部订阅</SelectItem>
            {subs.map((s) => (
              <SelectItem key={s.id} value={s.id}>
                {s.name}
              </SelectItem>
            ))}
            {hasManual && <SelectItem value={SOURCE_MANUAL}>手动添加</SelectItem>}
          </SelectContent>
        </Select>
        <Input
          placeholder="按名称模糊过滤…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="w-full sm:max-w-sm"
        />
      </div>

      <Card>
        {loading ? (
          <CenteredSpinner label="加载节点…" />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : visibleNodes.length === 0 ? (
          <EmptyState
            icon={<Server className="h-10 w-10" />}
            title={filtering ? '没有匹配的节点' : '暂无节点'}
            description={filtering ? '换个筛选条件试试' : '添加订阅或手动添加节点'}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>来源</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>协议</TableHead>
                <TableHead>地区</TableHead>
                <TableHead>服务器</TableHead>
                <TableHead>延迟</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleNodes.map((node) => (
                <TableRow key={node.id}>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {node.sources.map((src) => (
                        <Badge key={src} variant="muted" className="max-w-[8rem] truncate">
                          {sourceLabel(src)}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="max-w-[16rem]">
                    <span className="block truncate font-medium">{node.name}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline" className="uppercase">
                      {node.protocol || '未知'}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {node.region ? (
                      <Badge variant="secondary">{node.region}</Badge>
                    ) : (
                      <span className="text-xs text-muted-foreground">未知</span>
                    )}
                  </TableCell>
                  <TableCell className="max-w-[12rem]">
                    <span className="block truncate font-mono text-xs text-muted-foreground">
                      {node.server}
                    </span>
                  </TableCell>
                  <TableCell>
                    <DelayCell node={node} />
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleTest(node)}
                        disabled={testing[node.id]}
                      >
                        {testing[node.id] ? <Spinner /> : <Gauge className="h-4 w-4" />}
                        测速
                      </Button>
                      {isManualOnly(node) && (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          className="text-destructive"
                          onClick={() => setDeleting(node)}
                          aria-label="删除"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <AddNodeDialog open={addOpen} onOpenChange={setAddOpen} onSaved={load} />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除节点「${deleting?.name}」？`}
        description="仅手动添加的节点可删除，此操作不可撤销。"
        confirmText="删除"
        destructive
        loading={deleteLoading}
        onConfirm={handleDelete}
      />
    </div>
  )
}
