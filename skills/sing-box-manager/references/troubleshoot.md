# 排查手册

每一步都要说明依据的证据级别：saved（已保存）、applied（已写入运行配置）、runtime（真实流量）。

## 某网站没走代理 / 走错出站

1. `sbm ctl status`：`core.running` 必须为 true，否则任何规则都不影响流量。如果 `config.pending.exists=true`，说明界面上有未应用的改动，saved 和 applied 会不一致。
2. `sbm ctl explain domain <域名> --source <设备IP>`
   - `applied.outbound` 和 `applied.matched` 显示运行配置中第一条**确定**命中的规则。
   - `applied.may_match_first` 列出排在前面、但需要运行时信息才能判断的规则（rule_set/geosite、IP、端口、协议），它们可能先命中。
   - `differs=true` 表示已保存配置和运行配置的结论不同。
   - 只有 `runtime.connections` 中的 `chains` / `rule` 是实际结果。
3. 如果没有实时连接，请用户在设备上访问一次，然后执行 `sbm ctl connections`，或再跑一次 explain。
4. 设备没有出现在 `dns.recent` 或 connections 中时，流量可能根本没经过本实例：
   - DNS 旁路模式：检查设备 DNS 是否指向旁路地址，以及主路由上是否有 FakeIP 网段到旁路地址的静态路由；
   - 完整网关：检查设备的默认网关；
   - IPv6、备用网络、浏览器自带 DoH 都可能绕过。
5. 需要修复时，新建规则。priority 越小越先匹配，自定义规则排在预设组和导入规则之前。修复后按标准流程应用、验证、确认。

## 网站被拦截

- `explain domain` 显示 `REJECT` 时，查看命中的是 blocklist（`custom-domain-reject`）、自定义规则，还是 ad-block 规则组。
- `classification` 显示该域名是否在广告列表中。
- 解除拦截：`sbm ctl block remove <域名>`，或给 ad-block 前面加一条更具体的放行规则。

## 某台设备的整体策略

`sbm ctl explain client <IP>`：
- `device_policy` 是已保存的策略解释（split/direct/strict/bypass）；
- `applied_source_rules` 是运行配置中按来源匹配的规则；
- `dns` 与 `runtime` 是真实证据。

## 节点不可用或很慢

1. `sbm ctl proxies`：查看选择组当前选中的节点。
2. `sbm ctl delay <节点tag...>`：测延迟（需要 runtime 权限），默认 5000ms 超时。
3. 换节点：`sbm ctl select <选择组> <节点>`。这只改运行态，不改持久配置；内核重启后是否保留取决于内核缓存设置。确认窗口内会被拒绝。
4. 订阅节点过期：`sbm ctl sub refresh <id>` 会创建变更集，`changes` 中显示拉取到的节点数，再应用并确认。

## 日志

- `sbm ctl logs --grep <关键字> --lines 500`：内核日志，已脱敏。只有最近 N 行，不代表完整流量。
- `sbm ctl logs --source app`：管理器自身日志，例如订阅更新失败、应用失败。

## DNS

- `sbm ctl dns --domain <域名>` 或 `--client <IP>`：LAN DNS 查询记录。需要在界面开启查询记录，且只覆盖经过本实例的 DNS。
- DNS 旁路模式下，代理域名的 A 记录返回 FakeIP，AAAA 等返回空答，这是预期行为。

## 遇到退出码 3

不要自动处理。把 `error`、`code` 以及相关的 `pending` / `operation` 原样告诉用户，并给出可选做法（例如等待确认、回滚、带上 include_pending、让管理员人工核对）。
