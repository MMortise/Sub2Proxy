# config-persistence

## ADDED Requirements

### Requirement: config.yaml 为唯一真相源
系统 SHALL 从单一 config.yaml 加载全部配置，字段与默认值：

| 字段 | 必填 | 默认 | 校验 |
|------|------|------|------|
| `listen` | 否 | `0.0.0.0:27000` | 合法 host:port |
| `auth_key` | **是** | — | 长度 ≥ 8 |
| `port_range` | 否 | `[27001, 27999]` | 两元素、递增、不含 listen 端口 |
| `data_dir` | 否 | `/data` | 可创建/可写 |
| `subscriptions` | 否 | `[]` | 见 subscription-management |
| `manual_nodes` | 否 | `[]` | 每条为可解析分享链接 |
| `mappings` | 否 | `[]` | 见 port-mapping |

UI/API 引发的订阅、手动节点、映射变更 SHALL 写回同一文件。加载遇到未知字段 SHALL 告警但不报错（向前兼容）。文件缺失或 `auth_key` 为空 SHALL 生成一个随机 `auth_key`、写回文件、日志明文打印该 key，并正常启动（使 `docker compose up -d` 免手动配置即可用）。config.yaml 权限 SHALL 为 0600。

#### Scenario: 首次启动无配置
- **WHEN** 启动时 config.yaml 不存在
- **THEN** 生成含默认值与随机 auth_key 的配置文件（0600），日志打印生成的 key，进程正常启动

#### Scenario: auth_key 为空
- **WHEN** config.yaml 存在但 auth_key 为空字符串
- **THEN** 生成随机 auth_key、写回文件、日志打印，进程正常启动（不崩溃、不循环）

#### Scenario: 重启复用已生成的 key
- **WHEN** 已生成 key 的实例重启
- **THEN** 复用文件中的 key，不重新生成

#### Scenario: UI 变更写回
- **WHEN** 用户通过 UI 新增一条映射
- **THEN** config.yaml 的 `mappings` 段随之更新，重启后映射仍在

#### Scenario: 未知字段兼容
- **WHEN** config.yaml 含本版本不认识的字段 `future_option: x`
- **THEN** 启动成功并记录告警，写回时该字段被丢弃（写回以内存模型为准）

### Requirement: 原子写与防抖
写回 SHALL 采用同目录临时文件写入 + `rename()` 原子替换；1 秒内的连续变更 SHALL 合并为一次落盘。进程收到 SIGTERM/SIGINT SHALL 先冲刷未落盘变更（上限 5 秒）再退出。

#### Scenario: 写入中断不损坏
- **WHEN** 落盘过程中进程被强杀
- **THEN** 磁盘上的 config.yaml 要么是旧版要么是新版完整内容，不存在半写状态

#### Scenario: 连续操作合并落盘
- **WHEN** 用户 1 秒内连续启停三个映射
- **THEN** config.yaml 只被重写一次，内容为最终状态

#### Scenario: 优雅退出冲刷
- **WHEN** 存在未落盘变更时容器收到 stop（SIGTERM）
- **THEN** 变更先落盘，进程再退出

### Requirement: 节点缓存文件
订阅解析出的节点池 SHALL 缓存到 `<data_dir>/nodes.json`（0600），内容含：每订阅的节点列表（指纹、Clash proxy map、名称）、每订阅最近成功刷新时间。启动 SHALL 优先加载缓存使映射立即可服务，再按各订阅间隔后台刷新。缓存缺失或 JSON 损坏 SHALL 忽略并重新拉取订阅重建，不阻碍启动。手动节点不进缓存（真相源在 config.yaml）。

#### Scenario: 断网重启
- **WHEN** 容器重启且订阅服务器暂不可达
- **THEN** 节点池从缓存恢复，全部映射照常工作，订阅标记刷新失败待重试

#### Scenario: 缓存损坏
- **WHEN** nodes.json 内容损坏
- **THEN** 忽略缓存正常启动，立即拉取订阅重建；成功前引用订阅节点的映射按节点缺失状态处理

### Requirement: 配置校验
启动与每次写回前 SHALL 校验（除字段级校验外的交叉规则）：映射端口互斥且都在 `port_range` 内且不等于 listen 端口、策略为六枚举之一、single 映射恰好一个显式节点、`nodes` 与 `node_filter` 互斥、node_filter 可编译、health_check.interval 在 30–3600、订阅 refresh_interval 在 5m–24h。启动时校验失败 SHALL 报错退出并逐条指明字段与原因；运行时 API 变更校验失败 SHALL 返回 400 且不落盘、不重载。

#### Scenario: 手改配置非法
- **WHEN** 用户手改 config.yaml 将两个映射写成同端口后重启
- **THEN** 启动失败，错误信息指明冲突端口与两个映射名

#### Scenario: 手改配置合法
- **WHEN** 用户手改 config.yaml 调整某映射节点列表顺序后重启
- **THEN** 正常启动，failover 优先级按新顺序生效
