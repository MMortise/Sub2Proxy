## Context

订阅拉取目前由 `internal/subscribe.Fetcher` 使用单一共享 `http.Client` 直连完成；`model.Subscription` 仅有 `name` / `url` / `user_agent` / `refresh_interval`。部分部署无法直连订阅源，需要在控制面为每条订阅配置可选的 HTTP 拉取代理。数据面（mihomo 节点出站、端口映射）不在本变更范围内。

约束：与现有失败语义对齐（拉取失败保留节点池）；配置 0600 已含订阅 token，代理凭证同级敏感；不引入新外部依赖（标准库 `net/http` 即可）。

## Goals / Non-Goals

**Goals:**

- 每条订阅可选 `fetch_proxy`（HTTP/HTTPS 代理 URL），创建/更新/手动刷新/定时刷新均经该代理 GET 订阅 URL。
- 空字段 = 直连（与今天行为一致）。
- 非法值 400；代理或上游失败记 `last_error`、不破坏节点池。
- Web 对话框可配置并回显该字段。

**Non-Goals:**

- SOCKS5 / 其他代理协议
- 全局 `HTTP_PROXY` 环境变量必接（可后续增量）
- UI 从本机 mapping 端口一键填入
- 代理失败自动回退直连
- 测速、出站流量改走 `fetch_proxy`
- 变更 API 路径或响应 envelope

## Decisions

### D1: 字段名 `fetch_proxy`

与数据面「节点代理 / 端口代理」区分。持久化与 JSON 均用 `fetch_proxy`；yaml `omitempty`，空不写盘。

备选：`proxy`（易混淆）、`http_proxy`（暗示仅 HTTP 且像环境变量名）——落选。

### D2: 仅支持 HTTP(S) 代理 scheme

校验：非空时 `url.Parse` 成功，`scheme` 为 `http` 或 `https`，且 `Host` 非空。支持 userinfo（`http://user:pass@host:port`）。

备选：一期同时做 SOCKS5——价值高但需自定义 Dialer，方案 A 明确不做。

### D3: 按请求构建 Transport，不共享带 Proxy 的 client

`Fetcher` 保留默认直连 client；当 `sub.FetchProxy != ""` 时，为该次请求使用带 `http.ProxyURL` 的 `http.Client`（超时与 `CheckRedirect` 与直连一致：15s、最多 5 跳）。

理由：订阅刷新是低频路径；避免按 proxy 字符串维护 client 缓存的复杂度与泄漏面。若后续 profiling 显示连接复用有价值再加缓存。

备选：永远一个 client + `Proxy` 函数读 context——可做，但当前 Fetch 签名是 `(ctx, sub)`，临时 client 更直观。

### D4: 校验落在 `validateSubInput`（与 url 规则并列）

`core.validateSubInput` 增加 `fetch_proxy` 规则，API 增改统一走这里；config 启动加载若存在非法值——沿用项目对 config 校验的既有风格（若 `validate.go` 校验订阅字段则一并加上，保证手写 config 也被拦住）。

### D5: 更新 `fetch_proxy` 不强制立即重拉

与现有 Update 行为对齐：改字段写回 config 并更新内存；是否立刻 fetch 取决于现有 Update 实现（当前改 URL/UA 后由调用方或 worker 周期处理）。若现有 Update 在改 URL 后会触发 refresh，则 `fetch_proxy` 变更应同样触发，保持一致；实现时对照 `UpdateSubscription` 现逻辑，**优先一致性，不强加新的「改 proxy 必同步拉取」**，除非改 URL 已经同步拉取。

（实现核对：若 Update 仅写盘不 fetch，用户可点手动刷新验证新代理——可接受。）

### D6: UI 可选单行 Input

订阅对话框增加「拉取代理（可选）」；placeholder 示例 `http://127.0.0.1:7890`；列表行不强制展示完整 proxy（可后续加标记）；提交时 trim，空串当未设置。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| 代理凭证写入 config.yaml | 已有 0600；文档/README 一句提醒与订阅 token 同级 |
| HTTPS 订阅经 HTTP 代理（CONNECT）行为依赖 Go 默认 Transport | 使用标准 `ProxyURL`，与 curl `-x` 常见行为一致；单测覆盖 plain HTTP 代理转发即可 |
| 用户误填 SOCKS URL | 校验拒绝非 http/https，错误信息写明仅支持 HTTP 代理 |
| 临时 client 无连接复用 | 刷新频率低（≥5m），可接受 |
| 命名与「代理节点」混淆 | 字段名 `fetch_proxy` + UI 文案「拉取代理」 |

## Migration Plan

- 无数据迁移：缺省字段 = 直连。
- 滚动升级：旧二进制忽略未知 yaml 键；新二进制读旧 config 正常。
- 回滚：降级二进制后 `fetch_proxy` 键被忽略或仍留在 yaml 无害（视 yaml 库 strict 与否；当前非 strict）。

## Open Questions

无。方案 A 范围已闭合；若后续需要 SOCKS5 或 env 兜底，另开 change。
