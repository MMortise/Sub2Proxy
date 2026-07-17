import { useCallback, useEffect, useState } from 'react'
import { AlertCircle, Pencil, Plus, RefreshCw, Rss, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api, ApiError, errMessage } from '@/lib/api'
import type { Subscription, SubscriptionInput } from '@/lib/types'
import { formatBytes, formatExpire, formatRelative, truncateMiddle } from '@/lib/utils'
import { Button } from '@/components/ui/button'
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
import { Progress } from '@/components/ui/progress'
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

function QuotaCell({ sub }: { sub: Subscription }) {
  const q = sub.quota
  if (!q || q.total <= 0) return <span className="text-muted-foreground">-</span>
  const used = q.upload + q.download
  const pct = (used / q.total) * 100
  const danger = pct >= 90
  return (
    <div className="w-40 space-y-1">
      <Progress
        value={pct}
        indicatorClassName={danger ? 'bg-destructive' : 'bg-primary'}
      />
      <div className="flex justify-between text-xs text-muted-foreground">
        <span>
          {formatBytes(used)} / {formatBytes(q.total)}
        </span>
        <span>{pct.toFixed(0)}%</span>
      </div>
      <div className="text-xs text-muted-foreground">到期 {formatExpire(q.expire)}</div>
    </div>
  )
}

interface DialogState {
  open: boolean
  editing: Subscription | null
}

