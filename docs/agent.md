# Agent 接入（sbm ctl 与 Skill）

AI Agent 或脚本通过受限的 Agent API 操作管理器。客户端是 `sbm ctl`，与管理器是同一个二进制。Agent 不需要 SSH 登录服务器，也不需要 sudo。

## 组成

| 部分 | 说明 |
|---|---|
| Agent API `/api/agent/v1` | 只接受 `Authorization: Bearer` 令牌，不接受会话 Cookie；旧管理接口也不接受令牌 |
| 令牌 | 在「Agent 接入」页创建，明文只显示一次。服务端只保存 SHA-256 摘要（`agent-tokens.json`，权限 0600）。有效期必填，最长 90 天，可限制来源网段，可随时吊销 |
| 权限 | `read` 查看；`runtime` 切换节点、测延迟；`write` 创建并校验变更集；`deploy` 应用、确认、回滚本令牌创建的变更集 |
| 变更集 | 预览 → 应用 → 确认窗口 → 确认或回滚。记录保存在 `agent/changesets/`，恢复点在应用前落盘 |
| 审计 | `agent/audit.jsonl`，记录令牌创建与吊销、变更集的各个阶段，内容已脱敏 |
| Skill | `skills/sing-box-manager/`，教 Agent 安全使用 `sbm ctl` |

Agent 可以修改：自定义规则、导入规则（编辑匹配条件与出站、删除，按导入策略版本校验）、预设规则组开关与出站、自定义拦截集合、过滤器、订阅（新增、改名、启停、刷新、删除）、手动节点、hosts、Split DNS、普通设备与设备组、proxy/direct DNS、默认出站和日志级别，并可撤销最近 20 个已确认的变更。

Agent 写入的节点（手动添加或订阅拉取）按协议与字段白名单校验：只允许常见代理协议及其协议字段，禁止引用本地文件（`*_path`/`*_file`，例如 `tls.client_key_path`）、覆盖 type/tag/server 等保留键，以及 bind_interface、detour 等拨号字段。`sing-box check` 只判断配置合法，不判断是否越权，因此不依赖它做这项检查。

Agent API 不提供：网关启用与恢复、拓扑变更（包括会改变 bypass 网段的设备变更）、内核下载与替换、root 策略、端口/路径/监听/自动应用等系统设置、令牌管理与改密、配置导入与代理整理方案。这些操作由管理员在界面上完成，Agent 可以查看状态和预览；如果用户为某次维护另行明确授权，Agent 按用户的具体指示执行，而不是自行绕过。

## 管理员：签发令牌

1. 打开管理界面「Agent 接入」，填写名称，勾选所需的最小权限，设置有效期（必填），需要时填写来源网段。
2. 复制只显示一次的令牌，在 Agent 所在机器上保存：

   ```sh
   mkdir -p ~/.config/sbm && umask 077
   cat > ~/.config/sbm/token   # 粘贴后按 Ctrl-D
   ```

   CLI 发现令牌文件权限宽于 600 时会拒绝使用。不要把令牌放进命令行参数或共享的环境变量。

3. 选择连接方式：

   - **Agent 在服务器上运行**：直接执行 `sbm ctl status`（默认 `http://127.0.0.1:9090`）。
   - **SSH 隧道**：`ssh -N -L 19090:127.0.0.1:9090 user@host`，然后执行 `sbm ctl --server http://127.0.0.1:19090 status`。如果令牌限制了来源网段，需要包含 `127.0.0.1/32`。
   - **局域网直连**：管理器以 `-listen <IP> -tls-cert … -tls-key … -allow-network …` 启动，Agent 执行 `sbm ctl --server https://<IP>:9090 --fingerprint <证书 SHA-256> status`。对 IP 自签的证书用指纹固定；有私有 CA 时用 `--ca-file`。CLI 默认拒绝对非回环地址使用明文 http，也不跟随重定向。
   - **可信局域网明文 HTTP**：管理器以 `-allow-insecure-http` 启动时，CLI 也需显式加 `--allow-insecure-http`（或 `SBM_ALLOW_INSECURE_HTTP=1`），例如 `sbm ctl --allow-insecure-http --server http://<IP>:9090 status`。令牌会以明文传输，只在可信网络使用，并建议给令牌加上来源网段限制。

## 安装 Skill

```sh
./scripts/install-agent-skill.sh                     # ~/.claude/skills/sing-box-manager
./scripts/install-agent-skill.sh ./.claude/skills    # 项目级
./scripts/install-agent-skill.sh ~/.codex/skills     # Codex
```

重复安装时，旧版本（包括你自己添加的文件）会先完整备份到 `~/.local/state/sbm-skill-backups/`（可用 `SBM_SKILL_BACKUP_DIR` 指定）。备份放在 skills 目录之外，避免被当成重复的 Skill 加载。新版本在同一目录的临时位置准备好后再替换，脚本会输出恢复旧版本的命令。

