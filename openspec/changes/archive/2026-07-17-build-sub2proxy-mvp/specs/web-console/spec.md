# web-console

## ADDED Requirements

### Requirement: 登录认证
Web UI 与 REST API SHALL 以 config.yaml 的单一 `auth_key` 认证：

- UI：登录页提交 key，校验通过签发 session cookie（HttpOnly、SameSite=Lax、随机 128-bit token、内存存储、有效期 24 小时，进程重启失效）
- API：同时接受 `Authorization: Bearer <auth_key>`，无需 session
- 防暴力：同源 IP 连续 3 次登录失败后 SHALL 锁定该 IP 2 分钟，锁定期内所有登录尝试返回 429 并提示剩余时间；未锁定时的错误密钥返回 401 并提示剩余可尝试次数；成功登录清零计数；锁定到期后计数重置
- 除 `POST /api/login` 与 `GET /api/health` 外，所有接口未认证 SHALL 返回 401 `{"error": "unauthorized"}`

#### Scenario: key 登录
- **WHEN** 用户在登录页提交正确 key
- **THEN** 签发 session cookie 并进入面板

#### Scenario: 错误 key
- **WHEN** 提交错误 key
- **THEN** 返回 401，不签发 session

#### Scenario: 连续失败锁定
- **WHEN** 同一 IP 连续第 3 次提交错误 key
- **THEN** 返回 429 并提示已锁定 2 分钟；锁定期内即使提交正确 key 也返回 429

#### Scenario: API Bearer 直连
- **WHEN** 请求头带 `Authorization: Bearer <正确 key>`
- **THEN** API 正常响应，无需先登录

#### Scenario: session 过期
- **WHEN** session 签发超过 24 小时后请求 API
- **THEN** 返回 401，前端跳转登录页

### Requirement: Web 服务形态
Web UI 与 API SHALL 由主进程按 config.yaml `listen`（默认 `0.0.0.0:27000`）提供；前端静态产物经 `embed.FS` 打入二进制，无外部文件依赖。SHALL 提供无需认证的 `GET /api/health` 返回 `{"status":"ok","version":"<版本号>"}` 供容器健康检查。非 API 路径 SHALL 回退到前端入口（SPA 路由）。

#### Scenario: 单二进制出面板
- **WHEN** 容器启动后浏览器访问 `http://host:27000`
- **THEN** 返回登录页，无需任何外部静态文件

#### Scenario: 健康检查
- **WHEN** 请求 `GET /api/health`（无认证）
- **THEN** 返回 200 与 `{"status":"ok","version":"..."}`

### Requirement: REST API 覆盖全部操作
UI 的全部操作 SHALL 通过 REST API 完成，API 可独立脚本化调用。接口清单以 design.md 附录 A 为准，统一约定：前缀 `/api`，请求/响应 JSON，非 2xx 响应体 `{"error": "<原因>"}`，状态码语义——400 校验失败、401 未认证、404 资源不存在、409 冲突（端口占用、重复订阅 URL 等）。映射变更接口 SHALL 走完整链路：校验 → 持久化 → 触发热重载，失败在对应环节返回错误且不产生半程副作用。

#### Scenario: 脚本化建映射
- **WHEN** 脚本以 Bearer 认证 POST /api/mappings 创建映射
- **THEN** 行为与 UI 操作完全一致（校验、写回 config.yaml、热重载）

#### Scenario: 校验失败无副作用
- **WHEN** POST /api/mappings 提交范围外端口
- **THEN** 返回 400，config.yaml 与运行中数据面均无变化

### Requirement: 管理页面
面板 SHALL 使用 React + TypeScript + shadcn/ui 构建，提供四个页面：

1. **订阅管理**：订阅表格（名称、节点数、最近刷新时间与结果、配额进度条、到期日）；添加/编辑对话框（名称、URL、UA、刷新间隔）；每行操作：手动刷新、编辑、删除（删除需确认，提示将影响的映射数）
2. **节点列表**：表格（名称、协议 Badge、地区 Badge、来源、延迟列）；名称模糊过滤输入框；单节点测速按钮与全量测速按钮；手动添加对话框（粘贴分享链接）
3. **端口映射**：表格（端口、名称、策略、当前活跃节点、启停 Switch、失效标红+原因悬浮提示）；创建/编辑对话框——策略 Select（含各策略一句话说明，hash 注明「近似随机、同站固定」）、节点集二选一控件（显式：Command 组件模糊搜索多选 + 拖拽排序；或 node_filter 正则输入 + 实时匹配预览）、健康检查参数折叠区、端口输入（留空自动分配）
4. **运行状态**：引擎状态（正常/最近重载错误与时间）、每映射卡片（端口、活跃节点、连接数、实时上下行速率）

#### Scenario: 建映射交互
- **WHEN** 用户在映射页点击创建并选择 failover 策略
- **THEN** 对话框呈现节点多选（模糊搜索）与拖拽排序、健康检查折叠区、端口留空提示自动分配

#### Scenario: filter 实时预览
- **WHEN** 用户在 node_filter 输入「美国|US」
- **THEN** 对话框实时列出当前匹配到的节点名单与数量

#### Scenario: 失效映射可视
- **WHEN** 某映射因节点消失被自动禁用
- **THEN** 映射列表该行标红，悬浮显示原因与发生时间

#### Scenario: 删除订阅前告知影响
- **WHEN** 用户删除一个被 3 个映射引用节点的订阅
- **THEN** 确认对话框提示将受影响的映射数量