function SubscriptionDialog({
  state,
  onOpenChange,
  onSaved,
}: {
  state: DialogState
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const editing = state.editing
  const [form, setForm] = useState<SubscriptionInput>({
    name: '',
    url: '',
    user_agent: '',
    refresh_interval: '',
  })
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (state.open) {
      setForm({
        name: editing?.name ?? '',
        url: editing?.url ?? '',
        user_agent: editing?.user_agent ?? '',
        refresh_interval: editing?.refresh_interval ?? '',
      })
    }
  }, [state.open, editing])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const payload: SubscriptionInput = {
      name: form.name.trim(),
      url: form.url.trim(),
      user_agent: form.user_agent?.trim() || undefined,
      refresh_interval: form.refresh_interval?.trim() || undefined,
    }
    setSaving(true)
    try {
      if (editing) {
        await api.updateSubscription(editing.id, payload)
        toast.success('订阅已更新')
      } else {
        const res = await api.createSubscription(payload)
        toast.success(`订阅已添加，解析到 ${res.node_count} 个节点`)
      }
      onOpenChange(false)
      onSaved()
    } catch (err) {
      toast.error(errMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={state.open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? '编辑订阅' : '添加订阅'}</DialogTitle>
          <DialogDescription>配置机场订阅地址，保存后将自动拉取节点。</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="sub-name">名称</Label>
            <Input
              id="sub-name"
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="我的机场"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="sub-url">订阅 URL</Label>
            <Input
              id="sub-url"
              value={form.url}
              onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
              placeholder="https://example.com/subscribe?token=..."
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="sub-ua">User-Agent（可选）</Label>
            <Input
              id="sub-ua"
              value={form.user_agent}
              onChange={(e) => setForm((f) => ({ ...f, user_agent: e.target.value }))}
              placeholder="clash.meta"
            />
            <p className="text-xs text-muted-foreground">留空默认使用 clash.meta</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="sub-interval">刷新间隔（可选）</Label>
            <Input
              id="sub-interval"
              value={form.refresh_interval}
              onChange={(e) => setForm((f) => ({ ...f, refresh_interval: e.target.value }))}
              placeholder="6h"
            />
            <p className="text-xs text-muted-foreground">
              形如 30m、6h、24h，范围 5m–24h，留空默认 6h
            </p>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" loading={saving}>
              保存
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function SubscriptionsPage() {
  const [subs, setSubs] = useState<Subscription[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [dialog, setDialog] = useState<DialogState>({ open: false, editing: null })
  const [refreshing, setRefreshing] = useState<Record<string, boolean>>({})
  const [deleting, setDeleting] = useState<Subscription | null>(null)
  const [deleteLoading, setDeleteLoading] = useState(false)

  const load = useCallback(async () => {
    try {
      const data = await api.listSubscriptions()
      setSubs(data)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      setError(errMessage(err, '加载失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const handleRefresh = async (sub: Subscription) => {
    setRefreshing((r) => ({ ...r, [sub.id]: true }))
    try {
      const res = await api.refreshSubscription(sub.id)
      toast.success(`已刷新「${sub.name}」，${res.node_count} 个节点`)
      await load()
    } catch (err) {
      toast.error(errMessage(err, '刷新失败'))
    } finally {
      setRefreshing((r) => ({ ...r, [sub.id]: false }))
    }
  }

  const handleDelete = async () => {
    if (!deleting) return
    setDeleteLoading(true)
    try {
      await api.deleteSubscription(deleting.id)
      toast.success(`已删除「${deleting.name}」`)
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
        title="订阅管理"
        description="管理机场订阅，自动拉取并去重节点"
        actions={
          <Button onClick={() => setDialog({ open: true, editing: null })}>
            <Plus className="h-4 w-4" />
            添加订阅
          </Button>
        }
      />

      <Card>
        {loading ? (
          <CenteredSpinner label="加载订阅…" />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : subs.length === 0 ? (
          <EmptyState
            icon={<Rss className="h-10 w-10" />}
            title="暂无订阅"
            description="添加一个机场订阅以开始拉取节点"
            action={
              <Button onClick={() => setDialog({ open: true, editing: null })}>
                <Plus className="h-4 w-4" />
                添加订阅
              </Button>
            }
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>URL</TableHead>
                <TableHead className="text-right">节点数</TableHead>
                <TableHead>最近刷新</TableHead>
                <TableHead>配额</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {subs.map((sub) => (
                <TableRow key={sub.id}>
                  <TableCell className="font-medium">{sub.name}</TableCell>
                  <TableCell className="max-w-xs">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className="block truncate font-mono text-xs text-muted-foreground">
                          {truncateMiddle(sub.url, 40)}
                        </span>
                      </TooltipTrigger>
                      <TooltipContent className="break-all">{sub.url}</TooltipContent>
                    </Tooltip>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{sub.node_count}</TableCell>
                  <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                    {formatRelative(sub.last_refresh)}
                  </TableCell>
                  <TableCell>
                    <QuotaCell sub={sub} />
                  </TableCell>
                  <TableCell>
                    {sub.last_error ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="inline-flex items-center gap-1 text-xs text-destructive">
                            <AlertCircle className="h-3.5 w-3.5" />
                            错误
                          </span>
                        </TooltipTrigger>
                        <TooltipContent className="break-all">{sub.last_error}</TooltipContent>
                      </Tooltip>
                    ) : (
                      <span className="text-xs text-success">正常</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => handleRefresh(sub)}
                            loading={refreshing[sub.id]}
                            aria-label="刷新"
                          >
                            {!refreshing[sub.id] && <RefreshCw className="h-4 w-4" />}
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>手动刷新</TooltipContent>
                      </Tooltip>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => setDialog({ open: true, editing: sub })}
                        aria-label="编辑"
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="text-destructive"
                        onClick={() => setDeleting(sub)}
                        aria-label="删除"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <SubscriptionDialog
        state={dialog}
        onOpenChange={(open) => setDialog((d) => ({ ...d, open }))}
        onSaved={load}
      />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除订阅「${deleting?.name}」？`}
        description={
          <span>
            将移除该订阅拉取的节点。若有端口映射正在使用这些节点，可能会受到影响。此操作不可撤销。
          </span>
        }
        confirmText="删除"
        destructive
        loading={deleteLoading}
        onConfirm={handleDelete}
      />
    </div>
  )
}
