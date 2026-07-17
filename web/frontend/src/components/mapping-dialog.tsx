import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { toast } from 'sonner'
import { api, errMessage } from '@/lib/api'
import {
  STRATEGY_LABEL,
  STRATEGY_OPTIONS,
  SOURCE_ALL,
  SOURCE_MANUAL,
  hasManualNode,
  type Mapping,
  type MappingInput,
  type Node,
  type NodeRef,
  type Strategy,
  type Subscription,
} from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { NodeMultiSelect } from '@/components/node-multi-select'

type NodeMode = 'nodes' | 'filter'

interface MappingDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing: Mapping | null
  nodes: Node[]
  onSaved: () => void
}

const DEFAULT_HC_URL = 'http://www.gstatic.com/generate_204'
const DEFAULT_HC_INTERVAL = 300

export function MappingDialog({ open, onOpenChange, editing, nodes, onSaved }: MappingDialogProps) {
  const [name, setName] = useState('')
  const [port, setPort] = useState('')
  const [strategy, setStrategy] = useState<Strategy>('single')
  const [mode, setMode] = useState<NodeMode>('nodes')
  const [subs, setSubs] = useState<Subscription[]>([])
  const [subFilter, setSubFilter] = useState<string>(SOURCE_ALL)
  const [selected, setSelected] = useState<NodeRef[]>([])
  const [filter, setFilter] = useState('')
  const [hcUrl, setHcUrl] = useState(DEFAULT_HC_URL)
  const [hcInterval, setHcInterval] = useState(DEFAULT_HC_INTERVAL)
  const [hcOpen, setHcOpen] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(editing?.name ?? '')
    setPort(editing ? String(editing.port) : '')
    setStrategy(editing?.strategy ?? 'single')
    if (editing?.node_filter) {
      setMode('filter')
      setFilter(editing.node_filter)
      setSelected([])
    } else {
      setMode('nodes')
      setFilter('')
      setSelected(editing?.nodes ?? [])
    }
    setHcUrl(editing?.health_check?.url ?? DEFAULT_HC_URL)
    setHcInterval(editing?.health_check?.interval ?? DEFAULT_HC_INTERVAL)
    setHcOpen(false)
    setUsername(editing?.username ?? '')
    setPassword(editing?.password ?? '')
    setSubFilter(SOURCE_ALL)
    api.listSubscriptions().then(setSubs).catch(() => {})
  }, [open, editing])

  const isSingle = strategy === 'single'
  const isFailover = strategy === 'failover'

  // The subscription filter narrows which nodes appear in the picker below;
  // already-selected nodes persist across filter changes, so one mapping can
  // combine nodes from different subscriptions.
  const hasManual = useMemo(() => hasManualNode(nodes), [nodes])
  const visibleNodes = useMemo(
    () => (subFilter === SOURCE_ALL ? nodes : nodes.filter((n) => n.sources.includes(subFilter))),
    [nodes, subFilter],
  )

  // Keep only the first node when switching to single.
  useEffect(() => {
    if (isSingle && selected.length > 1) setSelected(selected.slice(0, 1))
  }, [isSingle, selected])

  // Live regex preview over loaded nodes.
  const filterPreview = useMemo(() => {
    if (mode !== 'filter' || !filter.trim()) return { ok: true, matches: [] as Node[], error: '' }
    try {
      const re = new RegExp(filter)
      return { ok: true, matches: nodes.filter((n) => re.test(n.name)), error: '' }
    } catch (e) {
      return { ok: false, matches: [] as Node[], error: (e as Error).message }
    }
  }, [mode, filter, nodes])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedName = name.trim()
    if (!trimmedName) {
      toast.error('请输入名称')
      return
    }
    let portNum: number | undefined
    if (port.trim()) {
      const p = Number(port.trim())
      if (!Number.isInteger(p) || p <= 0) {
        toast.error('端口必须为正整数')
        return
      }
      portNum = p
    }

    const payload: MappingInput = {
      name: trimmedName,
      strategy,
      enabled: editing ? editing.enabled : true,
    }
    if (portNum !== undefined) payload.port = portNum

    if (mode === 'filter') {
      if (!filter.trim()) {
        toast.error('请输入过滤正则')
        return
      }
      if (!filterPreview.ok) {
        toast.error('正则表达式无效')
        return
      }
      payload.node_filter = filter.trim()
    } else {
      if (selected.length === 0) {
        toast.error('请至少选择一个节点')
        return
      }
      payload.nodes = selected
    }

    if (!isSingle) {
      payload.health_check = { url: hcUrl.trim() || DEFAULT_HC_URL, interval: hcInterval }
    }

    const user = username.trim()
    if (user || password) {
      if (!user || !password) {
        toast.error('用户名和密码需同时填写')
        return
      }
      if (!/^[A-Za-z0-9_-]{1,32}$/.test(user)) {
        toast.error('用户名只能含字母、数字、_ 和 -，且不超过 32 位')
        return
      }
      if ([...password].length > 32) {
        toast.error('密码不超过 32 位')
        return
      }
      payload.username = user
      payload.password = password
    }

    setSaving(true)
    try {
      if (editing) {
        await api.updateMapping(editing.port, payload)
        toast.success('映射已更新')
      } else {
        const created = await api.createMapping(payload)
        toast.success(`映射已创建，端口 ${created.port}`)
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{editing ? `编辑映射（端口 ${editing.port}）` : '创建映射'}</DialogTitle>
          <DialogDescription>将本地端口绑定到一组节点，并选择转发策略。</DialogDescription>
        </DialogHeader>

        <form onSubmit={submit} className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="m-name">名称</Label>
              <Input
                id="m-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="美国节点组"
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="m-port">端口</Label>
              <Input
                id="m-port"
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="留空自动分配"
                inputMode="numeric"
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label>策略</Label>
            <Select value={strategy} onValueChange={(v) => setStrategy(v as Strategy)}>
              <SelectTrigger>
                {/* Closed trigger shows only the strategy title; the dropdown
                    items below carry title + subtitle. */}
                <span>{STRATEGY_LABEL[strategy]}</span>
              </SelectTrigger>
              <SelectContent>
                {STRATEGY_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    <div className="flex flex-col">
                      <span>{opt.label}</span>
                      <span className="text-xs text-muted-foreground">{opt.desc}</span>
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-3">
            <Label>节点集</Label>
            <RadioGroup
              value={mode}
              onValueChange={(v) => setMode(v as NodeMode)}
              className="flex gap-6"
            >
              <label className="flex cursor-pointer items-center gap-2 text-sm">
                <RadioGroupItem value="nodes" id="mode-nodes" />
                指定节点
              </label>
              <label className="flex cursor-pointer items-center gap-2 text-sm">
                <RadioGroupItem value="filter" id="mode-filter" />
                过滤器
              </label>
            </RadioGroup>

            {mode === 'nodes' ? (
              <div className="space-y-2">
                <div className="space-y-1.5">
                  <Label className="text-xs font-normal text-muted-foreground">订阅集</Label>
                  <Select value={subFilter} onValueChange={setSubFilter}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
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
                </div>
                <NodeMultiSelect
                  nodes={visibleNodes}
                  value={selected}
                  onChange={setSelected}
                  single={isSingle}
                  reorderable={isFailover}
                />
              </div>
            ) : (
              <div className="space-y-2">
                <Input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="正则，如 香港|HK|新加坡"
                  className={cn('font-mono text-sm', !filterPreview.ok && 'border-destructive')}
                />
                {!filterPreview.ok ? (
                  <p className="text-xs text-destructive">正则无效：{filterPreview.error}</p>
                ) : filter.trim() ? (
                  <div className="rounded-md border border-border p-2">
                    <p className="mb-1.5 text-xs text-muted-foreground">
                      当前匹配 {filterPreview.matches.length} 个节点
                    </p>
                    <div className="flex max-h-28 flex-wrap gap-1 overflow-y-auto">
                      {filterPreview.matches.slice(0, 60).map((n) => (
                        <Badge key={n.id} variant="muted" className="max-w-[12rem] truncate">
                          {n.name}
                        </Badge>
                      ))}
                      {filterPreview.matches.length > 60 && (
                        <Badge variant="outline">…{filterPreview.matches.length - 60} 更多</Badge>
                      )}
                    </div>
                  </div>
                ) : null}
              </div>
            )}
          </div>

          {!isSingle && (
            <div className="rounded-md border border-border">
              <button
                type="button"
                onClick={() => setHcOpen((o) => !o)}
                className="flex w-full items-center gap-2 px-3 py-2 text-sm font-medium"
              >
                {hcOpen ? (
                  <ChevronDown className="h-4 w-4" />
                ) : (
                  <ChevronRight className="h-4 w-4" />
                )}
                健康检查
                <span className="ml-auto text-xs font-normal text-muted-foreground">
                  每 {hcInterval}s
                </span>
              </button>
              {hcOpen && (
                <div className="grid gap-4 border-t border-border p-3 sm:grid-cols-2">
                  <div className="space-y-2 sm:col-span-2">
                    <Label htmlFor="hc-url">检查 URL</Label>
                    <Input
                      id="hc-url"
                      value={hcUrl}
                      onChange={(e) => setHcUrl(e.target.value)}
                      placeholder={DEFAULT_HC_URL}
                      className="font-mono text-xs"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="hc-interval">检查间隔（秒）</Label>
                    <Input
                      id="hc-interval"
                      type="number"
                      min={30}
                      max={3600}
                      value={hcInterval}
                      onChange={(e) => setHcInterval(Number(e.target.value))}
                    />
                  </div>
                </div>
              )}
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="m-user">用户名（选填）</Label>
              <Input
                id="m-user"
                value={username}
                maxLength={32}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="留空则无需认证"
                autoComplete="off"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="m-pass">密码（选填）</Label>
              <Input
                id="m-pass"
                type="password"
                value={password}
                maxLength={32}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="设置后连接需账号密码"
                autoComplete="new-password"
              />
            </div>
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
