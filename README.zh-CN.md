# FlowLens

[English](README.md)

FlowLens 是一个自托管的 Linux 流量可观测性服务。原生 Agent 采集网络流元数据并发送至原生服务端；服务端将数据存储在 PostgreSQL 中，并提供浏览器仪表盘。

## 功能

- 将流量归属到主机进程、显式启用时的 Docker 容器，以及已只读挂载访问日志的 Nginx Proxy Manager 主机。
- 使用 eBPF 套接字事件、网卡计数器、进程归属和受限的 DNS/TLS 证据，不存储数据包载荷。
- 在网页界面提供概览、流量、所有者、域名、告警、健康状态、保留策略和 Webhook 工作流。
- 在服务端临时不可用时，Agent 会在本地暂存事件。

## 架构与部署

FlowLens 以两个原生 systemd 服务运行：受观测 Linux 主机上的 `flowlens-agent.service`，以及与网页资源并置的 `flowlens-server.service`。服务端使用已有 PostgreSQL 数据库，通常通过由运维人员管理的 HTTPS 反向代理公开。FlowLens 不提供容器化部署栈。

从[原生安装指南](docs/operations/install.md)（英文）开始。[运维概览](docs/operations/foundation.md)（英文）说明服务归属和日常检查；[归属说明](docs/operations/attribution.md)（英文）解释采集器设置和证据等级。

## 安全与隐私权衡

Agent 需要访问 Linux 网络遥测信息。请根据本机加固策略审查随附的 systemd 服务和 sysctl 配置。Docker 归属默认关闭；即使 FlowLens 仅将 Docker socket 用于读取清单，Docker socket 访问仍等同于主机级权限。无需容器归属时请保持关闭。

FlowLens 存储连接元数据和派生归属信息，仍可能暴露基础设施之间的关系。请限制仪表盘、数据库、配置、备份、Agent 暂存、NPM 日志和 GeoIP 文件的访问权限。使用 HTTPS、专用凭据及符合自身需求的保留策略。私密漏洞报告方式见 [SECURITY.md](SECURITY.md)。

## 限制

- 预期部署环境需要 Linux、systemd、eBPF 支持和 PostgreSQL。
- 由于 DNS 缓存、加密名称解析、共享托管和连接复用等因素，域名归属可能是推断结果或不可用。
- 进程与容器归属具有时间敏感性；短生命周期工作负载可能在所有采集器观测完成前退出。
- FlowLens 是可观测性辅助工具，不是抓包系统、计费工具、入侵防御系统，也不能替代主机和数据库监控。
- 发布验收数据与环境相关。仓库提供不含测量数据的验收报告模板，而非性能基准声明。

## 概念图

![使用合成流量数据的 FlowLens 概念总览](docs/images/flowlens-overview-concept.png)

该概念图由真实仪表盘使用合成数据渲染，不包含生产主机名、地址、账号或流量记录。公开截图必须使用合成数据以及通用的保留示例名称和地址。不得发布包含真实基础设施、账号、主机名、地址、令牌、Cookie 或流量关系的截图。

## 开发与许可证

可复现的本地检查见 [CONTRIBUTING.md](CONTRIBUTING.md)（英文）。FlowLens 采用 [Apache-2.0](LICENSE) 许可证；署名信息见 [NOTICE](NOTICE)。
