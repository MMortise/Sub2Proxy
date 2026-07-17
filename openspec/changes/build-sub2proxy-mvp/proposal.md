# Proposal: build-sub2proxy-mvp

## Why

需要一个自托管工具：输入机场订阅后自动解析、去重出节点池，并把「本地端口 ↔ 节点/策略」一对一绑定——每个端口是独立的固定出口代理（如 27001 固定走美国 1，27002 固定走英国 1），供内网其他程序按需指定出口。现有客户端（Clash Verge、v2rayN 等）面向单一系统代理场景，没有「多端口、每端口独立出口 + Web 管理」的形态。

## What Changes

- 新建 sub2proxy 项目：Go 后端 + 内嵌 mihomo 核心（数据面零自研）+ React/shadcn Web 面板，单二进制、Docker 单容器部署。
- 订阅管理：拉取/解析（Clash YAML 优先、base64 分享链接兜底，复用 mihomo 解析器）、指纹去重、定时刷新、流量配额展示。
- 端口映射：在 27001–27999 范围内创建映射，每个映射绑定策略——`single`（单节点）、`failover`、`round-robin`、`hash`、`sticky`、`auto`（延迟优选），节点集支持显式列表或正则过滤器（`node_filter`）。
- Web 面板（端口 27000）：登录（单一 key）、订阅管理、节点列表（测速）、端口映射管理、运行状态（活跃节点/流量）。
- 持久化：单 `config.yaml`（真相源，UI 写回，原子写）+ `data/nodes.json` 节点缓存；无数据库。
- 部署：docker-compose 一键启动，端口段 27000–27999 一次性发布。

## Capabilities

### New Capabilities

- `subscription-management`: 订阅的增删改、拉取（UA 协商）、格式解析（Clash YAML / base64）、节点指纹去重、定时刷新、配额信息展示。
- `node-pool`: 去重后节点池的查询、地区/协议标注、延迟测速、手动添加节点、订阅刷新后的节点生命周期（指纹稳定、消失处理）。
- `port-mapping`: 端口映射的创建/启停/删除、六种出口策略、节点集（显式/过滤器）、健康检查参数、端口范围与冲突校验、节点消失时的降级规则。
- `proxy-engine`: 内嵌 mihomo 的配置生成（proxies/proxy-groups/listeners）、热重载、运行时状态查询（活跃节点、连接、流量）。
- `web-console`: Web UI 与 REST API、登录认证（key → session/Bearer）、各管理页面。
- `config-persistence`: config.yaml 读写回、原子写与防抖、节点缓存文件、启动加载与校验。

### Modified Capabilities

（无——全新项目，无既有 spec。）

## Impact

- 全新代码库：Go module（后端 + mihomo 库依赖）、`web/`（Vite + React + TS + Tailwind + shadcn/ui）、`Dockerfile`（三阶段构建）、`docker-compose.yml`。
- 端口约定：27000 = Web UI/API；27001–27999 = 代理映射段（mixed 入站，HTTP+SOCKS5 同端口）。
- 外部依赖：mihomo（库内嵌）、机场订阅 URL（运行时外部输入）。
- 安全边界：面板凭单一 key 认证；代理端口本身无认证，暴露范围由 docker 端口绑定控制（默认仅 127.0.0.1，文档说明）。
