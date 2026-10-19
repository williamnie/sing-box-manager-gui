import type { AgentConnection } from '../api/agent';

// 复制到终端的参数始终单独引用，地址或未来字段中的特殊字符不能成为 shell 指令。
function shellArg(value: string) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

export function agentConnectCommand(connection: AgentConnection) {
  const args = ['sbm ctl connect', '--server', shellArg(connection.server)];
  if (connection.fingerprint) args.push('--fingerprint', shellArg(connection.fingerprint));
  if (connection.allow_insecure_http) args.push('--allow-insecure-http');
  return args.join(' ');
}
