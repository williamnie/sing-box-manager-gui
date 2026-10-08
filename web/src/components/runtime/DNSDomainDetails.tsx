import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader } from '@nextui-org/react';
import { ExternalLink } from 'lucide-react';
import type { DNSDomainListStatus, DNSDomainMatch } from '../../api/dnsQueries';

export interface DNSInspection { domain: string; match?: DNSDomainMatch; status?: DNSDomainListStatus }

export default function DNSDomainDetails({ inspection, onClose, onBlock, blocking, listed, blockAvailable }: { inspection: DNSInspection | null; onClose: () => void; onBlock: (domain: string) => void; blocking: boolean; listed: boolean; blockAvailable: boolean }) {
  const { domain = '', match, status } = inspection ?? {};
  const external = 'inline-flex items-center gap-1 text-[#ff5722] underline underline-offset-4';
  return <Modal isOpen={!!inspection} onClose={onClose} scrollBehavior="inside" classNames={{ base: 'rounded bg-white text-zinc-800 dark:bg-[#12141d] dark:text-zinc-200', header: 'border-b border-zinc-200 dark:border-white/10', footer: 'border-t border-zinc-200 dark:border-white/10' }}>
    <ModalContent>
      <ModalHeader className="text-base">域名辅助判断</ModalHeader>
      <ModalBody className="gap-4 py-5 text-sm">
        <p className="break-all font-mono text-base font-semibold">{domain}</p>
        <div className={`space-y-2 rounded border p-3 text-xs leading-relaxed ${match ? 'border-amber-500/25 bg-amber-500/10 text-amber-800 dark:text-amber-200' : 'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/5 dark:text-zinc-300'}`}>
          <p className="font-semibold">{match ? '疑似广告 / 跟踪 · 命中 anti-AD' : status?.ready ? '未命中当前规则库' : '规则库尚未就绪'}</p>
          {match ? <><p>匹配依据：<code className="break-all">{match.rule}</code></p><p>{match.kind === 'domain' ? '完整域名匹配' : '域名及其子域名匹配'}。该列表也包含统计、日志收集等域名，命中不等于当前请求一定用于展示广告。</p></> : <p>{status?.ready ? '未命中不代表安全或没有广告，可能尚未被收录。请结合应用行为和其他来源判断。' : '先在列表上方点击“下载规则库”，即可在管理器本地比对域名。'}</p>}
          <p>此处仅提供判断线索，不代表内核已拦截，也不会自动添加拦截规则。</p>
        </div>
        {status?.ready && <p className="text-xs text-zinc-500 dark:text-zinc-400">本地更新：{new Date(status.updated_at).toLocaleString('zh-CN', { hour12: false })}<br />规则库版本：{status.version || '未提供'}</p>}
        <div className="space-y-2 text-xs leading-relaxed">
          <p className="font-medium">继续查询</p>
          <div className="flex flex-wrap gap-x-5 gap-y-3">
            <a className={external} href={`https://www.google.com/search?q=${encodeURIComponent(`"${domain}" 广告 跟踪`)}`} target="_blank" rel="noopener noreferrer">搜索域名用途<ExternalLink className="size-3" /></a>
            <a className={external} href={`https://www.virustotal.com/gui/domain/${encodeURIComponent(domain)}`} target="_blank" rel="noopener noreferrer">VirusTotal<ExternalLink className="size-3" /></a>
            <a className={external} href="https://github.com/privacy-protection-tools/anti-AD" target="_blank" rel="noopener noreferrer">规则库与误报反馈<ExternalLink className="size-3" /></a>
          </div>
          <p className="text-zinc-500 dark:text-zinc-400">VirusTotal 主要辅助查看安全信誉，并非广告分类。点击外部域名查询链接时，会将这个域名交给对应网站。</p>
        </div>
        <p className="text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">查询记录也包含被拒绝的请求，出现记录不等于访问成功。确认需要拦截后，可直接加入自定义拦截集合，仅匹配这个完整域名。保存沿用自动应用设置，可能短暂重启内核；“已加入”不代表已取得实际拦截结果。</p>
      </ModalBody>
      <ModalFooter><Button size="sm" variant="light" onPress={onClose}>关闭</Button><Button size="sm" className="rounded bg-[#ff5722] text-white" isLoading={blocking} isDisabled={!blockAvailable || listed || blocking} onPress={() => onBlock(domain)}>{listed ? '已加入自定义拦截集合' : '拦截此域名'}</Button></ModalFooter>
    </ModalContent>
  </Modal>;
}
