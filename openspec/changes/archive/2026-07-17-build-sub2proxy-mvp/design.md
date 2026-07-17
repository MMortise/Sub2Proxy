# Design: build-sub2proxy-mvp

## Context

全新项目，无既有代码。目标形态：单二进制、单容器的自托管代理编排工具，部署在内网服务器供单管理员使用。

核心洞察：vmess/vless/reality 等协议实现绝不自研，数据面全部交给成熟核心（mihomo），本项目只做「控制面」——订阅解析、节点建模、端口映射管理、mihomo 配置生成与热重载、Web UI。

约束：
- 内网自用，单管理员，无多租户需求
- Docker 一键部署（`docker compose up -d`），镜像 ~20MB 级，无特权（无 TUN / NET_ADMIN）
- 端口约定：**27000 = Web UI/API；27001–27999 = 代理映射段**；compose 一次性发布 `27000-27999`
- 配置人可读可手改（单一 config.yaml）

## Goals / Non-Goals

**Goals:**
- 订阅 URL → 去重节点池 → 每端口独立出口（单节点或策略组）
- 六种出口策略：single / failover / round-robin / hash / sticky / auto
- Web 面板全流程操作，登录 key 认证；全部操作有等价 REST API，可脚本化
- 单 config.yaml 持久化，备份 = 拷贝 config.yaml + data/

**Non-Goals:**
- 不做系统代理 / 透明代理 / TUN 模式
- 不做规则分流（按域名/GeoIP 路由）——每端口固定出口即全部路由语义
- 不做多用户 / 权限体系 / HTTPS 终结（公网场景由用户前置反代）
- 不做流量历史曲线、审计日志（无数据库的边界）
- 不做订阅格式转换服务（不是 subconverter）
- 一期不做代理入站认证（暴露面靠 docker 端口绑定控制）

## Decisions

### D1: 数据面 = mihomo 库内嵌（vs sing-box / Xray-core / 自研）

选 mihomo（Clash.Meta 内核），以 Go library 方式 import 进主进程，go.mod 锁定具体版本：

- `listeners` 配置原生支持「端口 → 指定 proxy/group」，就是本项目核心功能
- `proxy-groups` 原生提供全部六种策略的数据面实现（见 D4）
- 订阅解析可复用：`adapter.ParseProxy()` 吃 Clash proxy map；`convert.ConvertsV2Ray()` 把 base64 分享链接列表转成 Clash proxy map
- 运行时状态（组内活跃节点、连接、流量）经进程内 Go 接口直读，**不开启 mihomo 的 external-controller REST 端口**（避免多一个未认证暴露面）

备选评估：sing-box 协议更全但配置变更需重启进程（断连）；Xray-core gRPC 动态 API 灵活但订阅解析器和策略组需要自己拼装。均落选。

库内嵌 vs 子进程：内嵌 = 单进程，容器内 PID 1 信号处理干净，无子进程供养与二进制分发负担；代价是依赖 mihomo 内部包（非稳定公开 API），升级需回归测试。接受，锁版本缓解。

### D2: 语言 Go + 前端 React/shadcn，embed 单二进制

- mihomo 是 Go 库，内嵌方案下 Go 是唯一解
- 前端 Vite + React + TypeScript + Tailwind + shadcn/ui（组件源码拷入项目，Radix 系，可定制）
- `pnpm build` 产物经 `embed.FS` 打进二进制，UI 与 API 同端口 27000 服务
- Docker 三阶段构建：`node:22`（前端）→ `golang:1.24`（`CGO_ENABLED=0` 编译）→ `alpine`（+ ca-certificates、tzdata，非 root 用户运行）

### D3: 订阅解析走「Clash 方式」

机场按 User-Agent 内容协商。拉取参数（均为固定值，一期不做配置项，除 UA 外）：

| 参数 | 值 |
|------|-----|
| User-Agent | 默认 `clash.meta`，每订阅可覆盖 |
| 请求超时 | 15 秒 |
| 响应体上限 | 16 MiB，超限判定失败 |
| 重定向 | 跟随，最多 5 跳 |

解析顺序：

1. 响应能被解析为 YAML 且含非空 `proxies:` 数组 → 逐条 `adapter.ParseProxy()`；单条不支持的协议类型跳过并记录警告，不影响其余条目
2. 否则将响应体做 base64 解码（容忍标准/URL-safe、有无 padding），按行拆分为分享链接（`vmess://` `vless://` `ss://` `trojan://` `socks://` 等）→ `convert.ConvertsV2Ray()` → 与路径 1 汇合为 Clash proxy map
3. 两条路径均失败 → 拉取失败，错误信息附响应体前 200 字符（截断换行），保留上次成功结果

