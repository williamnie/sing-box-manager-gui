# 领域概念

## 部署角色

| 角色 | 说明 | Agent 能做什么 |
|---|---|---|
| desktop（单机） | 本机 mixed 代理，可选 TUN | 规则、DNS、节点等全部变更 |
| gateway + access_mode=dns（DNS 分流旁路） | 终端或 DHCP 把 DNS 指向旁路；代理域名返回 FakeIP，主路由把 FakeIP 网段静态路由到旁路；终端默认网关不变 | 同上；不能启用 strict 设备组 |
| gateway + access_mode=full（完整网关） | 终端默认网关指向本机，TUN + auto_route | 同上 |

网关的启用、恢复、拓扑和 root 策略由管理员在界面上操作，Agent 只能用 `gateway status|plan|clients` 查看。

## 设备策略组

- `split`（普通分流，推荐）：遵循自定义规则、规则组和导入规则，国内可直连。
- `strict`（严格全代理）：该设备的业务流量和 DNS 全部走指定代理出站，优先于国内直连等规则。出站不能含直连回退。
- `direct`：经过 sing-box 后直连。
- `bypass`：绕过内核，在网络层直接转发。**bypass 网段属于网关配置的一部分**，Agent 不能修改它。

设备策略只解释已保存的配置，不能证明设备流量真的经过网关。

## 路由顺序

1. 协议嗅探 / DNS 处理
2. 设备来源策略
3. hosts
4. 自定义规则（priority 从小到大，同级按存储顺序；`custom-domain-reject` 拦截集合通常排在最前）
5. 预设规则组
6. 导入规则（保持原有顺序）
7. final 出站（`dns.final_outbound`，默认 Proxy）

同一条规则内的条件联合匹配：目的条件（域名/IP/规则集）之间是「或」，与来源、端口、协议之间是「与」。

## 出站名称

- `Proxy`：默认代理选择组；
- `DIRECT` / `REJECT`；
- 过滤器（filter）名称、国家分组；
- 导入策略中的组。

可用名称以 `sbm ctl proxies`（运行时）或 `sbm ctl rules` 为准。

## DNS

- `proxy_dns` / `direct_dns` 支持 UDP、TCP、TLS、HTTPS、QUIC、HTTP3。域名型 DNS 通过 bootstrap 解析。
- 匹配顺序：hosts → 设备 DNS 策略 → Split DNS → 可映射的域名规则。
- DNS 旁路模式：代理域名只有 A 记录返回 FakeIP，其他类型返回空答；直连域名正常解析。

## 状态与证据

- **saved**：data.json 中的数据。在界面保存但未应用时，会和 applied 不一致（`sbm ctl diff`）。
- **applied**：generated/config.json。内核运行时就是正在生效的配置；内核停止时不产生流量。
- **runtime**：内核实时连接、选择组状态、LAN DNS 查询记录。只有这一级能证明「流量确实这样走」。
