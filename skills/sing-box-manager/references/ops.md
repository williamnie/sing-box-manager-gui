# 变更集操作（ops）参考

以 `sbm ctl capabilities` 返回的 `resources` 为准。下面是字段的语义说明。

## 文件格式

```json
{
  "title": "公司域名直连并拦截广告域名",
  "ops": [
    {"action": "create", "resource": "rule", "id": "corp-direct",
     "value": {"name": "公司内网", "rule_type": "domain_suffix", "values": ["corp.example"], "outbound": "DIRECT", "enabled": true, "priority": 10}},
    {"action": "add", "resource": "blocklist", "value": {"domains": ["ads.example.com"]}}
  ]
}
```

提交方式：`sbm ctl change create --file ops.json`。文件也可以直接是 ops 数组。

- `id`：`create` 时可自己指定（字母、数字和 `_.:-`，最长 64），省略则自动生成；`update`/`delete`/`refresh` 必填。
- `update` 的 `value` 是**部分字段补丁**，只写需要改的字段。不能改 `id`，未知字段会被拒绝。
- 同一变更集中可以先建分组再建引用它的设备，按顺序执行。
- 单个变更集最多 100 项。变更集有效期 1 小时，过期需重新创建。

## 资源

| resource | actions | value 字段 | 说明 |
|---|---|---|---|
| rule | create/update/delete | name, rule_type, values, outbound, enabled, priority, source_cidrs, network(tcp/udp), protocol, ports, port_ranges("a:b") | rule_type：domain / domain_suffix / domain_keyword / ip_cidr / geosite / geoip / port / port_range / match。priority 越小越先匹配。网关不支持 process_name |
| rule_group | update | enabled, outbound | 预设规则组（ad-block、google、cn …），不能新增或删除 |
| filter | create/update/delete | name, mode(urltest/select), include, exclude, include_countries, exclude_countries, subscriptions, all_nodes, node_tags, urltest_config, enabled | 代理分组；node_tags 为 null 表示按条件动态匹配，[] 表示不选节点 |
| subscription | create/update/delete/refresh | create：name, url；update：name, enabled | create/refresh 在**创建变更集时**拉取，节点结果会固定下来；url 用 `--url-file`/stdin 传入 |
| manual_node | create/update/delete | create：url（分享链接）或 node，enabled；update：node, enabled | 链接用 `--url-file`/stdin 传入 |
| host | create/update/delete | domain, ips, enabled | DNS hosts 映射 |
| split_dns | create/update/delete | domain_suffix, source_cidrs, server(direct/proxy) | |
| device_group | create/update/delete | name, policy(split/direct/strict/bypass), outbound | strict 必须选代理出站；DNS 旁路模式不允许 strict |
| device | create/update/delete | name, addresses, group_id, enabled | 网关模式下地址必须在 LAN 网段内，且启用设备的网段不能重叠 |
| dns | update | proxy_dns, direct_dns, final_outbound, log_level | 其他系统设置不开放 |
| blocklist | add/remove | domains | 自定义域名拦截集合（REJECT），只接受完整域名 |
| imported_rule | update/delete | revision, updates | 导入规则；id 是 `sbm ctl get imported_rule` 返回的索引，revision 必须与返回值一致。updates 只能改匹配条件、action(route/reject) 与 outbound，值为 null 表示删除该条件，其他扩展字段原样保留。必须单独成一个变更集 |
| revert | apply | （无 value） | 撤销已确认的变更集（id 为该变更集）。只在其后没有其他修改时可用；必须单独成一个变更集。CLI：`sbm ctl change revert <id>` |

## 会被拒绝的情况

- 节点字段越界：只允许 shadowsocks/vmess/vless/trojan/hysteria/hysteria2/tuic/socks/http 及其协议字段；tls、transport、multiplex、obfs 内部也有白名单（WebSocket/HTTP 的 `transport.path` 允许）；任何 `*_path`/`*_file` 本地文件引用都会被拒绝。

- `gateway_change_forbidden`：变更会改变规范化的网关配置。典型情况是启用、停用、改地址或删除 bypass 组里的设备，或者把组改成 bypass，这些都会改变 bypass 网段。应交给管理员在「部署与设备」页面处理。
- `validation_failed` / `kernel_check_failed`：配置无法生成或内核校验失败。错误信息会指出是第几项。
- 应用时返回 `pending_changes` / `base_changed` / `operation_pending` / `candidate_changed` / `expired`：都属于需要决策的情况（退出码 3），应先报告给用户，不要自动重试。

## 应用结果

- `pending_confirm`：已写入运行配置。如果内核原本在运行，此时已用新配置重启；**内核原本停止则保持停止**。
- `failed`：应用失败，已恢复到应用前的状态，可读取 `error`。
- `recovery_required`：自动恢复无法安全完成（例如发现了后续写入）。此时所有写入暂停，必须由用户在「Agent 接入」页核对后解除。Agent 不要尝试修复。
- 确认窗口内，其他配置写入（包括管理界面、订阅定时刷新和节点切换）都会被拒绝。