Agent 所在机器需要能执行 `sbm`（例如下载同版本的发布二进制并放进 PATH）。

## 关于 sudo 与系统账户

日常使用不需要 sudo。网关模式下，特权操作由 root 辅助服务按固定协议执行，Agent API 不会调用辅助服务的 apply 或 rollback。

如果 Agent 运行在服务器上，**不要**让它使用 root 账户、管理器的服务账户（`singbox-manager`）或拥有宽泛 sudo 的账户，也不要把它加入 `singbox-manager` 组。否则它可以直接读取数据目录，或访问辅助服务的 socket，API 层的权限隔离就失效了。请使用一个独立的普通账户，只给它令牌文件。

## 变更集与确认窗口

```sh
sbm ctl rule add --name 公司内网 --type domain_suffix --value corp.example --outbound DIRECT
sbm ctl change apply <id> --confirm-within 120
sbm ctl explain domain git.corp.example
sbm ctl change confirm <id>       # 或 sbm ctl change rollback <id>
```

- **不夹带未应用的改动**：如果管理界面已保存但尚未应用改动（`sbm ctl diff`），变更集默认无法应用。确需一并应用时，用 `include_pending=<差异摘要>` 重新创建。差异按 JSON 语义比较。
- **并发保护**：变更集绑定创建时的数据版本；之后数据有任何修改，应用都返回 `base_changed`。
- **只应用一次，重试幂等**：每个变更集只能应用一次。创建和应用都支持 `Idempotency-Key`，键与令牌及请求内容绑定：同一个键配不同内容返回 422，重试返回已记录的结果而不重新执行。
- **内核停止时不会被启动**：应用和回滚都恢复到操作前的内核运行状态。
- **确认窗口门禁**：等待确认期间，管理界面的配置与运行态写入、订阅定时刷新、节点切换都会被拒绝；登录、令牌吊销和确认/回滚仍然可用。界面顶部提供确认和回滚按钮。
- **恢复点**：应用前把恢复点（数据快照、运行配置、内核运行状态）写盘，并先持久化 `applying` 状态，然后才写入任何运行配置。确认与超时回滚互斥执行，确认时会校验本次应用的候选摘要和当前版本。
- **撤销已确认的变更**：确认后仍保留最近 20 个变更的恢复数据。`sbm ctl change revert <id>` 或界面上的「撤销此变更」会生成新的撤销变更集，预览后同样需要应用和确认。只有该变更之后没有其他修改时才允许撤销，否则返回 `revert_conflict`，不会覆盖后续数据。
- **主机重启边界**：恢复点记录系统启动标识（boot ID）。管理器重启（同一次开机）时会按原状态恢复内核运行；主机重启后只恢复数据与停止状态下的配置文件，不启动内核。如果内核已在运行，则不替换配置，进入 `recovery_required` 等待人工核对。
- **终态先落盘**：回滚或失败的终态写盘成功后才删除恢复点；写盘失败时保留恢复点并保持写入暂停。
- **日志不可信则停止**：变更日志无法读取时，Agent 的创建与应用都会被拒绝（`journal_unavailable`），只读接口仍可用。
- **回滚不覆盖后续数据**：回滚时如果发现已保存数据或运行配置在应用后又被修改（例如有人手工编辑了文件），不会覆盖，而是进入 `recovery_required` 并保持写入暂停，由管理员核对后在界面上标记为已处理。

### 自动回滚的保证范围

确认窗口由管理器进程计时：

- 管理器运行期间，到期未确认会自动回滚；
- 管理器启动时，在开放接口和启动订阅调度之前，会先恢复所有未确认的变更（包括尚未到期的）；
- 管理器崩溃或停机期间**没有独立看门狗，不保证准时回滚**。恢复只发生在下次启动时。新的启动不会写回旧的 sysctl 或 nftables，也不会隐式启用网关。

## 状态与证据

`sbm ctl status` 和 `explain` 区分三种证据：

- **saved**：已保存的数据；
- **applied**：已写入 `generated/config.json`，内核运行时就是生效配置；
- **runtime**：内核实时连接、选择组和 LAN DNS 查询记录，是真实流量证据。

`explain domain` 对配置的推断是静态的：rule_set、IP、端口等需要运行时信息的条件会列在 `may_match_first` 中，不作为结论。

## 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 用法或本地错误（令牌文件、参数） |
| 2 | 服务端拒绝：参数或校验失败 |
| 3 | 需要决策：等待确认、存在未应用改动、配置已变化、需人工恢复，或 `apply_ready=false` |
| 4 | 令牌无效、过期、吊销，或权限不足 |
| 5 | 网络或服务端错误；带 `--idempotency-key` 的创建和应用可以安全重试 |
