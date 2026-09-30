# Linux 家庭网关部署与恢复

本项目以部署角色区分单机与家庭接入，家庭接入再区分“DNS 分流旁路”和“完整网关接管”。Linux 本身不会启用接管；旧单机配置仍为单机，既有 `gateway` 配置仍为完整网关。macOS 不调用网关系统操作。本文件的 TUN、整机转发、NAT、可选 DHCP 和修改终端网关说明专用于**完整网关接管**；DNS 旁路的限定 TProxy、主路由 DNS / 静态路由与缓存步骤见 [DNS 分流旁路](dns-bypass.md)。两种模式共用特权辅助服务、显式应用及恢复边界，不能同时启用。本文件描述开发交付后的部署流程，不代表已经操作或验收家庭现网。

## 所属资源与接管方式

网关使用 sing-box 的 `sbm-tun`、`auto_route`、`auto_redirect`、`strict_route`；TCP 与 UDP 都需要经过网关。上游绑定/自动探测接口用于避免内核自身流量再次进入 TUN。项目不生成第二套 TProxy 接管规则，不修改 sing-box 自动管理的 nftables 表和策略路由。

这里的 `strict_route` 是 TUN 的网络接管设置，与设备的严格全代理 `strict` 策略不同。普通工作电脑、手机默认推荐选择 `split`，让国内直连和其他路由规则继续生效；需要整设备强制代理时才选择 `strict`。设备直连仍经过内核的直连出站，绕过则使用允许的网络层直接转发路径。

可单独为某台普通分流设备设置“来源 CIDR + 嗅探协议 `stun`”的联合规则，选择代理或拒绝。便捷设置将其放在当前其他自定义规则之前，也先于导入规则；显式设备策略与 hosts 仍在它之前。此设置不扩大到所有网站、全部 UDP 或 BT/PT，不依赖固定 STUN 端口，也不保证识别所有 WebRTC 流量。保护仅覆盖真正经过该实例的流量；IPv6、备用网络和旁路绕行必须同时在网络层确认。规则页提供草案、已应用配置及路由次序，保存规则后仍需校验、应用，再做真实设备验证。

