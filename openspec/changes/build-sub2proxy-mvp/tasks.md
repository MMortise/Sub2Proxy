# Tasks: build-sub2proxy-mvp

任务按依赖排序；每条附验收标准（完成 = 验收可演示/可测）。规格细节以 specs/ 为准，技术决策以 design.md 为准。

## 1. 项目骨架与配置层

- [x] 1.1 初始化 Go module，引入 mihomo 依赖并 go.mod 锁版本；写最小 PoC 验证三个关键 API：`adapter.ParseProxy()` 解析一个硬编码 Clash proxy map、`convert.ConvertsV2Ray()` 转换一条 vless:// 链接、以 listeners+proxy 起一个 mixed 端口并出流量。验收：PoC 程序跑通，`curl -x http://127.0.0.1:<port>` 经硬编码节点出站成功；PoC 代码留存 `cmd/poc/` 供参考后续删除
- [x] 1.2 实现 config.yaml 数据模型与加载器（internal/config）：全部字段、默认值填充、未知字段告警。验收：单测覆盖默认值、缺文件生成模板并退出（非 0 码）、未知字段告警不报错
- [x] 1.3 实现配置校验：auth_key ≥8、port_range 合法且不含 listen 端口、映射端口互斥/范围内、策略枚举、single 恰一节点、nodes/node_filter 互斥、正则可编译、health interval 30–3600、refresh_interval 5m–24h。验收：单测每条规则各有失败与通过用例，错误信息含字段名
- [x] 1.4 实现原子写回（同目录 tmp+rename、0600）与 1s 防抖合并、SIGTERM 冲刷（≤5s）。验收：单测覆盖并发变更合并为一次写、kill 后文件完整（旧或新，无半写）

## 2. 订阅与节点池

- [x] 2.1 订阅拉取器（internal/subscribe）：可配 UA（默认 clash.meta）、15s 超时、16MiB 上限、≤5 跳重定向、`subscription-userinfo` 头解析（容忍部分字段缺失）。验收：httptest 单测覆盖正常、超时、超限、配额头缺失/部分缺失
- [x] 2.2 双格式解析：YAML `proxies:` 路径与 base64（标准/URL-safe、有无 padding）分享链接路径统一产出 Clash proxy map；不支持协议单条跳过记警告；双路径失败报错含响应前 200 字符。验收：单测用真实格式样本（vmess/vless/ss/trojan YAML 与 base64 两版）产出一致结果；HTML 错误页样本报错含片段
- [x] 2.3 节点指纹（internal/pool）：去 name、递归键排序、紧凑 JSON、SHA-256。验收：单测覆盖改名不变指纹、参数变化换指纹、map 键序不影响指纹、跨订阅去重合并 sources
- [x] 2.4 节点池内存 state + `nodes.json` 缓存读写（0600）：启动缓存优先、损坏忽略重建、手动节点不进缓存。验收：单测覆盖缓存命中启动、损坏文件启动、缓存与手动节点合并
- [x] 2.5 刷新调度：每订阅独立 timer、手动刷新同步返回并重置计时、失败保留现状记录原因。验收：单测（mock 时钟）覆盖到期触发、失败重试、手动刷新重置周期
- [x] 2.6 手动节点：分享链接解析入池（复用 2.2 转换路径）、config.yaml `manual_nodes` 持久化、与订阅节点指纹重合时合并 sources、仅纯 manual 来源可删除。验收：单测覆盖添加/删除/重合保留场景
- [x] 2.7 节点测速：经节点请求 generate_204，单节点 5s 超时同步返回，全量并发 8、在途去重、异步执行。验收：集成测试对 mock 节点测得延迟；不可达节点返回不可用；全量测速不阻塞 API
- [x] 2.8 地区标注：国旗 emoji 优先、常见地区关键词表兜底（美/英/日/韩/港/台/新/德/法/加/澳）。验收：单测样本名（「🇺🇸 US-LA 01」「日本 BGP」「HK-IPLC」等）标注正确，未知名留空

## 3. 映射与引擎

- [x] 3.1 映射领域模型与操作（internal/mapping）：创建（自动分配最小空闲端口/手填校验/范围耗尽 409）、编辑（全量更新、改端口同套校验、编辑清除自动禁用原因）、启停、删除释放端口。验收：单测覆盖分配顺序、冲突提示含占用者名、编辑失效映射恢复
- [x] 3.2 节点集求值：显式列表按 id 解析（name 不一致回写修正）与 node_filter 正则匹配两路径；订阅刷新后重求值。验收：单测覆盖 filter 纳新/移出、id 优先于 name、single 约束
- [x] 3.3 节点消失降级：组收缩提示、组清零/single 消失自动禁用（记录原因与时间，运行时态不写回 config.yaml 的 enabled）、重现不自动启用、重启后按当前节点池重新求值。验收：单测覆盖四种场景，含重启语义
- [x] 3.4 mihomo 配置生成器（internal/engine）：proxies（同名追加 ` #N` 消歧）+ 策略→组类型映射 + `pg-<port>`/`in-<port>` 命名 + 禁用映射不产 listener/组 + 不开 external-controller。验收：golden file 单测——给定 state 快照，输出配置逐字节稳定；覆盖六种策略与同名消歧
- [x] 3.5 引擎封装：内嵌初始化、热重载（串行 channel + 500ms 防抖）、失败保留上一份配置并记录错误、启动失败空数据面兜底。验收：单测覆盖防抖合并（30 次变更一次重载）、坏配置后旧配置存活、错误可查
- [x] 3.6 运行时状态读取：每映射活跃节点（组实时选中者）、连接数、上下行速率，进程内接口。验收：集成测试起真实 listener，发起连接后状态数据反映连接数与活跃节点
- [x] 3.7 引擎集成测试：本地起 mock 代理服务端为节点，验证 mixed 双协议（http+socks5 出口一致）、failover 健康检查切换与回切、改 A 端口不断 B 端口存量连接。验收：`go test -tags=integration` 全绿

