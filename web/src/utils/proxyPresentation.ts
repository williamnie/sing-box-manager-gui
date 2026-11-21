import type { RuntimeProxy } from '../api/runtime';
import { t } from '../i18n';

export interface ProxyContext { importedTags: Set<string>; filters: Map<string, string>; hasImported: boolean; managedOnly?: boolean }
export function proxyPresentation(proxy: RuntimeProxy, context: ProxyContext) {
  const tag = proxy.tag;
  if (context.importedTags.has(tag)) return { name: tag, source: t('导入配置'), description: t('独立配置快照，更新或删除订阅不会同步修改此组。'), auxiliary: false };
  if (context.filters.has(tag)) return { name: tag, source: t('自定义过滤器'), description: t('成员在「节点管理 → 过滤器」中编辑。'), auxiliary: false };
  const prefix = context.hasImported ? 'Managed ' : '';
  if (tag === `${prefix}Auto`) return { name: t('订阅自动测速'), source: t('节点管理'), description: t('从全部启用的订阅和手动节点中自动选择出口。'), auxiliary: true };
  if (tag === `${prefix}Proxy`) return { name: prefix ? t('订阅节点选择') : 'Proxy', source: t('节点管理'), description: prefix ? t('选择自动测速、国家组或自定义过滤器；仅影响使用此组的规则。') : t('使用节点管理中启用的订阅和手动节点。'), auxiliary: false };
  if (tag === `${prefix}Final`) return { name: t('默认出口候选组'), source: t('节点管理'), description: t('仅在分流规则或默认出口引用此组时生效。'), auxiliary: true };
  if (tag === 'GLOBAL') return { name: 'GLOBAL', source: proxy.selectable ? t('节点管理') : t('内核'), description: proxy.selectable ? t('直连、拒绝、全部分组和节点。') : t('内核出口汇总。'), auxiliary: false };
  if (tag === 'DIRECT') return { name: t('DIRECT · 直连'), source: t('系统动作'), description: '', auxiliary: false };
  if (tag === 'REJECT') return { name: t('REJECT · 拒绝'), source: t('系统动作'), description: '', auxiliary: false };
  if (context.managedOnly && proxy.members?.length) return { name: tag, source: t('节点管理'), description: t('该国家或地区的启用节点。'), auxiliary: false };
  return { name: tag, source: t('运行配置'), description: /urltest/i.test(proxy.type) ? t('根据延迟自动选择成员，不能手动指定。') : t('按本组当前选择转交给下一级分组或节点。'), auxiliary: /urltest/i.test(proxy.type) };
}

export function proxyGroupRank(tag: string): number {
  return tag === 'GLOBAL' ? 0 : tag === 'Proxy' ? 1 : 2;
}