配额：读取 `subscription-userinfo` 响应头（`upload` / `download` / `total` / `expire`，单位字节 / Unix 秒），字段容忍缺失，头不存在则 UI 不展示配额区。

内部统一表示 = Clash proxy map（`map[string]any`）。协议 URI 方言全部由 mihomo 已有代码消化，本项目零协议解析代码。

### D4: 映射 = 端口 + 策略 + 节点集，物化为 mihomo 配置

映射模型：「端口 → 策略 over 节点集」。策略与 mihomo 的对应关系：

| 本项目策略 | mihomo 物化 | 语义 |
|-----------|------------|------|
| `single` | 无组，listener 直指节点名 | 固定单节点 |
| `failover` | `fallback` 组 | 按节点列表顺序取首个健康节点，失效下移、恢复回切 |
| `round-robin` | `load-balance` + `strategy: round-robin` | 每个新连接轮询 |
| `hash` | `load-balance` + `strategy: consistent-hashing` | 按目标地址一致性散列，同站点固定走同节点 |
| `sticky` | `load-balance` + `strategy: sticky-sessions` | 同（源 IP，目标）短期粘滞同节点 |
| `auto` | `url-test` 组 | 周期测速自动选延迟最低 |

注：mihomo 无纯随机策略；`hash` 在目标多时近似随机且更实用（同站不换 IP，降低风控），UI 文案明示此语义。

健康检查（非 single 策略必有）：测试 URL 默认 `http://www.gstatic.com/generate_204`，间隔默认 300 秒，允许范围 30–3600 秒。

节点集两种互斥表达：
- **显式列表**：有序节点指纹数组，顺序即 failover 优先级
- **`node_filter`**：Go `regexp`（RE2）语法，按节点名匹配；每次订阅刷新后重新求值，配置生成时物化为当时的匹配名单

命名约定：组名 `pg-<port>`，listener 名 `in-<port>`（如 `pg-27001` / `in-27001`）。节点名在 mihomo 配置内若重名，生成时对后者追加 ` #2` ` #3` 后缀消歧（节点池内部始终以指纹为准）。

### D5: 节点身份 = 指纹哈希

指纹算法（去重键与稳定 ID 同源）：

1. 取节点的 Clash proxy map，删除 `name` 键
2. 递归规范化：所有层级 map 按键名字典序排序，序列化为紧凑 JSON（无空白）
3. `SHA-256` 该 JSON，十六进制小写为完整指纹；UI 展示取前 8 字符

映射的 `nodes` 列表元素为 `{id: 完整指纹, name: 冗余展示名}`（name 供人读与手改 config.yaml；程序以 id 为准，两者不一致时以 id 解析并回写修正 name）。订阅刷新后按指纹重关联，节点改名不断链。

节点消失降级规则：
- 多节点组：成员收缩，服务不断；组内成员清零 → 映射自动禁用 + 记录原因（UI 标红）
- single：节点消失 → 映射自动禁用 + 记录原因
- 消失节点在后续刷新中以相同指纹重现 → 自动回池、自动重入组；但**运行期内已被自动禁用的映射保持禁用**，需人工重新启用（避免出口在用户不知情时复活）
- 自动禁用是纯运行时状态，不写回 config.yaml 的 `enabled`；进程重启后按当时节点池重新求值——节点已恢复则映射直接恢复服务（与 port-mapping spec「自动禁用不改写用户配置」场景一致）

### D6: 持久化 = 纯文件，无数据库

| 数据 | 存放 | 权限 | 说明 |
|------|------|------|------|
| 静态配置 + 订阅 + 手动节点 + 映射 | config.yaml | 0600 | 真相源，UI 写回 |
| 订阅节点池缓存 | data/nodes.json | 0600 | 派生数据，可随时重建 |
| 延迟结果 / 活跃节点 / session | 内存 | — | 重启重建 |

- 内存为主：启动加载 → 运行期单一 state 结构（`sync.RWMutex` 保护）→ 变更防抖 **1 秒**合并写回
- 原子写：同目录临时文件 + `rename()`，防半写损坏
- 进程收 SIGTERM/SIGINT：先冲刷未落盘变更（上限 5 秒）再退出
- 未知字段：加载时告警不报错（向前兼容）
- UI 写回 yaml 会丢注释——接受（单文件备份简单压倒一切），完整注释示例放 README 供对照
- 存储层收敛为 `Load()/Save()` 小接口，未来需要历史数据再换 SQLite，业务代码不动

### D7: 认证与暴露面

