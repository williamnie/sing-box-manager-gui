export type LogLevel = 'error' | 'warn' | 'info' | 'debug' | 'trace' | 'other';

export function logLevel(line: string): LogLevel {
  // 只识别独立级别字段，避免把域名或正文中的 error 字样误判成错误级别。
  const match = line.match(/(?:^|\s|\[)(TRACE|DEBUG|INFO|NOTICE|WARN(?:ING)?|ERROR|FATAL|PANIC)(?:\[|\]|\s|:|$)/i)?.[1]?.toLowerCase();
  if (match === 'fatal' || match === 'panic' || match === 'error') return 'error';
  if (match === 'warning' || match === 'warn') return 'warn';
  if (match === 'notice' || match === 'info') return 'info';
  return match === 'debug' || match === 'trace' ? match : 'other';
}
