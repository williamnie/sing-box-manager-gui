---
name: sing-box-manager
description: 通过 `sbm ctl` 操作内网部署的 sing-box manager（sbm）：查看状态与日志、解释某个域名或设备为什么走某个出站、排查 DNS/节点问题，以及用「变更集 → 应用 → 验证 → 确认」流程修改分流规则、拦截域名、DNS、订阅与手动节点、过滤器和普通设备策略。用户提到 sbm、sing-box manager、旁路网关/家庭网关的分流、某网站不走代理/被拦截、加规则、换节点、订阅刷新时使用。
---

# sing-box manager（sbm）Agent 操作

所有操作都通过 `sbm ctl` 完成。它是 Agent API 客户端：HTTPS（或本机回环 HTTP）加只读文件中的 Bearer 令牌。**不需要 SSH，也不需要 sudo。**

## 1. 连接

先运行 `sbm ctl capabilities`。它返回当前令牌的权限（scopes）、可修改的资源及字段、确认窗口的范围与保证说明。以它的返回为准，不要凭记忆假设。

连接参数可用环境变量或全局参数提供，**必须写在子命令之前**：

| 场景 | 用法 |
|---|---|
| 在服务器本机 | `sbm ctl status`（默认 `http://127.0.0.1:9090`） |
| 有 SSH 但管理器只监听回环 | 用户先建隧道 `ssh -N -L 19090:127.0.0.1:9090 user@host`，然后 `sbm ctl --server http://127.0.0.1:19090 status` |
| 局域网直连 | `sbm ctl --server https://<管理IP>:9090 --fingerprint <证书SHA-256> status`；非回环地址 CLI 拒绝明文 http |

令牌文件默认是 `~/.config/sbm/token`（也可用 `--token-file` 或 `SBM_TOKEN_FILE` 指定），权限必须为 600。令牌只能由用户在管理界面「Agent 接入」页创建。**不要索要管理密码**，也不要把令牌写进命令行参数、环境变量或日志。

## 2. 硬性规则

1. **先读后写。** 任何修改前先跑 `sbm ctl status`，看 `active_operation`、`config.pending`、`core.running`。
2. **只用变更集修改。** 所有写命令只创建并校验变更集，不改运行配置。真正生效需要 `change apply`，然后在确认窗口内验证，再 `change confirm`。
3. **应用后必须验证再确认。** 验证手段：`explain domain`、`connections`、`dns`、`logs`。验证失败或拿不准时执行 `change rollback`，不要确认。
4. **不夹带别人的改动。** `status` 中 `config.pending.exists=true` 表示有人在界面保存了改动但尚未应用。默认情况下变更集无法应用（退出码 3）。应该把 `sbm ctl diff` 的内容告诉用户，用户同意后才用 `--include-pending <digest>` 重新创建。
5. **不要绕过限制。** 网关启用/恢复、拓扑变更、影响 bypass 网段的设备变更、内核升级、root 策略、端口/路径/监听设置都不在 Agent API 范围内。需要时说明原因，建议用户在管理界面操作；只有用户为这类维护**另行明确授权**并提供相应权限时，才按用户的具体指示执行。不要自行用 SSH 改数据文件、sudo 或调用旧接口来绕过。
6. **区分三种证据。** `saved` 表示已保存；`applied` 表示已写入运行配置（内核运行时生效）；`runtime` 指实时连接或 DNS 记录，是真实流量证据。报告结论时说明依据的是哪一种。`explain` 中的 `may_match_first` 表示静态推断无法确定的规则，不要当成事实。
7. **确认窗口的保证有限。** 管理器运行期间，到期未确认会自动回滚；管理器重启时会优先恢复。**管理器停机或崩溃期间不保证准时回滚**，所以对可能断网的改动要缩短窗口，并尽快验证。
8. **退出码驱动分支。** 0 表示成功；1 用法错误；2 被服务端拒绝（参数或校验失败，修正后重试）；4 令牌无效或权限不足，告诉用户需要哪个权限；5 网络或服务端错误，可用相同的 `--idempotency-key` 重试。3 表示需要决策，按 `code` 分别处理：
   - `base_changed` / `candidate_changed` / `expired` / `imported_revision_changed`：数据在此期间变了。若用户的意图没变、改动范围也没变，可重新读取状态后重建变更集，**最多重试 2 次**；如果新状态让改动的含义变了，就停下来说明。
   - `operation_pending`：先 `sbm ctl change list` 查看。如果是你自己刚应用的变更，继续完成验证并确认或回滚；如果是别人的，等待并报告。
   - `pending_changes`（他人保存但未应用的草案）、`recovery_required`、`gateway_change_forbidden`、`revert_conflict`，以及任何需要扩大改动范围的情况：**停下来报告并询问用户**。

