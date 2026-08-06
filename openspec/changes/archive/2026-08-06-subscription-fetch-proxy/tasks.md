## 1. 模型与校验

- [x] 1.1 在 `model.Subscription` 增加 `FetchProxy` 字段（yaml/json：`fetch_proxy,omitempty`）
- [x] 1.2 在 `core.SubscriptionInput` 增加 `FetchProxy`；`AddSubscription` / `UpdateSubscription` 写入该字段
- [x] 1.3 在 `validateSubInput`（及 config 侧订阅校验若存在）校验：空合法；非空须为 http/https 且含 host；非法返回 400，错误信息标明仅支持 HTTP 代理

## 2. 拉取器

- [x] 2.1 `Fetcher.Fetch`：当 `sub.FetchProxy` 非空时用带 `http.ProxyURL` 的 client 发请求（超时 15s、最多 5 跳重定向与直连一致）；为空仍用现有直连 client
- [x] 2.2 单测：经本地 HTTP 转发代理成功拉取并解析；代理不可达时返回错误；直连路径回归不受影响（可复用 `pool_test` / 现有 httptest 模式）

## 3. 核心与 API 验收

- [x] 3.1 扩展 `core` / `api` 相关测试：带 `fetch_proxy` 创建成功、非法 `fetch_proxy` 得 400、代理失败刷新保留节点并记 `last_error`
- [x] 3.2 确认列表/详情 JSON 回显 `fetch_proxy`；config.yaml 读写 round-trip 保留该字段

## 4. Web 控制台

- [x] 4.1 `types.ts`：`Subscription` 与 `SubscriptionInput` 增加可选 `fetch_proxy`
- [x] 4.2 订阅添加/编辑对话框：可选「拉取代理」输入、编辑回显、提交 trim；文案与 placeholder 指向 HTTP 代理示例

## 5. 收尾

- [x] 5.1 如 README 订阅配置示例存在，补充一行 `fetch_proxy` 说明（可选、仅拉取）
- [x] 5.2 跑相关单测（`subscribe` / `core` / `config`）确认通过
