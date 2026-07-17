# port-mapping

## ADDED Requirements

### Requirement: 映射的创建与端口分配
系统 SHALL 支持创建端口映射，字段：`port`（`port_range` 范围内，默认 27001–27999；可省略由系统自动分配）、`name`（必填）、`strategy`、节点集（`nodes` 或 `node_filter` 二选一）、`health_check`（可选）、`enabled`（默认 true）。自动分配 SHALL 取范围内最小空闲端口；范围内无空闲端口 SHALL 返回 409。手填端口 SHALL 校验：在 `port_range` 内、未被其他映射占用、不等于 Web 端口。映射 SHALL 持久化到 config.yaml，主键为端口号。

#### Scenario: 自动分配端口
- **WHEN** 用户创建映射未指定端口且 27001 已被占用
- **THEN** 系统分配 27002（最小空闲）

#### Scenario: 端口冲突
- **WHEN** 用户手填的端口已被其他映射占用
- **THEN** 返回 409，提示占用该端口的映射名称

#### Scenario: 范围外端口
- **WHEN** 用户手填 8080
- **THEN** 返回 400，提示合法范围（当前 port_range 值）

### Requirement: 映射的编辑
系统 SHALL 支持对既有映射的全量更新（名称、策略、节点集、健康检查、端口）。改端口 SHALL 走与创建相同的校验；任何更新成功后 SHALL 触发配置重建与热重载。更新一个因节点消失被自动禁用的映射的节点集 SHALL 清除其自动禁用原因（enabled 状态由请求指定）。

#### Scenario: 改端口
- **WHEN** 用户将映射从 27001 改为空闲的 27005
- **THEN** 27001 停止监听、27005 开始监听，策略与节点集不变

#### Scenario: 换掉失效节点
- **WHEN** 用户编辑被自动禁用的映射，将节点集换为有效节点并置 enabled=true
- **THEN** 映射恢复服务，禁用原因清除

### Requirement: 出口策略
每个映射 SHALL 支持六种策略之一：`single`（固定单节点）、`failover`（按列表顺序故障转移）、`round-robin`（每连接轮询）、`hash`（按目标一致性散列，同站点固定节点）、`sticky`（同源同目标短期粘滞）、`auto`（周期测速选延迟最低）。非 single 策略 SHALL 支持健康检查参数：`url`（默认 `http://www.gstatic.com/generate_204`，须为 http/https）、`interval`（秒，默认 300，允许 30–3600）。single 策略 SHALL 忽略健康检查参数。

#### Scenario: failover 按序切换
- **WHEN** failover 映射的首选节点健康检查失败
- **THEN** 流量自动切到列表中下一个健康节点；首选恢复后自动回切

#### Scenario: single 固定出口
- **WHEN** single 映射绑定「美国 1」
- **THEN** 该端口所有流量始终经「美国 1」出站，无健康检查行为

#### Scenario: 健康检查参数越界
- **WHEN** 用户提交 interval=10
- **THEN** 返回 400，提示允许范围 30–3600 秒

### Requirement: 节点集的两种表达
映射的节点集 SHALL 支持两种互斥表达，同时提供或都不提供 SHALL 返回 400：(1) `nodes`——显式有序列表，元素为 `{id: 节点指纹, name: 冗余展示名}`，顺序即 failover 优先级，程序以 id 为准（id 与 name 不一致时按 id 解析并回写修正 name）；(2) `node_filter`——RE2 正则，按节点名匹配，创建时校验可编译，每次订阅刷新后重新求值自动纳入/移出匹配节点。`single` 策略 SHALL 只接受恰好一个显式节点，不接受 filter。

#### Scenario: 过滤器自动纳新
- **WHEN** 映射使用 `node_filter: "美国|US"` 且订阅刷新新增「美国 3」
- **THEN** 「美国 3」自动加入该映射的策略组，无需手动操作

#### Scenario: 非法正则
- **WHEN** 提交 `node_filter: "美国("`
- **THEN** 返回 400 与正则编译错误

#### Scenario: single 策略节点集约束
- **WHEN** 策略为 single 且提交了两个节点或提交了 node_filter
- **THEN** 返回 400，提示 single 需恰好一个显式节点

#### Scenario: 过滤器创建时零匹配
- **WHEN** 创建 enabled=true 的映射，node_filter 当前匹配不到任何节点
- **THEN** 创建成功但映射立即进入自动禁用状态，原因标注「过滤器无匹配节点」

### Requirement: 映射入站形态
每个启用的映射 SHALL 在其端口开启 mixed 入站（同端口同时接受 HTTP 代理与 SOCKS5），容器内监听 0.0.0.0，不设入站认证（暴露面由部署层端口绑定控制）。

#### Scenario: HTTP 与 SOCKS5 同端口
- **WHEN** 客户端分别以 `curl -x http://host:27001` 和 `curl -x socks5://host:27001` 访问
- **THEN** 两种协议均正常代理，出口节点一致

### Requirement: 映射启停与删除
系统 SHALL 支持映射的启用/禁用与删除。禁用 SHALL 立即关闭对应端口监听，但端口仍归该映射占有（他人不可复用，重新启用无需重新分配）；删除 SHALL 关闭监听、从 config.yaml 移除该映射并释放端口。两者均不影响其他映射的存量连接。

#### Scenario: 禁用不影响他端口
- **WHEN** 用户禁用 27001 映射且 27002 上有活跃连接
- **THEN** 27001 停止监听，27002 存量连接保持不断

#### Scenario: 删除后端口可复用
- **WHEN** 用户删除 27003 的映射后创建新映射
- **THEN** 27003 可被自动分配或手填使用

### Requirement: 节点消失降级
映射引用的节点从节点池消失时：多节点组 SHALL 收缩成员继续服务并在 UI 提示成员变化；组内成员清零或 single 的节点消失 SHALL 自动禁用映射、记录原因（含消失节点名与时间）并 UI 标红。自动禁用 SHALL 保留映射全部配置；节点重现不自动重新启用，需人工启用或编辑（见映射编辑）。自动禁用状态与原因为运行时信息，SHALL NOT 覆盖 config.yaml 中用户设置的 `enabled` 值。

#### Scenario: 组员清零
- **WHEN** failover 映射引用的全部节点在刷新后消失
- **THEN** 映射自动禁用（端口停止监听），UI 标红并注明「节点已全部失效」与时间

#### Scenario: 组员部分消失
- **WHEN** failover 组三个节点消失一个
- **THEN** 组收缩为两节点继续服务，UI 提示成员变化，config.yaml 中的节点列表保持用户原值

#### Scenario: 自动禁用不改写用户配置
- **WHEN** 映射因组清零被自动禁用后进程重启，且此时节点已随订阅恢复
- **THEN** config.yaml 中 enabled 仍为 true，映射恢复正常服务（自动禁用是运行时状态，重启后按当前节点池重新求值）
