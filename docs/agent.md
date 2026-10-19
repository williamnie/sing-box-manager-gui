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
| Skill | `skills/sing-box-manager/`，编入 sbm 二进制，教 Agent 安全使用 `sbm ctl` |

Agent 可以修改：自定义规则、导入规则（编辑匹配条件与出站、删除，按导入策略版本校验）、预设规则组开关与出站、自定义拦截集合、过滤器、订阅（新增、改名、启停、刷新、删除）、手动节点、hosts、Split DNS、普通设备与设备组、proxy/direct DNS、默认出站和日志级别，并可撤销最近 20 个已确认的变更。

Agent 写入的节点（手动添加或订阅拉取）按协议与字段白名单校验：只允许常见代理协议及其协议字段，禁止引用本地文件（`*_path`/`*_file`，例如 `tls.client_key_path`）、覆盖 type/tag/server 等保留键，以及 bind_interface、detour 等拨号字段。`sing-box check` 只判断配置合法，不判断是否越权，因此不依赖它做这项检查。

Agent API 不提供：网关启用与恢复、拓扑变更（包括会改变 bypass 网段的设备变更）、内核下载与替换、root 策略、端口/路径/监听/自动应用等系统设置、令牌管理与改密、配置导入与代理整理方案。这些操作由管理员在界面上完成，Agent 可以查看状态和预览；如果用户为某次维护另行明确授权，Agent 按用户的具体指示执行，而不是自行绕过。

## 接入步骤

接入涉及三台“设备”，每一步都注明在哪里执行：

| 名称 | 指的是 |
|---|---|
| **网关服务器** | 运行 sbm 管理器的机器（例如家里的 Linux 旁路网关） |
| **浏览器** | 任意能打开管理界面的电脑或手机 |
| **Agent 电脑** | 运行 Claude Code、Codex 等 Agent 的电脑（例如 Mac mini）。可以就是网关服务器本身 |

### 0. 让管理界面可从局域网访问（网关服务器，只需一次）

内网部署推荐使用明文 HTTP，不需要证书：

```sh
# 网关服务器上执行；IP 换成服务器的局域网地址
sudo scripts/install-gateway.sh /绝对路径/sbm /绝对路径/sbm-gateway --install --lan-http 192.168.50.142
```

脚本会让管理器监听 `http://192.168.50.142:9090`，来源网段默认取该网卡所在网段（可用 `--allow-network` 指定）。不用安装脚本部署时，给 sbm 加上 `-listen <IP> -allow-insecure-http -allow-network <网段>` 即可，详见[首次登录及监听](deployment-modes.md#首次登录及监听)。首次设置管理密码仍需在服务器本机或 SSH 隧道完成。

如果 Agent 就运行在网关服务器上，可以跳过这一步。

### 1. 创建令牌（浏览器）

用 `http://服务器局域网IP:9090` 打开管理界面，进入「Agent 接入」，填写名称，勾选所需的最小权限，设置有效期（必填，最长 90 天）。来源网段已按管理器允许的局域网预填。令牌明文只显示一次，先复制下来。

### 2. 执行一键接入命令（Agent 电脑）

在同一页面复制「一键接入命令」，到 **Agent 电脑**的终端执行，例如：

```sh
curl -fsSL 'http://192.168.50.142:9090/agent/setup.sh' | sh
```

脚本依次完成：

1. 从这台管理器下载与管理器同版本的 `sbm` 到 `~/.local/bin`（可用 `SBM_BIN_DIR` 指定），并在需要时把该目录写入 shell 的 PATH；
2. 把 Skill 安装到已存在的 `~/.claude/skills` 和 `~/.codex/skills`（都不存在时装到 Claude Code）；
3. 运行 `sbm ctl connect`，提示粘贴第 1 步的令牌。输入不显示；CLI 验证令牌和 API 版本成功后才保存连接。

下载不经过 Agent 电脑访问 GitHub：Agent 电脑与服务器平台相同时，管理器直接提供自身的二进制；平台不同时（例如 Linux 服务器、Mac Agent），管理器按自身版本从 GitHub Releases 下载一次（使用设置中的 GitHub 加速地址），按 `SHA256SUMS` 校验后缓存在数据目录的 `agent/clients/v<版本>/`，之后局域网内直接提供。服务器无法访问 GitHub 时，可把对应的 `sbm-<系统>-<架构>` 文件手动放进该目录。`/agent/setup.sh` 与 `/agent/download/` 不需要登录（内容都是公开文件），仍受来源网段限制。

管理器使用自签 HTTPS 时，页面生成的命令带 `--pinnedpubkey`，curl 只接受这台管理器的证书公钥，后续连接也自动固定证书指纹，不需要自己查找指纹。

### 3. 验证（Agent 电脑）

重新打开 Claude Code 或 Codex，对它说「用 sbm 看看网关状态」；也可以在新终端运行 `sbm ctl status`。

连接保存在 `~/.config/sbm/connection.json`（支持 `XDG_CONFIG_HOME`），包含令牌，权限 0600。再次执行一键命令或 `sbm ctl connect ...` 可更新 sbm、Skill 或令牌；验证失败不会覆盖原连接。已保存的令牌绑定服务地址，切换目标需重新连接。明文 HTTP 的提示只在连接时显示一次。

### 手动接入

已经装好 sbm 时，可以分别执行（都在 Agent 电脑上）：

```sh
sbm ctl skill install                       # 安装或更新内置 Skill；--dir 可指定目录，可重复
sbm ctl connect --server http://192.168.50.142:9090 --allow-insecure-http
```

`sbm ctl skill install` 重复安装时，旧版本（包括你自己添加的文件）会先移动到 `~/.local/state/sbm-skill-backups/`（可用 `SBM_SKILL_BACKUP_DIR` 指定），放在 skills 目录之外，避免被当成重复的 Skill 加载。从源码仓库安装也可以用 `scripts/install-agent-skill.sh [目录]`。

其他连接方式：

- **Agent 在服务器上运行**：直接执行 `sbm ctl status`（默认 `http://127.0.0.1:9090`）。
- **SSH 隧道**：`ssh -N -L 19090:127.0.0.1:9090 user@host`，然后执行 `sbm ctl --server http://127.0.0.1:19090 status`。如果令牌限制了来源网段，需要包含 `127.0.0.1/32`。
- **HTTPS 局域网直连**：管理器以 `-listen <IP> -tls-cert … -tls-key … -allow-network …` 启动，`sbm ctl --server https://<IP>:9090 --fingerprint <证书 SHA-256> status`；有私有 CA 时用 `--ca-file`。CLI 默认拒绝对非回环地址使用明文 http（需显式 `--allow-insecure-http` 或 `SBM_ALLOW_INSECURE_HTTP=1`），也不跟随重定向。
- **令牌文件**：`connect` 和普通命令都可用 `--token-file` 读取权限为 600 的令牌文件，适合脚本。不要把令牌放进命令行参数或共享的环境变量。

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
