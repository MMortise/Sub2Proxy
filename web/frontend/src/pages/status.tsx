import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Activity,
  ArrowDownToLine,
  ArrowUpFromLine,
  CheckCircle2,
  Link2,
  XCircle,
} from 'lucide-react'
import { api, ApiError, errMessage } from '@/lib/api'
import type { Mapping, Status } from '@/lib/types'
import { formatRate, formatTime } from '@/lib/utils'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { CenteredSpinner } from '@/components/ui/spinner'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { PageHeader } from '@/components/page-header'

const POLL_MS = 2000

function StatTile({
  icon,
  label,
  value,
  sub,
}: {
  icon: React.ReactNode
  label: string
  value: string
  sub?: string
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-4 p-5">
        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-secondary text-secondary-foreground">
          {icon}
        </div>
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className="truncate text-xl font-semibold tabular-nums">{value}</p>
          {sub && <p className="text-xs text-muted-foreground">{sub}</p>}
        </div>
      </CardContent>
    </Card>
  )
}

export function StatusPage() {
  const [status, setStatus] = useState<Status | null>(null)
  const [nameByPort, setNameByPort] = useState<Record<number, string>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const timer = useRef<number | null>(null)

  const poll = useCallback(async () => {
    try {
      const s = await api.status()
      setStatus(s)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return
      setError(errMessage(err, '加载失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    poll()
    timer.current = window.setInterval(poll, POLL_MS)
    api
      .listMappings()
      .then((ms: Mapping[]) => {
        const map: Record<number, string> = {}
        for (const m of ms) map[m.port] = m.name
        setNameByPort(map)
      })
      .catch(() => {})
    return () => {
      if (timer.current) window.clearInterval(timer.current)
    }
  }, [poll])

  const hasError = !!status?.last_error

  return (
    <div>
      <PageHeader title="运行状态" description="引擎与各端口的实时流量（每 2 秒刷新）" />

      {loading && !status ? (
        <CenteredSpinner label="加载状态…" />
      ) : error && !status ? (
        <ErrorState message={error} onRetry={poll} />
      ) : status ? (
        <div className="space-y-6">
          {/* Engine status */}
          <Card
            className={
              hasError
                ? 'border-destructive/50 bg-destructive/5'
                : 'border-success/40 bg-success/5'
            }
          >
            <CardContent className="flex items-start gap-4 p-5">
              {hasError ? (
                <XCircle className="mt-0.5 h-6 w-6 shrink-0 text-destructive" />
              ) : (
                <CheckCircle2 className="mt-0.5 h-6 w-6 shrink-0 text-success" />
              )}
              <div className="min-w-0 flex-1">
                <p className="font-medium">
                  {hasError ? '引擎存在错误' : '引擎运行正常'}
                </p>
                {hasError ? (
                  <>
                    <p className="mt-1 break-all text-sm text-destructive">
                      {status.last_error}
                    </p>
                    {status.last_error_at && (
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        发生于 {formatTime(status.last_error_at)}
                      </p>
                    )}
                  </>
                ) : (
                  <p className="mt-1 text-sm text-muted-foreground">
                    配置已加载，代理引擎无报错
                  </p>
                )}
              </div>
            </CardContent>
          </Card>

          {/* Global rates */}
          <div className="grid gap-4 sm:grid-cols-3">
            <StatTile
              icon={<ArrowUpFromLine className="h-5 w-5" />}
              label="总上行速率"
              value={formatRate(status.total_up_rate)}
            />
            <StatTile
              icon={<ArrowDownToLine className="h-5 w-5" />}
              label="总下行速率"
              value={formatRate(status.total_down_rate)}
            />
            <StatTile
              icon={<Activity className="h-5 w-5" />}
              label="活跃映射"
              value={String(status.mappings.length)}
              sub="个端口正在运行"
            />
          </div>

          {/* Per-mapping cards */}
          <div>
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">端口明细</h2>
            {status.mappings.length === 0 ? (
              <Card>
                <EmptyState
                  icon={<Activity className="h-10 w-10" />}
                  title="暂无运行中的映射"
                  description="启用端口映射后将在此显示实时流量"
                />
              </Card>
            ) : (
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {status.mappings.map((ms) => (
                  <Card key={ms.port}>
                    <CardHeader className="pb-3">
                      <CardTitle className="flex items-center justify-between text-base">
                        <span className="font-mono tabular-nums">:{ms.port}</span>
                        <span className="text-xs font-normal text-muted-foreground">
                          {nameByPort[ms.port] ?? '—'}
                        </span>
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2.5 pt-0 text-sm">
                      <div className="flex items-center gap-2 text-muted-foreground">
                        <Link2 className="h-4 w-4 shrink-0" />
                        <span className="truncate text-foreground">
                          {ms.active_node || '无活跃节点'}
                        </span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-muted-foreground">连接数</span>
                        <span className="tabular-nums">{ms.connections}</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="inline-flex items-center gap-1 text-muted-foreground">
                          <ArrowUpFromLine className="h-3.5 w-3.5" /> 上行
                        </span>
                        <span className="tabular-nums">{formatRate(ms.up_rate)}</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="inline-flex items-center gap-1 text-muted-foreground">
                          <ArrowDownToLine className="h-3.5 w-3.5" /> 下行
                        </span>
                        <span className="tabular-nums">{formatRate(ms.down_rate)}</span>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : null}
    </div>
  )
}
