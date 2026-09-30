import { usePanelPreferences, defaultPanelPreferences } from '../store/panelPreferences';

const field = 'w-full rounded border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 dark:border-white/10 dark:bg-zinc-950 dark:text-zinc-100';
export default function PanelPreferences() {
  const [preferences, update] = usePanelPreferences();
  return <section className="rounded border border-zinc-200 bg-white p-5 dark:border-white/10 dark:bg-zinc-950">
    <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div><h2 className="font-semibold text-zinc-900 dark:text-zinc-100">面板偏好</h2><p className="mt-1 text-xs text-zinc-500">仅保存在当前浏览器，立即生效。不会应用配置或重启内核。</p></div>
      <button type="button" onClick={() => update(defaultPanelPreferences)} className="text-xs text-[#ff5722] hover:underline">恢复面板默认值</button>
    </div>
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      <label className="space-y-2 text-xs text-zinc-600 dark:text-zinc-400"><span>运行状态刷新频率</span><select className={field} value={preferences.refreshInterval} onChange={e => update({ refreshInterval: Number(e.target.value) as 1000 | 2000 | 5000 })}><option value={1000}>1 秒</option><option value={2000}>2 秒</option><option value={5000}>5 秒</option></select></label>
      <label className="space-y-2 text-xs text-zinc-600 dark:text-zinc-400"><span>代理节点默认排序</span><select className={field} value={preferences.proxySort} onChange={e => update({ proxySort: e.target.value as 'default' | 'name' | 'delay' })}><option value="default">配置顺序</option><option value="name">名称</option><option value="delay">延迟</option></select></label>
      <label className="space-y-2 text-xs text-zinc-600 dark:text-zinc-400"><span>日志显示缓存</span><select className={field} value={preferences.logBuffer} onChange={e => update({ logBuffer: Number(e.target.value) })}>{[500, 1000, 3000, 10000].map(n => <option key={n} value={n}>{n} 行</option>)}</select></label>
      <label className="space-y-2 text-xs text-zinc-600 dark:text-zinc-400 sm:col-span-2"><span>手动延迟测试地址（公网 HTTPS）</span><input type="url" className={field} value={preferences.testURL} onChange={e => update({ testURL: e.target.value })} maxLength={2048} /></label>
      <label className="space-y-2 text-xs text-zinc-600 dark:text-zinc-400"><span>单节点测试超时</span><select className={field} value={preferences.testTimeout} onChange={e => update({ testTimeout: Number(e.target.value) })}>{[1000, 3000, 5000, 10000].map(n => <option key={n} value={n}>{n / 1000} 秒</option>)}</select></label>
    </div>
    <div className="mt-4 flex flex-wrap items-center gap-4 text-xs text-zinc-600 dark:text-zinc-400">
      <label className="flex items-center gap-2"><input type="checkbox" className="accent-[#ff5722]" checked={preferences.logFollow} onChange={e => update({ logFollow: e.target.checked })} />新日志自动跟随</label>
      {preferences.hiddenGroups.length > 0 && <button type="button" className="text-[#ff5722]" onClick={() => update({ hiddenGroups: [] })}>显示全部 {preferences.hiddenGroups.length} 个隐藏组</button>}
      <span>自动测速组的周期和地址仍在节点管理中配置。</span>
    </div>
  </section>;
}
