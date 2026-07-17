# sub2proxy

自托管的「订阅转多端口代理」工具。输入机场订阅，自动解析、去重出节点池，再把**每个本地端口**绑定到一个节点或一组策略——每个端口就是一个固定出口的独立代理。

例如：订阅里有「美国 1」「英国 1」，你可以让 `http://127.0.0.1:27001` 固定走美国、`http://127.0.0.1:27002` 固定走英国，内网其他程序按需指定出口。

- **控制面自研，数据面交给 [mihomo](https://github.com/MetaCubeX/mihomo)**（内嵌为库）——协议解析、策略组、监听端口全部复用其成熟实现。
- 单二进制、单容器；Web 面板管理；单 `config.yaml` 持久化；无数据库。
- 每个端口是 mixed 入站（HTTP 代理 + SOCKS5 同端口）。

## 快速开始（Docker）

```bash
# 拉起容器（首次会在 ./data 下自动生成 config.yaml 并随机生成一个登录 key）
docker compose up -d

# 从日志里读出自动生成的登录 key
docker compose logs | grep auth_key
# => auth_key was empty; generated one: <你的登录 key>  (change it in /data/config.yaml to set your own)
```

打开 `http://127.0.0.1:27000`，用日志里的 key 登录。想自定义 key：编辑 `./data/config.yaml` 的 `auth_key`（≥8 字符）后 `docker compose restart`。

登录后：添加订阅 → 节点列表测速 → 建端口映射 → 内网程序用 `http://<主机>:27001` 等出口。

不用 Docker 也行：`go build ./cmd/sub2proxy && ./sub2proxy -config ./config.yaml`（需要 Go 1.26+，前端需先 `cd web/frontend && pnpm build`）。

## 二进制部署（推荐，无需在服务器上编译）

在服务器上构建镜像会把内嵌的整个 mihomo 内核重新编译一遍（很慢，且需能访问 Docker Hub）。改用预编译二进制：在本地打包上传到 GitHub Release，服务器直接下载对应架构的二进制运行，用 systemd 托管。

发布（在有 Go + pnpm 的机器上，一条命令）：

```bash
scripts/release.sh v0.1.0   # 构建前端 + 交叉编译 linux amd64/arm64（内嵌 UI）→ 发布到 GitHub Release
```

服务器安装 / 升级（root，仓库目录下执行）：

```bash
sudo deploy/install.sh                        # 不带参数=装最新 release；带 tag（如 deploy/install.sh v0.1.0）可指定版本
journalctl -u sub2proxy | grep auth_key       # 读首次自动生成的登录 key
```

二进制直接在宿主机监听 `27000-27999`：无需 Docker、无端口发布开销、无挂载权限问题。`config.yaml` 与节点缓存位于 `/opt/sub2proxy/data`。

## 端口约定

| 端口 | 用途 |
|------|------|
| `27000` | Web UI / REST API |
| `27001–27999` | 代理映射段，每个映射占一个端口 |

Docker 端口发布是容器创建时静态确定的，所以整段 `27000-27999` 一次性发布。**若修改 `config.yaml` 的 `port_range`，必须同步修改 `docker-compose.yml` 的端口发布段**，否则新端口无法从宿主机访问。

默认 compose 把端口段绑到所有网卡，局域网其他设备可直接访问。要限制为**仅本机**，把 `docker-compose.yml` 里的
`"27000-27999:27000-27999"` 改回 `"127.0.0.1:27000-27999:27000-27999"`。⚠️ 代理端口（27001–27999）只有在映射里设置了用户名/密码时才有认证，否则局域网内任何人都能使用。

## 出口策略

创建映射时为每个端口选择一种策略：

| 策略 | 行为 | 底层（mihomo） |
|------|------|---------------|
| `single` | 固定单节点 | 直连该节点 |
| `failover` | 按节点列表顺序取首个健康节点，失效自动下移、恢复回切 | `fallback` 组 |
| `round-robin` | 每个新连接轮询下一个节点 | `load-balance` / round-robin |
| `hash` | 按目标地址一致性散列，**同一站点固定走同一节点**（多目标时近似随机，且不易触发风控） | `load-balance` / consistent-hashing |
| `sticky` | 同一(源, 目标)短期粘滞同一节点 | `load-balance` / sticky-sessions |
| `auto` | 周期测速，自动选延迟最低 | `url-test` 组 |

> 注：mihomo 无「纯随机」策略。需要「换 IP」类近似随机时用 `hash`——它对不同目标站点分散到不同节点，同一站点保持稳定，比真随机更实用。

节点集有两种指定方式（二选一）：
- **显式列表**：手选节点，顺序即 failover 优先级。
- **过滤器 `node_filter`**：正则匹配节点名（如 `美国|US`），订阅刷新后自动纳入/移出匹配到的节点，无需手动维护。

节点消失时：多节点组自动收缩继续服务；组内节点清零或 single 节点消失，则该映射自动禁用并在面板标红（这是运行时状态，不改写你在 config.yaml 里设置的 `enabled`；节点恢复后需手动重新启用）。

## config.yaml 完整示例

```yaml
listen: 0.0.0.0:27000            # Web UI / API 监听地址
auth_key: "change-me-please"     # 登录 key，必填，≥8 字符（UI 登录与 API Bearer 共用）
port_range: [27001, 27999]       # 映射端口自动分配范围（与 compose 发布段保持一致）
data_dir: /data                  # 节点缓存 nodes.json 等派生数据目录

subscriptions:                   # 一般通过 UI 添加，也可手写
  - id: a1b2c3d4                 # 8 字符随机串，创建时生成，勿手改
    name: 机场A
    url: https://example.com/sub?token=xxx
    user_agent: clash.meta       # 可选，默认 clash.meta（机场按 UA 返回不同格式）
    refresh_interval: 6h         # 可选，默认 6h，范围 5m–24h

manual_nodes:                    # 手动添加的节点（原始分享链接）
  - "vless://uuid@host:443?security=tls&sni=example.com&type=ws#我的自建"

mappings:
  - port: 27001
    name: 美国线路
    strategy: failover           # single|failover|round-robin|hash|sticky|auto
    nodes:                        # 显式节点集（与 node_filter 二选一）
      - { id: "3f2a…完整指纹…", name: "美国 1" }
      - { id: "9c81…完整指纹…", name: "美国 2" }
    health_check:                 # 非 single 策略可配，缺省用默认值
      url: http://www.gstatic.com/generate_204
      interval: 300               # 秒，范围 30–3600
    enabled: true
  - port: 27002
    name: 全美自动
    strategy: auto
    node_filter: "美国|US"         # 正则；订阅刷新后自动重新求值
    enabled: true
```

> UI 的改动会写回同一个 `config.yaml`（原子写、防抖合并）。程序重写会丢失你手写的注释——完整字段以本示例为准。节点指纹（`nodes[].id`）由连接参数哈希得到，是节点的稳定 ID，改名不变；一般不需手填，用 UI 建映射即可。

## 安全边界

- 本工具定位**内网自用**。面板用单一 `auth_key` 认证（config.yaml 明文），代理端口本身不加认证——暴露范围完全由 compose 的端口绑定控制。
- **不要把它裸奔在公网。** 若必须公网访问，请在前面放置反向代理并启用 TLS，且强烈建议不要暴露 27001–27999 代理段。
- config.yaml / nodes.json 以 0600 权限写入，包含订阅 token 等敏感信息，注意勿提交到版本库（本仓库 `.gitignore` 已忽略 `config.yaml`、`data/`）。

## 开发

```bash
go test ./internal/...                          # 单元测试
go test -tags=integration ./internal/engine/    # 端到端数据面集成测试（绑真实端口）
cd web/frontend && pnpm install && pnpm build   # 前端构建到 web/dist（被 Go embed）
```

模块划分见 `openspec/changes/build-sub2proxy-mvp/design.md`（含 REST API 一览、数据模型、生成的 mihomo 配置样例）。
