import type { AgentConnection } from '../api/agent';

// 复制到终端的参数始终单独引用，地址或未来字段中的特殊字符不能成为 shell 指令。
function shellArg(value: string) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

type ConnectionTarget = Pick<AgentConnection, 'server' | 'fingerprint' | 'allow_insecure_http'>;

export function agentConnectCommand(connection: ConnectionTarget) {
  const args = ['sbm ctl connect', '--server', shellArg(connection.server)];
  if (connection.fingerprint) args.push('--fingerprint', shellArg(connection.fingerprint));
  if (connection.allow_insecure_http) args.push('--allow-insecure-http');
  return args.join(' ');
}

// 一键接入：从管理器下载同版本 sbm、安装 Skill 并连接。自签 HTTPS 用公钥固定代替 CA 校验。
export function agentSetupCommand(connection: Pick<AgentConnection, 'server' | 'pinned_pubkey'>) {
  const args = ['curl -fsSL'];
  if (connection.pinned_pubkey) args.push('--insecure --pinnedpubkey', shellArg(connection.pinned_pubkey));
  args.push(shellArg(`${connection.server}/agent/setup.sh`), '| sh');
  return args.join(' ');
}
