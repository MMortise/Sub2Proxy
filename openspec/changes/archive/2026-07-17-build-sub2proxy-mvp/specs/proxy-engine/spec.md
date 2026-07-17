# proxy-engine

## ADDED Requirements

### Requirement: mihomo 配置生成
系统 SHALL 由内存 state（节点池 + 映射表）全量生成 mihomo 配置：

- 全部去重节点写入 `proxies`；若节点展示名重复，生成时对后续同名者追加 ` #2` ` #3` 后缀消歧（内部引用始终以指纹解析）
- 每个**启用**的非 single 映射生成一个 `proxy-group`，命名 `pg-<port>`；类型映射：failover→`fallback`、round-robin→`load-balance`(strategy=round-robin)、hash→`load-balance`(strategy=consistent-hashing)、sticky→`load-balance`(strategy=sticky-sessions)、auto→`url-test`；组成员为节点集求值结果（显式列表按序 / filter 按当时匹配），健康检查 url 与 interval 写入组参数
- 每个**启用**的映射生成一个 mixed `listener`，命名 `in-<port>`，监听 `0.0.0.0:<port>`；`proxy` 指向 `pg-<port>`（非 single）或节点名（single）
- 禁用（含自动禁用）的映射 SHALL NOT 产生 listener 与组
- mihomo 的 external-controller SHALL NOT 开启（运行时状态经进程内接口读取）

#### Scenario: failover 映射的配置物化
- **WHEN** 存在端口 27001、策略 failover、节点 [美国 1, 美国 2]、interval 300 的启用映射
- **THEN** 生成 fallback 组 `pg-27001`（成员按序、url 与 interval 写入）与 listener `in-27001` 指向 `pg-27001`

#### Scenario: single 映射不生成组
- **WHEN** 存在 single 策略映射绑定「英国 1」
- **THEN** listener 的 `proxy` 直接为「英国 1」，不生成 proxy-group

#### Scenario: 同名节点消歧
- **WHEN** 两条订阅各有一个名为「香港 1」但指纹不同的节点
- **THEN** 生成配置中出现「香港 1」与「香港 1 #2」，引用它们的映射各自指向正确条目

### Requirement: 热重载
订阅刷新、映射增删改启停、节点池变化等任何影响数据面的变更 SHALL 触发配置重建与 mihomo 热重载。重载请求 SHALL 串行化并以 500ms 防抖合并（一次订阅刷新的多处变更只触发一次重载）。热重载 SHALL 不中断未受影响端口的存量连接。引擎 SHALL 内嵌于主进程运行，无子进程。

#### Scenario: 新增映射不断存量
- **WHEN** 27001 上有活跃下载且用户新建 27003 映射
- **THEN** 27003 立即可用，27001 的下载不中断

#### Scenario: 连续变更合并重载
- **WHEN** 一次订阅刷新导致 30 个节点变化与 3 个映射组成员变化
- **THEN** 引擎只执行一次重载

### Requirement: 运行时状态查询
系统 SHALL 经进程内接口暴露每个启用映射的运行时状态：当前活跃出口节点（single 即其节点；策略组为实时选中者）、活跃连接数、实时上下行速率（字节/秒）。数据不落盘，接口供 REST `/api/status` 与映射列表使用。

#### Scenario: 查看 failover 当前出口
- **WHEN** failover 组因首选失效已切到第二节点
- **THEN** 状态数据返回该映射当前活跃节点为第二节点

#### Scenario: 连接数与速率
- **WHEN** 27001 上有 3 条活跃连接正在传输
- **THEN** 状态数据返回该映射连接数 3 与非零实时速率

### Requirement: 引擎故障恢复
引擎初始化或热重载失败 SHALL NOT 使主进程崩溃：重载失败时保留上一份生效配置继续服务，记录错误（时间 + 原因）供 `/api/status` 与 UI 展示；下一次变更正常触发新的重载尝试。启动时引擎初始化失败（如缓存节点数据异常）SHALL 以空数据面启动（仅 Web 可用），错误上报 UI。

#### Scenario: 非法配置重载失败
- **WHEN** 一次配置重建被 mihomo 校验拒绝
- **THEN** 上一份配置继续生效（既有端口不受影响），UI 显示重载失败时间与原因

#### Scenario: 失败后可恢复
- **WHEN** 重载失败后用户修正了问题映射
- **THEN** 新变更触发重载并成功，错误状态清除
