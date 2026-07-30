## Why

部分部署环境无法直连机场订阅 URL（出口受限、订阅源只允许特定路径等），而当前 Fetcher 始终直连，导致订阅无法添加或刷新。需要在订阅配置上提供可选的 HTTP 拉取代理，让控制面能经指定通道获取节点列表，而不改动数据面出站行为。

## What Changes

- 订阅增加可选字段 `fetch_proxy`：HTTP(S) 代理 URL（如 `http://127.0.0.1:7890` 或带用户名密码的形式），仅用于拉取该订阅。
- 添加、编辑、手动/定时刷新均使用该字段；为空时保持现有直连行为。
- 校验非法 `fetch_proxy`（非 http/https 或缺少 host）返回 400。
- 代理不可达或经代理拉取失败时，沿用现有失败语义：记录错误、保留上次成功节点池。
- Web 订阅添加/编辑对话框增加可选「拉取代理」输入框。
- **非 BREAKING**：现有无该字段的订阅与 API 客户端行为不变。

## Capabilities

### New Capabilities

（无）本变更为既有订阅能力的增量，不引入新 capability 名称。

### Modified Capabilities

- `subscription-management`: 订阅字段与拉取路径支持可选 HTTP `fetch_proxy`；创建/更新校验；拉取与刷新经代理执行。
- `web-console`: 订阅增改表单展示并可提交 `fetch_proxy`。

## Impact

- **模型 / 配置**：`model.Subscription`、`config.yaml` 订阅条目持久化 `fetch_proxy`。
- **核心 API**：`core.SubscriptionInput`、校验与增改逻辑透传该字段。
- **拉取器**：`internal/subscribe.Fetcher` 按订阅设置 `http.Transport.Proxy`（每请求或按代理缓存 client）。
- **HTTP API**：`POST/PUT /api/subscriptions` 请求体可选 `fetch_proxy`；列表/详情 JSON 回显。
- **前端**：`types`、`subscriptions` 页对话框。
- **测试**：经本地 HTTP 代理成功拉取、非法 proxy 400、代理失败保留节点。
- **不做**：SOCKS5、全局 env 代理必接、借道本机 mapping 端口快捷选择、代理失败回退直连、数据面/mihomo 行为变更。