这些字段在 1.14 系列可用；不依赖 1.15 新增的 `auto_redirect_tproxy_mark`、`multi_queue`。能力仍须由实际安装版本和 `sing-box check` 验证。[官方 TUN 文档](https://sing-box.sagernet.org/configuration/inbound/tun/) 说明 `auto_redirect` 的 Linux 路由及 Docker 兼容设计。

特权辅助服务独立管理以下资源：

| 资源 | 行为与恢复 |
| --- | --- |
| `inet sbm_gateway` | 单个 nftables 事务更新；只处理 DNS 访问约束、受管来源转发保护、可选源 NAT。不存在所属记录的同名表拒绝接管 |
| IPv4 转发、相关接口 `rp_filter`/ICMP redirects | 保存原值，包括内核联动的各接口/default forwarding 与 accept_redirects；先安装转发保护，再启用转发；恢复时先恢复参数，再撤下保护 |
| IPv6 转发/上游 RA 接收 | 仅 `proxy` 模式启用；离开此模式恢复原值；`disabled` 不全局关闭主机 IPv6，而是在 LAN 转发路径拒绝非绕过来源的 IPv6 |
| 专用 DHCP 服务 | 默认关闭。显式启用时生成独立 dnsmasq 配置，语法校验、启动与健康检查；失败恢复前一配置和服务状态 |
| 管理器系统服务 | 状态读取及固定 `singbox-manager.service` 的异步重启；不接受其他服务名、命令或路径 |

全局转发开关会联动修改接口参数，检查会枚举当前接口，把这些联动键一并纳入快照，并保留无关 Docker/其他接口原值。

只有显式绕过组的来源 CIDR 可直接转发。其他 LAN 来源只能发往 TUN 或明确的内网/排除网段；内核停止时，外网原始转发被拒绝。此规则不覆盖在主路由上直接出网的设备。`exclude_cidrs` 是网络接管排除，不应填入宽泛公网地址范围。相同二层 LAN 的终端也可能互相伪造来源；家庭网关来源规则不等价于交换机端口安全。

NAT 默认关闭。旁路同网段且主路由已有回程通常不需要 NAT；不同下游网段且上游无法增加回程路由时，才显式允许和启用 IPv4 NAT。其规则限制 LAN 来源及上游接口，并排除内网目标，不对所有接口无条件 masquerade。IPv6 不做 NAT66；代理模式要求真实 IPv6 前缀、客户端默认路由以及上游回程。仅设置 DNS 不会改变 IPv6 默认路由。

Docker bridge 不应被当成独立物理终端；sing-box 自动重定向处理其兼容工作，管理器不清空 Docker 的规则。若网关本机运行容器，检查 bridge 网段并按真实拓扑配置排除。另一台机器上 BT/PT 的监听端口不是所有对端连接端口；需按设备来源及必要目的条件设置直连，不能对全网放开 `6881:60000` 之类宽范围规则。需要仅放行一个容器时，必须有可识别的容器独立地址/网段或明确的网络隔离，不能以远端进程名匹配，也不能无区别放行宿主全部服务。

## 初始化

在独立 Linux 主机准备 `iproute2`（`ip`、`ss`）、`nftables`、`procps`（`sysctl`）、systemd；可选 DHCP 另外安装发行版维护的 dnsmasq。不要为安装项目停用已有 DHCP。

在源码仓库构建，示例适用于 Linux amd64；其他架构更改 `GOARCH`：

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/sbm ./cmd/sbm
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/sbm-gateway ./cmd/sbm-gateway
sudo ./scripts/install-gateway.sh /tmp/sbm /tmp/sbm-gateway --install
```

脚本创建专用账户、数据目录、三个系统级 unit 和默认禁止网关的策略。它仅执行 `daemon-reload`，不启用/启动服务，不修改转发、路由、防火墙或现有 DHCP。若已有不属于本安装器的同名系统服务，脚本拒绝覆盖，需先备份和规划迁移。已存在的策略与数据保留。

辅助服务策略 `/etc/sbm-gateway/policy.json`、所有上层目录及 `/var/lib/sbm-gateway` 必须为 root 所有、不可被组/其他用户写入。参考策略只可在确认实际接口和网段后由系统管理员填写：

```json
{
  "allow_gateway": true,
  "allow_dns_bypass": false,
  "allowed_lan_interfaces": ["enp1s0"],
  "allowed_uplink_interfaces": ["enp1s0"],
  "allowed_lan_cidrs": ["192.0.2.0/24"],
  "allow_dhcp": false,
  "allow_nat": false,
  "managed_executable": "/var/lib/singbox-manager/bin/sing-box",
  "managed_config": "/var/lib/singbox-manager/generated/config.json",
  "managed_data_dir": "/var/lib/singbox-manager"
}
```

示例接口和文档地址仅供说明，必须替换为实际 LAN 接口与网段。`allow_gateway` 是家庭接管总开关；DNS 分流旁路还需要 root 策略额外显式授权 `allow_dns_bypass`，旧策略缺少此字段时默认不允许新能力。这是独立的系统授权边界，不接受 Web 请求修改。策略按 helper 启动时读取；初始化阶段修改后重启 helper。日常网关配置无需重写此策略，只有扩大特权允许范围才需系统管理员调整。

完成策略审阅后，初始化服务：

```sh
sudo systemctl enable --now sbm-gateway.service singbox-manager.service
```

管理器默认监听 `127.0.0.1:9090`，先通过 SSH 隧道登录。需要 LAN 管理时使用管理器的认证、TLS 和访问范围设置，不能通过修改安装脚本跳过鉴权。安装器的管理器账户拥有运行 TUN 所需的网络能力；helper 负责 sysctl/nftables 的结构化操作。helper 的 `CAP_SYS_PTRACE` 仅用于跨账户读取受管进程的 `/proc` 身份；协议没有 ptrace/debug 操作。dnsmasq 按官方方式由 root 初始化后降权到专用账户，独立配置不加载系统 dnsmasq 配置。[dnsmasq 手册](https://thekelleys.org.uk/dnsmasq/docs/dnsmasq-man.html)

## 日常检查、应用与恢复

管理界面执行预览、检查、应用、状态与恢复。特权通道固定为 `/run/sbm-gateway/control.sock`，权限 `0660 root:singbox-manager`，目录 `0750`；不是 TCP 服务。请求最大 64 KiB，拒绝未知字段和多 JSON 对象，有操作超时。只接受结构化网关配置；命令、服务名、文件路径和原始 nftables 语句均不能由客户端指定。

预览可在任何平台生成，不执行命令。检查允许未启用的草案；应用还要求 Linux、`gateway` 角色、`enabled=true`、root 策略允许的接口/网段，以及所有阻塞检查通过。检查包括接口地址、实际 IPv4 主表默认路由、DNS 端口、旧 `5354/9888` 监听、nftables TProxy、已占用的 sing-box 自动表/默认策略规则、所属资源漂移，以及 nftables/dnsmasq 语法。上游校验按主表有效默认路由的最低 metric 选择，所选接口和 gateway 必须与配置吻合；仅能在 LAN 上到达网关地址不足以通过检查。相同优先级的不同出口、multipath/nexthop、非单播默认路由及无法确认归属的策略路由会阻止应用，管理器不会替用户修改路由。IPv6 代理要求 LAN 接口实际存在对应配置前缀的有效单播地址；仅有 link-local 地址或 tentative、DAD 失败、已弃用地址不能通过。现有本地 `127.0.0.53` resolver 与专用 LAN 地址监听可并存；真实占用由监听地址判断。自身 DNS 监听按可执行路径、工作目录和唯一 `run -c` 配置参数核实，不能通过同名进程或 PID 文件冒充。

应用记录保存在 root 私有状态文件中，包含原始参数、所属表快照、配置摘要和未完成事务。重复应用相同配置不重复写入。每次变更、回退先比较实际资源；外部程序改过表、参数或 DHCP 文件时拒绝覆盖。系统命令只执行固定程序及内部生成参数，既不调用 shell，也不返回整个外部配置/进程参数到界面。

可选 DHCP 由专用 `sbm-gateway-dhcp.service` 提供，`port=0` 禁止其 DNS 服务，DNS 仍由 sing-box LAN DNS 入站处理。保留地址可带设备/组 tag，单设备可指定原主路由作为绕过网关，DNS 固定指向管理器 LAN 地址并由来源策略分流。不生成 `dhcp-authoritative`、不主动接管其他租约。端口检查只能发现本机冲突；开启前必须确认同一广播域中不存在其他发放相同地址段的 DHCP 服务器。DHCPv6/RA 不自动发布；IPv6 路由按现有拓扑单独验收。

状态文件同时记录 Linux `/proc/sys/kernel/random/boot_id`。主机重启后，旧运行态快照不能作为本次启动的恢复依据；状态会显示 `rebooted=true`、`recovery_required=true`，并阻止自动应用和日常重启操作。旧版本缺少 boot ID 的状态也须先恢复，不能假定属于本次启动。

重启后的显式“恢复”只在自有 nftables 表、sing-box/TProxy 接管规则、冲突监听、专用 DHCP 服务与旧策略路由均不存在时，先把旧状态持久化归档到 `state-previous-boot-*.json`，再清除 active/pending 记录；它不回写旧启动的 sysctl、nftables 或服务状态。完成恢复后，重新检查并显式应用以获得本次启动的 baseline。若系统启动脚本重建了旧接管资源，必须先核对和安全停止这些资源，不能盲目删除。同一次启动中的外部漂移仍按原规则拒绝覆盖，不能借重启恢复流程绕过。

因此系统管理服务可以随主机启动，网关接管不会仅凭上次启动的状态自动重放；重启后必须经过管理器恢复、检查和显式应用。

恢复撤回 helper 自有资源到第一次应用前的快照，并停止由它启动的 DHCP。恢复不删除其他表、不清理其他进程、不猜测原主路由设置。切换回单机前，应先把验证终端的网关/DNS 恢复到原路由器，再执行管理界面恢复并停止旧网关 core，最后保存单机角色。恢复中断后再次恢复会读取持久化事务继续。若进程在 nftables 事务已提交而指纹尚未写盘的极小窗口崩溃，工具会保留 `recovery_required` 并拒绝猜测删除；管理员须核对 `inet sbm_gateway` 与状态文件，不能清空整个规则集。

管理器的日常系统服务重启通过 helper 的固定 `restart-manager` 操作提交，响应后界面短暂断开。系统管理器 unit 使用 `KillMode=process` 保留 core 与日志中继以供重启恢复；要完全停用代理，应先经管理界面停止 core 再停止管理服务。首次初始化、修改 root 策略和升级系统 unit 属于系统管理员的初始化/维护操作，不通过 Web 开放通用 systemctl。

## 从已有代理实例分步切换

1. 在配置管理中导入最终 sing-box JSON，预览并核对节点、选择组、规则次序、来源范围、DNS、hosts 与无法等价转换的项，保留原导出和本机配置备份。导入只保存草案；可编辑规则列表为空时，应继续检查导入规则和完整路由次序。不要把含节点密钥的完整配置写入日志或公开问题。
2. 在独立数据目录先做离线配置检查。并行验证只启用无接管的独立 mixed 监听，使用不冲突端口；不要同时启动两套 TUN/TProxy，也不要让两个实例共享配置/PID/缓存目录。helper 会拒绝既有 TProxy、端口及默认路由规则冲突。
3. 预约单设备切换窗口，在保留主路由直连和带外管理的前提下先停止旧接管，再应用网关系统计划和已检查的 sing-box 候选配置。
4. 手动仅修改测试设备网关/DNS。分别核对 TCP、UDP、真实 IP 目标、域名、IPv4、IPv6、LAN 访问、DNS/hosts、普通分流/直连/严格全代理/绕过组与断核保护。普通分流应验证国内规则直连；配置 STUN 保护时应验证目标来源的可识别 STUN 命中及其他设备、普通 UDP 和 BT/PT 不被扩大匹配。也检查容器出站及回程，不以 STUN 某个固定端口测试代替覆盖。
5. 停止 core 验证严格策略没有退回家庭公网出口，恢复 core 验证业务恢复。演练原主路由/DNS 回退与 helper 恢复，再按设备逐步迁移。未完成这些检查前不要更改全家的 DHCP 或默认路由。

## 验证边界

普通开发测试完全使用假系统命令，覆盖权限/平台/开关、草案检查、幂等、冲突、资源归属、配置切换、失败恢复、DHCP 和跨进程中断恢复：

```sh
go test ./internal/gateway ./cmd/sbm-gateway
go test -race ./internal/gateway
```

提供显式 opt-in Linux 集成测试；仅在专用 Linux 测试机运行：

```sh
sudo env SBM_GATEWAY_NETNS_TEST=1 go test ./internal/gateway -run '^TestLinuxNamespace$' -v
```

测试创建新的网络 namespace，验证命名空间身份确实不同后才配置 dummy LAN；使用真实 ip/nft/sysctl 检查、应用、重复应用与恢复。宿主 systemd 在测试中被阻断，所有网络资源随 namespace 退出销毁。此测试还不能替代实际 sing-box 包转发、DHCP 租约、IPv6 RA 或家庭拓扑验收；缺少 Linux/root/namespace 能力时会明确跳过或失败，不能记为真实网关验收成功。