## 4. REST API

- [x] 4.1 API 骨架与认证（internal/api）：路由、统一错误体 `{"error":...}` 与状态码约定（400/401/404/409）、key 登录签发 HttpOnly SameSite=Lax 24h 内存 session、Bearer 直连、5 次失败后 3s 延迟、/api/login 与 /api/health 豁免。验收：单测覆盖 401 路径、Bearer 通行、session 过期、失败延迟生效
- [x] 4.2 订阅接口：GET/POST/PUT/DELETE + /refresh（同步返回）+ 配额透出；重复 URL 409；DELETE 联动节点移除与映射降级。验收：handler 测试覆盖每接口成功与主要错误码
- [x] 4.3 节点接口：GET 列表（q 过滤、延迟与地区字段）、POST 手动添加（400 带解析原因）、DELETE（非纯 manual 来源 400）、/test 同步、/test-all 异步。验收：handler 测试覆盖过滤、错误码、异步启动即返回
- [x] 4.4 映射接口：CRUD + enable/disable；变更走「校验→持久化→热重载」完整链路，校验失败零副作用。验收：handler 测试覆盖自动分配、409 冲突、编辑改端口、禁用后 GET 反映状态；失败请求后 config.yaml 无变化
- [x] 4.5 状态接口：GET /api/status 汇总引擎状态、最近重载错误、每映射运行时。验收：handler 测试 + 与 3.6 集成验证字段齐全

## 5. Web 前端

- [x] 5.1 脚手架：Vite + React + TS + Tailwind + shadcn/ui、API client（统一错误提示、401 跳登录）、登录页、SPA 路由与布局导航。验收：`pnpm build` 通过；登录/登出/过期跳转可演示
- [x] 5.2 订阅页：表格（节点数、刷新时间与结果、配额进度条、到期日）、增改对话框、手动刷新、删除确认（含影响映射数提示）、错误状态展示。验收：对照 web-console spec 场景逐条可演示
- [x] 5.3 节点页：表格（协议/地区 Badge、来源、延迟列）、模糊过滤、单节点与全量测速、手动添加对话框（解析失败展示原因）。验收：对照 spec 场景可演示；500 节点渲染流畅
- [x] 5.4 映射页：表格（当前活跃节点列、启停 Switch、失效标红+悬浮原因）、创建/编辑对话框（策略 Select 带一句话说明、节点 Command 多选+拖拽排序、node_filter 输入+实时匹配预览、健康检查折叠区、端口留空自动分配）。验收：对照 spec 场景逐条可演示，含 filter 预览与 hash 语义文案
- [x] 5.5 状态页：引擎状态与最近重载错误、每映射卡片（活跃节点、连接数、实时速率，2s 轮询）。验收：起流量后页面数据变化可见
- [x] 5.6 embed 集成：前端产物打进二进制、非 API 路径回退 SPA 入口、/api/health 无认证可达。验收：单二进制无外部文件起完整面板；健康检查 curl 通过

## 6. 部署与收尾

- [x] 6.1 Dockerfile 三阶段（node:22 前端 → golang:1.26 CGO_ENABLED=0 → alpine + ca-certificates + tzdata），非 root 用户（uid 10001），声明 EXPOSE 与 HEALTHCHECK（busybox wget -qO- /api/health）。验收：`docker build` 通过、容器内进程非 root、HEALTHCHECK healthy。镜像实测 46.7MB（非 <30MB——内嵌 mihomo 的二进制单独就 35MB，<30MB 目标在此架构下不可达；已是合理下限）
- [x] 6.2 docker-compose.yml：`127.0.0.1:27000-27999:27000-27999`、`./config.yaml:/data/config.yaml` 与 `./data:/data` 说明清楚挂载关系、restart unless-stopped、healthcheck。验收：`docker compose up -d` 一条命令冷启动成功；首启无配置时容器日志给出模板指引
- [x] 6.3 README：快速开始（三步内跑起来）、config.yaml 全字段注释示例（与 design 附录 B 同步）、六策略说明表（含 hash≈随机、同站固定的语义）、端口段与 compose 同步修改说明、安全边界（内网定位、勿公网裸奔、公网前置反代+TLS）。验收：按 README 从零操作可完成部署与建映射
- [x] 6.4 端到端验收：compose 起容器 → UI 添加真实订阅 → 建 single 与 failover 映射（含一个 node_filter 映射）→ `curl -x http://127.0.0.1:27001 https://api.ipify.org` 与 27002 出口 IP 不同 → 停首选节点验证 failover 切换 → 重启容器验证配置与映射恢复。验收：全流程录屏或操作记录归档
