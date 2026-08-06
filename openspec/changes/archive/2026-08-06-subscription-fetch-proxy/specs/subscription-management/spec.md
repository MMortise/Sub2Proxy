## MODIFIED Requirements

### Requirement: 订阅的增删改
系统 SHALL 支持添加、编辑、删除订阅。每条订阅字段：`id`（创建时生成的 8 字符随机串，持久不变）、`name`（必填）、`url`（必填，必须为 http/https）、`user_agent`（可选，默认 `clash.meta`）、`refresh_interval`（可选，默认 6h，允许范围 5m–24h）、`fetch_proxy`（可选，HTTP/HTTPS 代理 URL，仅用于拉取该订阅；空表示直连）。与既有订阅 URL 完全相同的添加请求 SHALL 被拒绝（409）。非空的 `fetch_proxy` SHALL 校验为合法 http/https URL 且含 host，否则返回 400。变更 SHALL 持久化到 config.yaml。

#### Scenario: 添加订阅
- **WHEN** 用户提交名称与合法订阅 URL
- **THEN** 系统生成 id、保存订阅并立即同步触发一次拉取，响应返回解析出的节点数量

#### Scenario: 添加订阅并指定拉取代理
- **WHEN** 用户提交名称、合法订阅 URL 与合法 `fetch_proxy`（如 `http://127.0.0.1:7890`）
- **THEN** 系统保存该字段，首次同步拉取经该 HTTP 代理访问订阅 URL，响应返回解析出的节点数量

#### Scenario: 重复 URL
- **WHEN** 提交的 URL 与既有订阅完全相同
- **THEN** 返回 409 与该订阅名称，不创建

#### Scenario: 非法 URL
- **WHEN** 提交 `ftp://` 或无法解析的 URL
- **THEN** 返回 400，不创建

#### Scenario: 非法 fetch_proxy
- **WHEN** 提交的 `fetch_proxy` 为 `socks5://127.0.0.1:1080`、缺少 host 或无法解析
- **THEN** 返回 400，不创建或更新

#### Scenario: 删除订阅
- **WHEN** 用户删除某订阅
- **THEN** 仅以该订阅为唯一来源的节点从节点池移除（其他订阅或手动来源仍持有的节点保留），引用被移除节点的映射按 port-mapping 的节点消失规则降级

### Requirement: 订阅拉取与格式解析
系统 SHALL 使用订阅的 User-Agent 拉取 URL（超时 15 秒，响应体上限 16 MiB，跟随重定向最多 5 跳）。若订阅配置了非空 `fetch_proxy`，拉取请求 SHALL 经该 HTTP/HTTPS 代理发出；未配置时 SHALL 直连。并按以下顺序解析：(1) 响应可解析为 YAML 且含非空 `proxies:` 数组 → 按 Clash proxy 列表逐条解析；(2) 否则对响应体做 base64 解码（SHALL 同时容忍标准与 URL-safe 字母表、有无 padding），按行拆分为分享链接并转换为 Clash proxy 结构。两条路径均失败 SHALL 判定拉取失败，错误信息含响应体前 200 字符。代理不可达或代理隧道失败 SHALL 判定拉取失败并记录原因，不影响现有节点池。

#### Scenario: Clash YAML 订阅
- **WHEN** 订阅响应为含 `proxies:` 的 YAML
- **THEN** 每个 proxy 条目解析入节点池；单条不支持的协议类型跳过并记录警告，不影响其余条目

#### Scenario: base64 分享链接订阅
- **WHEN** 订阅响应为 base64 编码的分享链接行列表（vmess:// vless:// ss:// trojan:// socks:// 等）
- **THEN** 系统转换为 Clash proxy 结构入池，结果与等价内容的 YAML 订阅一致

#### Scenario: 无法解析的响应
- **WHEN** 响应既非合法 Clash YAML 也非合法 base64 行列表（如机场返回 HTML 错误页）
- **THEN** 拉取标记失败，错误信息包含响应前 200 字符，节点池保留上次成功结果

#### Scenario: 响应超限
- **WHEN** 响应体超过 16 MiB 或请求超过 15 秒
- **THEN** 拉取判定失败并记录原因，不影响现有节点池

#### Scenario: 经 HTTP 代理拉取成功
- **WHEN** 订阅配置了可达的 `fetch_proxy`，且经该代理可取得合法订阅响应
- **THEN** 解析结果与直连成功时一致，节点入池

#### Scenario: 拉取代理不可达
- **WHEN** 订阅配置了 `fetch_proxy` 但代理连接失败或超时
- **THEN** 拉取判定失败并记录原因，现有节点池不变
