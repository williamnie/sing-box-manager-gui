# Mac mini / macOS 部署

Mac mini 的硬件可以承载两种部署：原生 macOS 运行单机代理，或在 Linux 虚拟机中运行家庭网关。程序支持 macOS，并不表示 Linux 的 nftables、策略路由和 TProxy 接管方式也能直接迁移到 macOS。

| 需求 | 原生 macOS | Linux 虚拟机 |
| --- | --- | --- |
| 本机或局域网客户端使用 HTTP / SOCKS5 | 支持，使用 mixed 端口 | 支持 |
| 代理组切换、连接面板、日志、配置应用 | 支持，针对实际进入实例的连接 | 支持 |
| 本机应用通过 TUN 接管 | 支持生成配置，内核运行需要相应权限 | 支持 |
| 终端只指定 DNS，经主路由静态路由接入 | 不支持当前 DNS 旁路模式 | 支持，需 LAN 可达与主路由配合 |
| 家庭完整透明网关、接收主路由转交的真实 IP | 不支持当前 Linux helper 接管方式 | 支持，需正确网卡、权限和路由 |

## 作为局域网 HTTP / SOCKS 代理服务器

适合 Mac mini 继续运行下载、文件服务等应用，其他设备显式设置代理的场景。

1. Apple Silicon 使用 `sbm-darwin-arm64`，Intel 使用 `sbm-darwin-amd64`。每个实例使用独立数据目录，例如 `./sbm-darwin-arm64 -data "$HOME/.singbox-manager"`。首次登录流程见[部署说明](deployment-modes.md#首次登录及监听)。
2. 保持部署角色为单机 `desktop`。默认启用 TUN；仅需 HTTP/SOCKS 时，先关闭 TUN，再应用配置或启动内核。开启“允许局域网访问”，设置空闲 mixed 端口（默认 `2080`）。
3. 安装对应架构的 sing-box 内核，添加订阅或节点，检查并应用配置。客户端将 HTTP、HTTPS 或 SOCKS5 代理指向 Mac mini 的 LAN 地址和 mixed 端口。应用是否遵循 macOS 系统代理取决于应用本身；Telegram 等应用可配置自己的 SOCKS5 代理。
4. macOS 防火墙须允许受信 LAN 客户端连接 mixed 端口。该入口监听所有 IPv4 接口，当前不提供代理入口认证；不要通过公网端口转发暴露它。
5. Web 管理接口和内核控制 API 仍独立绑定回环地址；允许 LAN 代理不会同时开放面板。远程管理使用 SSH 转发，或按部署说明显式配置 HTTPS 和来源范围。

关闭 TUN 后，管理器不会替你修改 macOS 系统代理或默认路由。其他设备只修改 DNS 不会自动使用这个 mixed 入口。

## 本机 TUN 与后台启动

TUN 用于接管这台 Mac 自身的流量，不能代替本项目的 Linux 家庭网关。macOS 创建 TUN 和配置路由需要相应权限；当前用户级 LaunchAgent 不会自动赋予 root 权限。无权限时应检查启动日志，不能以“launchd 已安装”判断 TUN 已工作。

Tailscale、其他 VPN、已有系统代理和 IPv6 路由可能影响实际路径。共存时需在目标机器上分别验证代理出口、内网访问和连接来源。当前版本的 macOS 验证覆盖真实内核的 mixed 代理、连接控制、配置检查及重启恢复；未将 macOS TUN、Tailscale 共存或家庭透明接管当作已经真机验收的功能。

## 从 Linux 家庭网关迁移

希望保留“设备按需指定 DNS”的接入方式，应把 Linux 网关运行在 Mac mini 的 Linux 虚拟机内，并让虚拟机获得 LAN 可达的稳定地址。虚拟机只有宿主 NAT 地址时，不能假设主路由能把 FakeIP 和真实 IP 流量送到它。按 [DNS 旁路文档](dns-bypass.md) 配置与验证后再迁移终端 DNS。

原生 macOS 部署则使用新的数据目录，迁移节点和规则后选择单机模式。不要直接复制仍启用 `gateway` 角色的 Linux 数据目录并启动；macOS 会拒绝应用 Linux 网关配置。最近接入设备列表属于 Linux 家庭接入功能；原生 mixed 客户端应在连接面板按来源地址查看。