## 3. 标准修改流程

```sh
sbm ctl status                                   # 无 active_operation，pending.exists=false
sbm ctl rule add --name 公司内网 --type domain_suffix --value corp.example --outbound DIRECT
# → 输出 data.id、changes、config_changes、warnings、apply_ready
sbm ctl change apply <id> --confirm-within 120   # state=pending_confirm
sbm ctl explain domain git.corp.example          # applied.outbound 应为 DIRECT
sbm ctl connections                              # 如有实时流量，核对 chains/rule
sbm ctl change confirm <id>                      # 或 sbm ctl change rollback <id>
```

要撤销一个**已确认**的变更，执行 `sbm ctl change revert <id>`。它会生成新的撤销变更集，同样需要 apply → 验证 → confirm。只有该变更之后没有任何其他修改时才能撤销（否则返回 `revert_conflict`，应改为新建反向变更）。只保留最近 20 个已确认变更的恢复数据。未确认的变更直接用 `change rollback`。

一次要改多项时，写一个 ops 文件，用 `sbm ctl change create --file ops.json` 一次提交，这样只应用一次、只确认一次。各资源的字段见 [references/ops.md](references/ops.md)。

节点（手动添加或订阅拉取）只接受常见代理协议及其协议字段，禁止引用本地文件（`*_path`）、覆盖 type/tag/server 等保留键，以及 bind_interface/detour 等拨号字段；被拒时如实告诉用户原因，不要改写节点去绕过。

订阅 URL 和节点分享链接含凭据，**只能从文件或 stdin 传入**，例如 `sbm ctl sub add --name 主订阅 --url-file -`，然后通过 stdin 输入 URL。读取类输出会自动脱敏。

## 4. 排查

先读 [references/troubleshoot.md](references/troubleshoot.md)。常用命令：

- `sbm ctl explain domain <域名> [--source <设备IP>]`：已保存与已应用配置分别命中哪条规则、DNS 查询记录、实时连接。
- `sbm ctl explain client <IP>`：设备策略、按来源匹配的规则、该设备最近的 DNS 查询和连接。
- `sbm ctl logs --grep <关键字> --lines 500`、`sbm ctl dns --domain <域名>`、`sbm ctl proxies`、`sbm ctl delay <节点...>`。
- `sbm ctl rules`：规则的实际生成顺序（自定义 → 预设组 → 导入 → final）。

领域概念（部署模式、设备策略、规则顺序、DNS 旁路）见 [references/concepts.md](references/concepts.md)。

## 5. 命令速查

只读（read 权限）：`capabilities` `status` `diff` `versions` `rules` `get <资源>` `nodes [--country HK]` `proxies` `connections` `kernel` `gateway status|clients|plan` `explain domain|client` `logs` `dns` `audit` `change list|show <id>`

运行态（runtime 权限，不改持久配置）：`select <选择组> <节点>`、`delay <节点...>`

变更（write 权限创建，deploy 权限应用、确认、回滚）：
- `rule add|delete|enable|disable`
- `block add|remove <域名...>`
- `sub add|refresh|delete`
- `node add|delete`
- `op <action> <resource> [id] --value JSON | --value-file F`
- `change create --file F`
- `change apply|confirm|rollback <id>`

完整用法：`sbm ctl help`。
