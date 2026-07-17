import { useCallback, useEffect, useState } from 'react'
import { AlertTriangle, Network, Pencil, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api, ApiError, errMessage } from '@/lib/api'
import { STRATEGY_LABEL, type Mapping, type Node } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { CenteredSpinner } from '@/components/ui/spinner'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { PageHeader } from '@/components/page-header'
import { MappingDialog } from '@/components/mapping-dialog'

export function MappingsPage() {
  const [mappings, setMappings] = useState<Mapping[]>([])
  const [nodes, setNodes] = useState<Node[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<Mapping | null>(null)
  const [toggling, setToggling] = useState<Record<number, boolean>>({})
  const [deleting, setDeleting] = useState<Mapping | null>(null)
  const [deleteLoading, setDeleteLoading] = useState(false)

  const load = useCallback(async () => {
    try {
      const data = await api.listMappings()
      setMappings(data)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      setError(errMessage(err, '加载失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  const refreshNodes = useCallback(
    () => api.listNodes().then(setNodes).catch(() => {}),
    [],
  )

  useEffect(() => {
    load()
    refreshNodes()
  }, [load, refreshNodes])

  const openCreate = () => {
    setEditing(null)
    setDialogOpen(true)
    refreshNodes()
  }

  const openEdit = (m: Mapping) => {
    setEditing(m)
    setDialogOpen(true)
    refreshNodes()
  }

  const handleToggle = async (m: Mapping, next: boolean) => {
    setToggling((t) => ({ ...t, [m.port]: true }))
    try {
      if (next) await api.enableMapping(m.port)
      else await api.disableMapping(m.port)
      toast.success(next ? `已启用端口 ${m.port}` : `已停用端口 ${m.port}`)
      await load()
    } catch (err) {
      toast.error(errMessage(err, '操作失败'))
    } finally {
      setToggling((t) => ({ ...t, [m.port]: false }))
    }
  }

  const handleDelete = async () => {
    if (!deleting) return
    setDeleteLoading(true)
    try {
      await api.deleteMapping(deleting.port)
      toast.success(`已删除端口 ${deleting.port} 的映射`)
      setDeleting(null)
      await load()
    } catch (err) {
      toast.error(errMessage(err, '删除失败'))
    } finally {
      setDeleteLoading(false)
    }
  }

  return (
    <div>
      <PageHeader
        title="端口映射"
        description="将本地端口绑定到节点组，按策略转发流量"
        actions={
          <Button onClick={openCreate}>
            <Plus className="h-4 w-4" />
            创建映射
          </Button>
        }
      />

      <Card>
        {loading ? (
          <CenteredSpinner label="加载映射…" />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : mappings.length === 0 ? (
          <EmptyState
            icon={<Network className="h-10 w-10" />}
            title="暂无映射"
            description="创建一个端口映射以对外提供代理"
            action={
              <Button onClick={openCreate}>
                <Plus className="h-4 w-4" />
                创建映射
              </Button>
            }
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>端口</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>策略</TableHead>
                <TableHead>当前活跃节点</TableHead>
                <TableHead>启用</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {mappings.map((m) => {
                const problem = !!m.disabled_reason
                return (
                  <TableRow key={m.port} className={cn(problem && 'bg-destructive/5')}>
                    <TableCell className="font-mono font-medium tabular-nums">{m.port}</TableCell>
                    <TableCell className="font-medium">{m.name || '-'}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{STRATEGY_LABEL[m.strategy] ?? m.strategy}</Badge>
                    </TableCell>
                    <TableCell>
                      {m.active_node ? (
                        <span className="max-w-[14rem] truncate text-sm">{m.active_node}</span>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={m.enabled}
                        disabled={toggling[m.port]}
                        onCheckedChange={(v) => handleToggle(m, v)}
                        aria-label="启用/停用"
                      />
                    </TableCell>
                    <TableCell>
                      {problem ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="inline-flex items-center gap-1 text-xs font-medium text-destructive">
                              <AlertTriangle className="h-3.5 w-3.5" />
                              异常
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>{m.disabled_reason}</TooltipContent>
                        </Tooltip>
                      ) : m.enabled ? (
                        <span className="text-xs text-success">运行中</span>
                      ) : (
                        <span className="text-xs text-muted-foreground">已停用</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => openEdit(m)}
                          aria-label="编辑"
                        >
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          className="text-destructive"
                          onClick={() => setDeleting(m)}
                          aria-label="删除"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </Card>

      <MappingDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editing={editing}
        nodes={nodes}
        onSaved={load}
      />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除端口 ${deleting?.port} 的映射？`}
        description="该端口将被释放，此操作不可撤销。"
        confirmText="删除"
        destructive
        loading={deleteLoading}
        onConfirm={handleDelete}
      />
    </div>
  )
}
