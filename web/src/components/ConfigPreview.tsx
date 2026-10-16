export interface ConfigPreviewInfo {
  bytes: number;
  hash: string;
  preview_bytes: number;
  truncated: boolean;
}

interface Props {
  content: string;
  info?: ConfigPreviewInfo;
  source: 'current' | 'candidate' | 'previous';
}

export default function ConfigPreview({ content, info, source }: Props) {
  // 即便前端资源与旧服务端短暂混用，也不将完整大文本挂载到 DOM。
  const preview = content.slice(0, 16 * 1024).split('\n').slice(0, 200).join('\n');
  const truncated = info?.truncated || preview.length < content.length;
  return (
    <div className="min-w-0 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-zinc-400">
        <span>{info ? `原始配置 ${info.bytes.toLocaleString()} bytes · ` : ''}{truncated ? '仅显示开头片段（最多 16 KiB / 200 行）' : '完整脱敏预览'}</span>
        {info && info.bytes > 0 && (
          <a
            className="text-[#ff5722] hover:underline"
            href={`/api/config/download?source=${source}&hash=${encodeURIComponent(info.hash)}`}
            target="_blank"
            rel="noopener noreferrer"
            download
          >
            下载完整脱敏 JSON
          </a>
        )}
      </div>
      {truncated && <p className="text-[11px] text-amber-700 dark:text-amber-400">片段不代表完整 JSON；差异判断、校验与应用仍使用完整配置。下载文件保留脱敏标记，仅供审阅。</p>}
      <pre tabIndex={0} aria-label={`${source} 配置预览`} className={`max-w-full text-[11px] font-mono leading-relaxed text-zinc-300 whitespace-pre ${source === 'previous' ? 'max-h-60' : 'max-h-[60vh]'} overflow-auto selection:bg-[#ff5722]`}>
        {preview || (info?.bytes ? '// 无法预览：配置不是有效 JSON' : '// 尚无已应用配置')}
      </pre>
    </div>
  );
}
