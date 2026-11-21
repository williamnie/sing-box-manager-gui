import { Button } from '@nextui-org/react';
import { gatewayClientsApi } from '../api/gatewayClients';
import useRuntimePolling from './runtime/useRuntimePolling';
import { t } from '../i18n';

const policies = { split: '按规则分流', direct: '直连', bypass: '绕过', strict: '严格全代理' };

export default function GatewayClients({ onConfigure }: { onConfigure: (address: string) => void }) {
  const snapshot = useRuntimePolling(gatewayClientsApi.list, 5000);

  return (
    <section className="overflow-hidden rounded-[4px] border border-zinc-200 bg-white dark:border-white/10 dark:bg-[#0b0c10]">
      <div className="flex items-center justify-between gap-3 border-b border-zinc-200 px-4 py-3 dark:border-white/10">
        <div>
          <h2 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{t('最近接入设备')}</h2>
          <p className="mt-1 text-xs leading-relaxed text-zinc-500">{t('设备直接使用本机 DNS 即可按规则分流，无需逐台添加。只有需要单独指定策略时才配置。')}</p>
        </div>
        <Button size="sm" variant="light" isLoading={snapshot.loading} onPress={() => void snapshot.refresh()}>{t('刷新')}</Button>
      </div>
      {snapshot.error ? <p role="alert" className="px-4 py-3 text-xs text-rose-500">{snapshot.error}</p> : null}
      <div className="overflow-x-auto">
        <table className="w-full min-w-[640px] text-left text-xs">
          <thead className="bg-zinc-50 text-zinc-500 dark:bg-white/[0.03]"><tr>
            <th className="px-4 py-3 font-medium">{t('设备 / IP')}</th><th className="px-4 py-3 font-medium">{t('观察到的流量')}</th>
            <th className="px-4 py-3 font-medium">{t('活动连接')}</th><th className="px-4 py-3 font-medium">{t('已保存设备策略')}</th>
            <th className="px-4 py-3 font-medium">{t('最近出现')}</th><th className="px-4 py-3 font-medium">{t('操作')}</th>
          </tr></thead>
          <tbody className="divide-y divide-zinc-100 text-zinc-700 dark:divide-white/5 dark:text-zinc-300">
            {snapshot.data?.clients.map(client => <tr key={client.address}>
              <td className="px-4 py-3"><div>{client.name || t('自动发现')}</div><div className="mt-1 font-mono text-zinc-500">{client.address}</div></td>
              <td className="px-4 py-3">{[client.dns_seen ? 'DNS' : '', client.proxy_seen ? t('代理入站') : ''].filter(Boolean).join(' · ')}</td>
              <td className="px-4 py-3">{snapshot.data?.runtime_available ? client.active_connections : t('暂不可读')}</td>
              <td className="px-4 py-3">{client.configured ? t(policies[client.policy]) : t('默认分流（无需登记）')}</td>
              <td className="whitespace-nowrap px-4 py-3">{new Date(client.last_seen_at).toLocaleTimeString()}</td>
              <td className="px-4 py-3"><Button size="sm" variant="light" isDisabled={client.configured} onPress={() => onConfigure(client.address)}>{client.configured ? t('已配置策略') : t('配置单独策略')}</Button></td>
            </tr>)}
          </tbody>
        </table>
        {!snapshot.data?.clients.length ? <p className="px-4 py-6 text-sm text-zinc-500">{snapshot.loading ? t('正在读取接入记录…') : t('暂未观察到设备；让设备使用本机 DNS 访问网站后会自动显示。')}</p> : null}
      </div>
      <p className="border-t border-zinc-200 px-4 py-3 text-[11px] leading-relaxed text-zinc-500 dark:border-white/10">{t('根据当前连接和近期 DNS / 代理日志显示最多 512 个来源，观察记录保留 24 小时。未出现在列表不影响接入；只使用 DNS 的设备也能显示。策略修改需应用后生效。提高日志级别或重启管理器可能缩短可见历史。')}</p>
    </section>
  );
}