- Web UI/API：单一 `auth_key`（config.yaml 明文，长度 ≥ 8 校验）
  - UI：登录页提交 key → 校验通过签发 HttpOnly、SameSite=Lax 的 session cookie（随机 128-bit token，内存存储，有效期 24 小时）
  - API：亦接受 `Authorization: Bearer <auth_key>` 直连，无需 session
  - 防暴力：同源 IP 连续 3 次失败后锁定 2 分钟（锁定期内 429 + 剩余时间提示；未锁定时 401 + 剩余次数提示；成功清零；锁定到期重置）
- 代理端口（27001–27999）本身无认证——内网即插即用；暴露范围由 compose 端口绑定控制（默认 `127.0.0.1:27000-27999:27000-27999`，放开局域网由用户改绑定为 `0.0.0.0`）
- 代理入站固定监听 0.0.0.0（容器网络必需），app 不提供代理监听地址开关；Web 端监听地址经 `listen` 字段可配（默认 `0.0.0.0:27000`）

### D8: Docker 端口段预发布

Docker 端口发布在容器创建时静态确定，与「动态建端口」冲突。解法：预留段 27000–27999 一次性发布；映射端口自动分配 = 27001–27999 内最小空闲端口；手填端口校验范围与冲突。`port_range` 在 config.yaml 可改，但需与 compose 端口发布段同步修改（README 说明）。

### D9: 模块划分与并发模型

```
cmd/sub2proxy/main.go     组装与生命周期（信号、优雅退出）
internal/config/          yaml 模型、加载/校验/原子写回（防抖）
internal/subscribe/       拉取器（UA/超时/userinfo）、双格式解析
internal/pool/            节点池 state、指纹、nodes.json 缓存、刷新调度、延迟测速
internal/mapping/         映射领域模型、端口分配、节点集求值、降级规则
internal/engine/          mihomo 内嵌封装：配置生成、热重载、运行时状态读取
internal/api/             REST 路由、认证中间件、session 存储
web/                      Vite + React + shadcn 前端（构建产物 embed）
```

并发模型：
- 全局单一 `State`（订阅+节点池+映射），`sync.RWMutex`；API handler 与后台任务都经它
- 后台 goroutine：订阅刷新调度器（每订阅独立 timer）、测速执行器（并发上限 8、单节点超时 5 秒、同节点在途去重）
- 引擎重载串行化：单 goroutine 消费重载请求 channel，**500ms 防抖合并**（订阅刷新常一次性改动几十节点）；重载失败保留上一份生效配置

## Risks / Trade-offs

- [mihomo 内部包非稳定 API，升级可能破坏编译] → go.mod 锁版本；升级走独立 PR 带集成回归
- [UI 写回丢 yaml 注释] → 字段命名自解释 + README 提供完整注释版示例；接受
- [端口段占用 1000 个宿主端口] → 默认仅绑 127.0.0.1；段大小/范围可在 compose 与 config.yaml 同步调整
- [订阅格式方言超出 Clash YAML / base64 两类] → 一期只保这两类；失败报错带响应片段便于排查
- [热重载语义（存量连接保持）依赖 mihomo 行为] → 集成测试固化「改 A 端口不断 B 端口连接」场景
- [单 key 明文认证强度有限] → 定位内网；README 明示勿公网裸奔，公网需前置反代 + TLS
- [`hash`≠真随机] → UI/README 文案明确语义，避免误解

## Migration Plan

全新项目，无迁移。部署 = `docker compose up -d`；回滚 = 换镜像 tag。config.yaml 向后兼容策略：新版本只增字段（带默认值）、不改既有字段语义；未知字段告警不报错。

## Open Questions

（无——探索阶段已收敛全部决策。）

---

## 附录 A：REST API 一览

统一约定：前缀 `/api`；请求/响应 JSON；非 2xx 响应体为 `{"error": "<人类可读信息>"}`；校验失败 400、未认证 401、资源不存在 404、端口/URL 冲突 409。除标注外均需认证（session cookie 或 Bearer）。

| Method | Path | 说明 |
|--------|------|------|
| POST | /api/login | 提交 `{"key": "..."}`，成功 set session cookie。无需认证 |
| POST | /api/logout | 销毁当前 session |
| GET | /api/health | `{"status":"ok","version":"..."}`。无需认证 |
| GET | /api/subscriptions | 订阅列表（含状态、配额、最近刷新结果） |
| POST | /api/subscriptions | 新增（触发一次同步拉取，返回节点数） |
| PUT | /api/subscriptions/{id} | 修改 name/url/user_agent/refresh_interval |
| DELETE | /api/subscriptions/{id} | 删除及其独有节点，联动映射降级 |
| POST | /api/subscriptions/{id}/refresh | 手动刷新（同步返回结果） |
| GET | /api/nodes?q=\<filter\> | 节点列表，q 为名称模糊过滤 |
| POST | /api/nodes | 手动添加 `{"link": "vless://..."}` |
| DELETE | /api/nodes/{id} | 删除节点（仅允许纯手动来源） |
| POST | /api/nodes/{id}/test | 单节点测速，同步返回 `{"delay_ms": 123}` 或失败 |
| POST | /api/nodes/test-all | 全量测速（异步启动，结果经列表接口轮询） |
| GET | /api/mappings | 映射列表（含运行时：当前活跃节点、禁用原因） |
| POST | /api/mappings | 创建（port 缺省则自动分配） |
| PUT | /api/mappings/{port} | 全量更新（含改端口，同套校验） |
| DELETE | /api/mappings/{port} | 删除并释放端口 |
| POST | /api/mappings/{port}/enable | 启用 |
| POST | /api/mappings/{port}/disable | 禁用 |
| GET | /api/status | 引擎状态、最近重载错误、各映射连接数与实时速率 |

