# subscription-management Specification

## Purpose
TBD - created by archiving change build-sub2proxy-mvp. Update Purpose after archive.

## Requirements

### Requirement: 订阅的增删改
系统 SHALL 支持添加、编辑、删除订阅。每条订阅字段：`id`（创建时生成的 8 字符随机串，持久不变）、`name`（必填）、`url`（必填，必须为 http/https）、`user_agent`（可选，默认 `clash.meta`）、`refresh_interval`（可选，默认 6h，允许范围 5m–24h）。与既有订阅 URL 完全相同的添加请求 SHALL 被拒绝（409）。变更 SHALL 持久化到 config.yaml。

#### Scenario: 添加订阅
- **WHEN** 用户提交名称与合法订阅 URL
- **THEN** 系统生成 id、保存订阅并立即同步触发一次拉取，响应返回解析出的节点数量

#### Scenario: 重复 URL
- **WHEN** 提交的 URL 与既有订阅完全相同
- **THEN** 返回 409 与该订阅名称，不创建

#### Scenario: 非法 URL
- **WHEN** 提交 `ftp://` 或无法解析的 URL
- **THEN** 返回 400，不创建

#### Scenario: 删除订阅
- **WHEN** 用户删除某订阅
- **THEN** 仅以该订阅为唯一来源的节点从节点池移除（其他订阅或手动来源仍持有的节点保留），引用被移除节点的映射按 port-mapping 的节点消失规则降级

### Requirement: 订阅拉取与格式解析
系统 SHALL 使用订阅的 User-Agent 拉取 URL（超时 15 秒，响应体上限 16 MiB，跟随重定向最多 5 跳），并按以下顺序解析：(1) 响应可解析为 YAML 且含非空 `proxies:` 数组 → 按 Clash proxy 列表逐条解析；(2) 否则对响应体做 base64 解码（SHALL 同时容忍标准与 URL-safe 字母表、有无 padding），按行拆分为分享链接并转换为 Clash proxy 结构。两条路径均失败 SHALL 判定拉取失败，错误信息含响应体前 200 字符。

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

### Requirement: 定时刷新
系统 SHALL 按每条订阅的 `refresh_interval` 自动拉取（每订阅独立计时，自上次成功或失败时刻起算），并支持手动立即刷新（同步返回结果）。刷新成功 SHALL 记录刷新时间并按指纹增量更新节点池；刷新失败 SHALL 保留上次成功的节点数据、记录失败原因与时间，并在下个周期重试。

#### Scenario: 自动刷新成功
- **WHEN** 刷新间隔到期且拉取成功
- **THEN** 节点池按指纹增量更新（新增入池、消失移除、指纹相同保留），订阅记录本次刷新时间与节点数

#### Scenario: 刷新失败不破坏现状
- **WHEN** 定时拉取网络失败或解析失败
- **THEN** 现有节点池不变，订阅标记错误状态与原因（UI 可见），下个周期自动重试

#### Scenario: 手动刷新
- **WHEN** 用户点击某订阅的手动刷新
- **THEN** 立即执行拉取并同步返回成功（节点数）或失败（原因），重置该订阅的下次自动刷新计时

### Requirement: 配额信息展示
系统 SHALL 解析订阅响应的 `subscription-userinfo` 头（分号分隔的 `upload=` `download=` `total=`（字节）与 `expire=`（Unix 秒），字段允许部分缺失），持久化到该订阅并在 UI 展示已用流量比例与到期日期；头缺失时不展示配额区。

#### Scenario: 含配额头的订阅
- **WHEN** 拉取响应带 `subscription-userinfo: upload=123; download=456; total=1073741824; expire=1735689600`
- **THEN** 订阅列表展示已用 (upload+download)/total 进度与到期日期

#### Scenario: 配额头部分缺失
- **WHEN** 头只含 `total` 与 `expire`，无 upload/download
- **THEN** 展示可得字段，缺失字段留空，不报错
