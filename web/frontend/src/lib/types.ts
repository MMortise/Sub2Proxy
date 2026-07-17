// Types mirror the backend API contract (see internal/model and internal/engine).

export type Quota = {
  upload: number
  download: number
  total: number
  expire: number // Unix seconds
}

export type Subscription = {
  id: string
  name: string
  url: string
  user_agent?: string
  refresh_interval?: string
  node_count: number
  last_refresh?: string
  last_error?: string
  quota?: Quota
}

export type Node = {
  id: string
  name: string
  protocol: string
  server: string
  region: string
  sources: string[]
  delay_ms?: number
  tested: boolean
  alive: boolean
}

export type NodeRef = {
  id: string
  name: string
}

export type HealthCheck = {
  url: string
  interval: number
}

export type Strategy =
  | 'single'
  | 'failover'
  | 'round-robin'
  | 'hash'
  | 'sticky'
  | 'auto'

export type Mapping = {
  port: number
  name: string
  strategy: Strategy
  nodes?: NodeRef[]
  node_filter?: string
  health_check?: HealthCheck
  enabled: boolean
  disabled_reason?: string
  active_node?: string
  username?: string
  password?: string
}

export type MappingStatus = {
  port: number
  active_node: string
  connections: number
  up_rate: number
  down_rate: number
}

export type Status = {
  mappings: MappingStatus[]
  last_error?: string
  last_error_at?: string
  total_up_rate: number
  total_down_rate: number
}

export type Health = {
  status: string
  version: string
}

// Payloads
export type SubscriptionInput = {
  name: string
  url: string
  user_agent?: string
  refresh_interval?: string
}

export type MappingInput = {
  port?: number
  name: string
  strategy: Strategy
  nodes?: NodeRef[]
  node_filter?: string
  health_check?: HealthCheck
  enabled?: boolean
  username?: string
  password?: string
}

export type TestResult = {
  delay_ms: number
  ok: boolean
  tested_at: string
}

export const STRATEGY_OPTIONS: { value: Strategy; label: string; desc: string }[] = [
  { value: 'single', label: '固定单节点', desc: 'single：固定使用单个节点' },
  { value: 'failover', label: '故障转移', desc: 'failover：按顺序故障转移，前一个失败切下一个' },
  { value: 'round-robin', label: '轮询', desc: 'round-robin：每个连接轮流使用不同节点' },
  { value: 'hash', label: '散列', desc: 'hash：按目标散列（近似随机、同站点固定同节点）' },
  { value: 'sticky', label: '会话粘滞', desc: 'sticky：同一会话粘滞在同一节点' },
  { value: 'auto', label: '自动择优', desc: 'auto：自动选择延迟最低的节点' },
]

export const STRATEGY_LABEL: Record<Strategy, string> = Object.fromEntries(
  STRATEGY_OPTIONS.map((o) => [o.value, o.label]),
) as Record<Strategy, string>

// Reserved node-source tokens. SOURCE_ALL is the UI-only "no filter" sentinel;
// SOURCE_MANUAL marks manually added nodes (mirrors internal/model.SourceManual).
export const SOURCE_ALL = 'all'
export const SOURCE_MANUAL = 'manual'

// hasManualNode reports whether any node was added manually — used to decide
// whether to offer the "手动添加" option in source filters.
export const hasManualNode = (nodes: Node[]) =>
  nodes.some((n) => n.sources.includes(SOURCE_MANUAL))
