import type {
  Health,
  Mapping,
  MappingInput,
  Node,
  PortRange,
  Status,
  Subscription,
  SubscriptionInput,
  TestResult,
} from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = 'ApiError'
  }
}

// errMessage extracts a user-facing message from a thrown value, falling back to
// the given default for non-ApiError throws. Used for toast/error text everywhere.
export function errMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

// A global handler invoked whenever the API returns 401. The router registers it
// so any unauthorized response redirects to the login page.
let unauthorizedHandler: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null) {
  unauthorizedHandler = fn
}

type RequestOptions = {
  method?: string
  body?: unknown
  // When true, a 401 does NOT trigger the global redirect (used by the login call).
  silent401?: boolean
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, silent401 = false } = opts
  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError(0, '网络错误，请检查连接')
  }

  if (res.status === 401 && !silent401) {
    unauthorizedHandler?.()
    throw new ApiError(401, '未授权，请重新登录')
  }

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }

  if (!res.ok) {
    const msg =
      (data && typeof data === 'object' && 'error' in data
        ? String((data as { error: unknown }).error)
        : '') || `请求失败 (${res.status})`
    throw new ApiError(res.status, msg)
  }

  return data as T
}

export const api = {
  // --- auth ---
  health: () => request<Health>('/api/health'),
  login: (key: string) =>
    request<{ ok: true }>('/api/login', { method: 'POST', body: { key }, silent401: true }),
  logout: () => request<{ ok: true }>('/api/logout', { method: 'POST' }),

  // --- subscriptions ---
  listSubscriptions: () => request<Subscription[]>('/api/subscriptions'),
  createSubscription: (input: SubscriptionInput) =>
    request<{ subscription: Subscription; node_count: number }>('/api/subscriptions', {
      method: 'POST',
      body: input,
    }),
  updateSubscription: (id: string, input: SubscriptionInput) =>
    request<Subscription>(`/api/subscriptions/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: input,
    }),
  deleteSubscription: (id: string) =>
    request<{ ok: true }>(`/api/subscriptions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  refreshSubscription: (id: string) =>
    request<{ node_count: number }>(`/api/subscriptions/${encodeURIComponent(id)}/refresh`, {
      method: 'POST',
    }),

  // --- nodes ---
  listNodes: () => request<Node[]>('/api/nodes'),
  addNode: (link: string) => request<Node>('/api/nodes', { method: 'POST', body: { link } }),
  deleteNode: (id: string) =>
    request<{ ok: true }>(`/api/nodes/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testNode: (id: string) =>
    request<TestResult>(`/api/nodes/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  testAllNodes: (source?: string) =>
    request<{ started: true }>(
      `/api/nodes/test-all${source ? `?source=${encodeURIComponent(source)}` : ''}`,
      { method: 'POST' },
    ),

  // --- mappings ---
  listMappings: () => request<Mapping[]>('/api/mappings'),
  mappingPortRange: () => request<PortRange>('/api/mappings/port-range'),
  createMapping: (input: MappingInput) =>
    request<Mapping>('/api/mappings', { method: 'POST', body: input }),
  updateMapping: (port: number, input: MappingInput) =>
    request<Mapping>(`/api/mappings/${port}`, { method: 'PUT', body: input }),
  deleteMapping: (port: number) =>
    request<{ ok: true }>(`/api/mappings/${port}`, { method: 'DELETE' }),
  enableMapping: (port: number) =>
    request<{ ok: true }>(`/api/mappings/${port}/enable`, { method: 'POST' }),
  disableMapping: (port: number) =>
    request<{ ok: true }>(`/api/mappings/${port}/disable`, { method: 'POST' }),

  // --- status ---
  status: () => request<Status>('/api/status'),
}