订阅 `id`：创建时生成的短随机串（8 字符，config.yaml 内持久）。节点 `id`：指纹（见 D5）。映射主键：端口号。

## 附录 B：config.yaml 完整示例

```yaml
listen: 0.0.0.0:27000            # Web UI / API 监听地址
auth_key: "change-me-please"     # 登录 key，必填，≥8 字符
port_range: [27001, 27999]       # 映射端口分配范围（与 compose 发布段对应）
data_dir: /data                  # 节点缓存等派生数据目录

subscriptions:
  - id: a1b2c3d4
    name: 机场A
    url: https://example.com/sub?token=xxx
    user_agent: clash.meta       # 可选，默认 clash.meta
    refresh_interval: 6h         # 可选，默认 6h，允许 5m–24h

manual_nodes:                    # 手动添加的节点（原始分享链接，加载时解析）
  - "vless://uuid@host:443?security=tls&sni=example.com&type=ws#我的自建"

mappings:
  - port: 27001
    name: 美国线路
    strategy: failover           # single|failover|round-robin|hash|sticky|auto
    nodes:                       # 显式节点集（与 node_filter 二选一）
      - { id: "3f2a…完整指纹…", name: "美国 1" }
      - { id: "9c81…完整指纹…", name: "美国 2" }
    health_check:                # 非 single 策略可配，缺省用默认值
      url: http://www.gstatic.com/generate_204
      interval: 300              # 秒，30–3600
    enabled: true
  - port: 27002
    name: 全美自动
    strategy: auto
    node_filter: "美国|US"        # 正则；订阅刷新后自动重求值
    enabled: true
```

## 附录 C：核心数据模型（示意）

```go
type Node struct {
    ID       string         // 指纹：sha256(规范化 proxy map 去 name)
    Name     string         // 展示名（首个来源的名称）
    Protocol string         // vmess/vless/ss/trojan/socks/...
    Region   string         // 启发式提取（国旗 emoji / 常见地区词），未识别为 ""
    Proxy    map[string]any // Clash proxy map（真相源）
    Sources  []string       // 订阅 id 列表；手动节点含 "manual"
}

type Mapping struct {
    Port           int
    Name           string
    Strategy       string      // single|failover|round-robin|hash|sticky|auto
    Nodes          []NodeRef   // 显式节点集（与 NodeFilter 互斥）
    NodeFilter     string      // RE2 正则（与 Nodes 互斥）
    HealthCheck    HealthCheck // 非 single 有效
    Enabled        bool
    DisabledReason string      // 运行时字段，自动禁用原因；不落盘则为空
}

type NodeRef struct{ ID, Name string }
type HealthCheck struct{ URL string; IntervalSec int }
```

## 附录 D：生成的 mihomo 配置样例

对应附录 B 的两条映射（27002 的 filter 假设当时匹配到美国 1/2/3）：

```yaml
mode: global
log-level: warning
proxies:
  - { name: "美国 1", type: vless, ... }
  - { name: "美国 2", type: vless, ... }
  - { name: "美国 3", type: vless, ... }
  - { name: "英国 1", type: vless, ... }
proxy-groups:
  - name: pg-27001
    type: fallback
    proxies: ["美国 1", "美国 2"]
    url: http://www.gstatic.com/generate_204
    interval: 300
  - name: pg-27002
    type: url-test
    proxies: ["美国 1", "美国 2", "美国 3"]
    url: http://www.gstatic.com/generate_204
    interval: 300
listeners:
  - { name: in-27001, type: mixed, port: 27001, listen: 0.0.0.0, proxy: pg-27001 }
  - { name: in-27002, type: mixed, port: 27002, listen: 0.0.0.0, proxy: pg-27002 }
```

禁用的映射不产生 listener（其组也不生成）。single 映射的 listener `proxy` 直接写节点名。
