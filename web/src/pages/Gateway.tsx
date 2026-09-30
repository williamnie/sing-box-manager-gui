import { useCallback, useEffect, useState } from 'react';
import { Button, Checkbox, Input, Switch, Tab, Tabs } from '@nextui-org/react';
import { AppSelect, SelectItem } from '../components/AppSelect';
import { Plus, Trash2 } from 'lucide-react';
import { errorMessage, gatewayApi, settingsApi } from '../api';
import { useStore } from '../store';
import type { DevicePolicy, GatewayConfig, Settings } from '../store';
import { toast } from '../components/Toast';
import ConfirmModal from '../components/ConfirmModal';
import ListInput from '../components/ListInput';
import { devicePolicyLabels, splitGroupForNewDevice } from '../utils/rulePolicy';

const policyLabels = devicePolicyLabels;
const defaultGateway: GatewayConfig = {
  access_mode: 'full', fakeip_range: '198.18.0.0/15', dns_source: 'client', static_route_confirmed: false,
  enabled: false, lan_interface: '', lan_cidrs: [], lan_address: '', upstream_gateway: '', uplink_interface: '',
  ipv6_mode: 'disabled', nat: false, exclude_cidrs: [], dns_port: 53,
  dhcp: { enabled: false, range_start: '', range_end: '', lease_time: '12h', reservations: [] },
};
interface GatewayStatus {
  platform: string; role: string; access_mode?: 'full' | 'dns'; enabled: boolean; helper: unknown; warnings: string[];
  devices: { id: string; name: string; addresses: string[]; enabled: boolean; policy: DevicePolicy; outbound: string; dns: string; explanation: string }[];
}

export default function Gateway() {
  const [draft, setDraft] = useState<Settings | null>(null);
  const [status, setStatus] = useState<GatewayStatus | null>(null);
  const [result, setResult] = useState<{ title: string; data: unknown } | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [confirmation, setConfirmation] = useState<'apply' | 'rollback' | 'restart' | null>(null);
  const [saved, setSaved] = useState(false);
  const [checked, setChecked] = useState(false);
  const [revision, setRevision] = useState(0);
  const fetchSettings = useStore((state) => state.fetchSettings);

  const refreshStatus = useCallback(async () => {
    const response = await gatewayApi.status();
    setStatus(response.data.data);
  }, []);

  useEffect(() => {
    let active = true;
    void Promise.all([settingsApi.get(), gatewayApi.status()]).then(([settingsResponse, statusResponse]) => {
      if (!active) return;
      setDraft(settingsResponse.data.data);
      setStatus(statusResponse.data.data);
      setSaved(true);
    }).catch((cause) => { if (active) setError(errorMessage(cause, '无法加载部署设置')); });
    return () => { active = false; };
  }, []);

  const edit = (changes: Partial<Settings>) => {
    setDraft((previous) => previous ? { ...previous, ...changes } : previous);
    setSaved(false); setChecked(false); setResult(null);
  };

  const action = async (name: 'save' | 'preview' | 'check' | 'apply' | 'rollback' | 'restart') => {
    if (!draft) return;
    setBusy(name); setError('');
    try {
      if (name === 'save') {
        const response = await gatewayApi.save(draft);
        if (response.data.data) { setDraft(response.data.data); setRevision((value) => value + 1); }
        await fetchSettings();
        setSaved(true); setChecked(false);
        toast.success('部署草案已保存；系统接管状态未改变');
      } else if (name === 'preview' || name === 'check') {
        const response = name === 'preview' ? await gatewayApi.preview(draft) : await gatewayApi.check(draft);
        setResult({ title: name === 'preview' ? '预期变更与资源归属' : '启用前检查', data: response.data.data });
        // 服务端始终在应用时重新检查；这里只表示用户已阅读检查结果。
        if (name === 'check') setChecked(response.data.data?.ok === true || response.data.data?.ready === true);
      } else if (name === 'restart') {
        await gatewayApi.restartManager(); toast.success('管理服务重启已提交，请稍后重新登录');
      } else {
        const response = name === 'apply' ? await gatewayApi.apply() : await gatewayApi.rollback();
        setResult({ title: name === 'apply' ? '应用结果' : '恢复结果', data: response.data.data });
        const settingsResponse = await settingsApi.get();
        setDraft(settingsResponse.data.data); setRevision((value) => value + 1); setSaved(true); setChecked(false);
        await fetchSettings();
        toast.success(name === 'apply' ? '接入配置已应用，请按验证清单检查实际路径' : '已恢复项目管理的接管资源');
      }
      await refreshStatus();
    } catch (cause) { setError(errorMessage(cause)); }
    finally { setBusy(''); setConfirmation(null); }
  };

  if (!draft) return <div>{error || '正在读取部署配置…'}</div>;
  const gateway = { ...defaultGateway, ...draft.gateway, dhcp: { ...defaultGateway.dhcp, ...draft.gateway?.dhcp } };
  const editGateway = (changes: Partial<GatewayConfig>) => edit({ gateway: {
    ...gateway, ...changes,
    ...(('fakeip_range' in changes || 'lan_address' in changes) ? { static_route_confirmed: false } : {}),
  } });
  const groups = draft.device_groups || [];
  const devices = draft.devices || [];
  const dnsRules = draft.split_dns || [];
  const reservations = gateway.dhcp.reservations || [];
  const linux = status?.platform === 'linux';
  const gatewayRole = draft.deployment_role === 'gateway';
  const dnsBypass = gatewayRole && gateway.access_mode === 'dns';
  const routerDNS = dnsBypass && gateway.dns_source === 'router';
  const modeLabel = dnsBypass ? 'DNS 分流旁路' : '完整网关接管';
  const activeModeLabel = status?.access_mode === 'dns' ? 'DNS 分流旁路' : '完整网关接管';
  const strictGroups = dnsBypass && groups.some((group) => group.policy === 'strict');
  const routerSourceConflict = routerDNS && (devices.some((device) => device.enabled) || dnsRules.some((rule) => rule.source_cidrs?.length));
  const dnsPrerequisites = !dnsBypass || (gateway.static_route_confirmed && !strictGroups && !routerSourceConflict);
  const selectAccessMode = (access_mode: 'full' | 'dns') => {
    editGateway(access_mode === 'dns' ? {
      access_mode, ipv6_mode: 'disabled', nat: false, dns_port: 53,
      dhcp: { ...gateway.dhcp, enabled: false },
    } : { access_mode });
  };
  const addDevice = () => {
    const group = splitGroupForNewDevice(groups, crypto.randomUUID());
    edit({
      device_groups: groups.some((item) => item.id === group.id) ? groups : [...groups, group],
      devices: [...devices, { id: crypto.randomUUID(), name: '', addresses: [], group_id: group.id, enabled: true }],
    });
  };

  return (
    <div className="space-y-6">
      {/* 极客头部与保存草案操作 */}
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4 border-b border-zinc-200/80 dark:border-white/[0.08] pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5 font-mono text-[11px] text-[#ff5722] tracking-wider uppercase font-semibold">
            <span>[ SYSTEM // TOPOLOGY & HARDWARE ]</span>
            <span className="text-zinc-400 dark:text-zinc-600">--</span>
            <span className="text-zinc-500 dark:text-zinc-400">GATEWAY DEPLOYMENT</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white font-sans flex items-center gap-3">
            部署与设备策略
          </h1>
        </div>

        <div className="flex items-center gap-3 font-mono text-xs">
          <Button
            size="sm"
            className="rounded-[3px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-white font-semibold uppercase tracking-wider shadow-geek-glow"
            isLoading={busy === 'save'}
            isDisabled={!!busy || saved}
            onPress={() => action('save')}
          >
            保存部署草案
          </Button>
        </div>
      </div>

      {error ? (
        <div role="alert" className="p-3.5 rounded-[3px] bg-rose-500/10 border border-rose-500/30 text-rose-400 font-mono text-xs whitespace-pre-wrap">
          [ERROR] {error}
        </div>
      ) : null}

      {status?.warnings?.map((warning) => (
        <div key={warning} className="p-3.5 rounded-[3px] bg-amber-500/10 border border-amber-500/30 text-amber-800 dark:text-amber-300 font-mono text-xs">
          [WARNING] {warning}
        </div>
      ))}

      {/* 部署角色与系统卡片 */}
      <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] overflow-hidden">
        <div className="h-10 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between">
          <span className="font-mono text-xs text-white font-semibold uppercase tracking-wider">
            // 部署角色与目标系统 (TARGET ARCHITECTURE)
          </span>
          <div className="flex items-center gap-2 font-mono text-[10px]">
            <span className={`px-2 py-0.5 rounded-[2px] border ${saved ? 'bg-white/[0.04] border-white/[0.08] text-zinc-400' : 'bg-amber-500/10 border-amber-500/30 text-amber-800 dark:text-amber-300'}`}>
              {saved ? 'DRAFT: SAVED' : 'DRAFT: MODIFIED'}
            </span>
            <span className={`px-2 py-0.5 rounded-[2px] border ${status?.enabled ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400' : 'bg-white/[0.04] border-white/[0.08] text-zinc-500'}`}>
              {status?.enabled ? `${activeModeLabel}: ENABLED` : 'STATUS: STANDBY'}
            </span>
          </div>
        </div>

        <div className="p-5 space-y-4 font-mono text-xs">
          <div className="grid md:grid-cols-2 gap-4">
            <AppSelect
              label="部署角色"
              isDisabled={status?.enabled}
              selectedKeys={[draft.deployment_role || 'desktop']}
              onChange={(event) => edit({ deployment_role: event.target.value as 'desktop' | 'gateway' })}
            >
              <SelectItem key="desktop">单机代理（macOS / Linux）</SelectItem>
              <SelectItem key="gateway">家庭接入网关（Linux）</SelectItem>
            </AppSelect>

            <div className="p-3 rounded-[3px] bg-black/40 border border-white/[0.06] text-zinc-400 space-y-1">
              <p>HOST OS: <span className="text-white font-semibold">{status?.platform || 'UNKNOWN'}</span></p>
              <p className="text-[11px] text-zinc-500">部署角色由人工指定；即便宿主系统为 Linux 也不会自动接管网关。</p>
            </div>
          </div>

          {gatewayRole && (
            <div className="space-y-3 pt-2 border-t border-white/[0.06]">
              <AppSelect
                label="家庭接入方式"
                isDisabled={status?.enabled}
                selectedKeys={[gateway.access_mode || 'full']}
                onChange={(event) => selectAccessMode(event.target.value as 'full' | 'dns')}
              >
                <SelectItem key="dns" description="主路由下发 DNS 和 FakeIP 静态路由，终端默认网关不变">
                  DNS 分流旁路 (推荐低侵入)
                </SelectItem>
                <SelectItem key="full" description="设备默认网关指向本机，接管完整转发路径">
                  完整网关接管 (透明代理)
                </SelectItem>
              </AppSelect>
              <p className="text-[11px] text-zinc-500">
                切换模式前须先恢复当前接管资源；选择 DNS 分流旁路会关闭草案中的 NAT、DHCP 和 IPv6 接管。
              </p>
            </div>
          )}

          <p className="text-[11px] text-zinc-500">
            单机模式保留 mixed / TUN 配置，不执行 Linux 家庭接管操作。保存、预览、检查不会启用接管；只有确认应用后才改变系统。
          </p>

          {status?.enabled && (
            <div className="p-3 rounded-[3px] bg-amber-500/10 border border-amber-500/20 text-amber-800 dark:text-amber-300 text-xs">
              [ALERT] 当前系统已存在生效的接管规则。更改模式、接口或地址池前，请先在下方执行「恢复接管资源」。
            </div>
          )}

          {!linux && (
            <div className="p-3 rounded-[3px] bg-white/[0.03] border border-white/[0.06] text-zinc-400 text-xs">
              当前系统非 Linux，不能执行本地网络接管指令，但可准备草案并查看生成的配置预览。
            </div>
          )}
        </div>
      </div>

      {/* DNS 旁路前提检查卡片 */}
      {dnsBypass && (
        <div className="rounded-[4px] border border-cyan-500/30 bg-[#070b10] overflow-hidden">
          <div className="h-9 px-4 border-b border-cyan-500/20 bg-cyan-950/20 flex items-center justify-between text-cyan-300 font-mono text-xs font-semibold">
            <span>// DNS 分流旁路 · 外部网络拓扑前提声明</span>
            <span className="text-[10px] text-cyan-400/80">PREREQUISITES</span>
          </div>
          <div className="p-5 space-y-3 font-mono text-xs text-zinc-300">
            <ol className="list-decimal pl-5 space-y-1.5 leading-relaxed text-zinc-400">
              <li>为旁路设备保留固定 LAN IP。主路由继续运行 DHCP，并在 LAN DNS 设置中填入此设备 IP 作为首选 DNS。</li>
              <li>在主路由添加静态路由：<strong className="text-cyan-300">{gateway.fakeip_range || defaultGateway.fakeip_range} &rarr; {gateway.lan_address || '旁路 LAN 地址'}</strong>。</li>
              <li>启用本实例后，终端重新连接 WiFi 或更新 DHCP 租约即可无感分流。</li>
            </ol>
            <div className="pt-2">
              <Checkbox
                isSelected={gateway.static_route_confirmed === true}
                onValueChange={(static_route_confirmed) => editGateway({ static_route_confirmed })}
                classNames={{ label: "text-xs font-mono text-zinc-200" }}
              >
                我已确认主路由已配置上述 FakeIP 静态路由
              </Checkbox>
            </div>
          </div>
        </div>
      )}

      {/* 详细配置 Tabs */}
      <div key={revision}>
        <Tabs
          aria-label="网关配置分类"
          variant="underlined"
          classNames={{
            tabList: "gap-6 border-b border-white/[0.08] p-0 font-mono text-xs",
            cursor: "w-full bg-[#ff5722]",
            tab: "max-w-fit px-0 h-10 text-zinc-400 data-[selected=true]:text-white",
          }}
        >
          <Tab key="network" title="// 接口与接管 (INTERFACES)">
            <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] p-5 space-y-4 mt-3">
              <div className="grid md:grid-cols-2 gap-4">
                <Input label="LAN 接口" placeholder="enp1s0" value={gateway.lan_interface} onValueChange={(value) => editGateway({ lan_interface: value })} variant="bordered" classNames={{ inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]", label: "font-mono text-xs text-zinc-400", input: "font-mono text-xs text-white" }} />
                <Input label="LAN 地址" placeholder="192.168.1.2" value={gateway.lan_address} onValueChange={(value) => editGateway({ lan_address: value })} variant="bordered" classNames={{ inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]", label: "font-mono text-xs text-zinc-400", input: "font-mono text-xs text-white" }} />
                <Input label="上游主路由网关" placeholder="192.168.1.1" value={gateway.upstream_gateway} onValueChange={(value) => editGateway({ upstream_gateway: value })} variant="bordered" classNames={{ inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]", label: "font-mono text-xs text-zinc-400", input: "font-mono text-xs text-white" }} />
                <Input label="上联接口" description="单网卡旁路可与 LAN 接口相同" value={gateway.uplink_interface} onValueChange={(value) => editGateway({ uplink_interface: value })} variant="bordered" classNames={{ inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]", label: "font-mono text-xs text-zinc-400", input: "font-mono text-xs text-white" }} />
                <ListInput label="LAN 网段 (CIDR)" values={gateway.lan_cidrs || []} onChange={(value) => editGateway({ lan_cidrs: value })} placeholder="192.168.1.0/24" description="受管网段，每行一个" />
                <ListInput label="接管排除网段" values={gateway.exclude_cidrs || []} onChange={(value) => editGateway({ exclude_cidrs: value })} description={dnsBypass ? '不能与 FakeIP 池重叠' : '例如 Docker 内部网络'} />
                {dnsBypass ? (
                  <Input label="FakeIP IPv4 地址池" value={gateway.fakeip_range || defaultGateway.fakeip_range} placeholder="198.18.0.0/15" description="须与主路由静态路由一致" onValueChange={(fakeip_range) => editGateway({ fakeip_range })} variant="bordered" classNames={{ inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]", label: "font-mono text-xs text-zinc-400", input: "font-mono text-xs text-white" }} />
                ) : (
                  <AppSelect label="IPv6 策略" selectedKeys={[gateway.ipv6_mode]} onChange={(event) => editGateway({ ipv6_mode: event.target.value as GatewayConfig['ipv6_mode'] })}>
                    <SelectItem key="disabled">禁用网关转发 IPv6</SelectItem>
                    <SelectItem key="proxy">接管 IPv6</SelectItem>
                  </AppSelect>
                )}
                {!dnsBypass && (
                  <div className="p-3 rounded-[3px] bg-black/40 border border-white/[0.06] flex items-center justify-between">
                    <div>
                      <span className="font-mono text-xs text-zinc-200">启用源地址 NAT</span>
                      <p className="text-[10px] font-mono text-zinc-500">仅在回程路由需要时启用；默认保留真实 IP</p>
                    </div>
                    <Switch size="sm" isSelected={gateway.nat} onValueChange={(nat) => editGateway({ nat })} classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }} />
                  </div>
                )}
              </div>
            </div>
          </Tab>

          <Tab key="devices" title="// 设备与策略 (DEVICES)">
            <div className="space-y-4 mt-3">
              {/* 分组管理 */}
              <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] overflow-hidden">
                <div className="h-9 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between">
                  <span className="font-mono text-xs text-white font-semibold">// 策略分组 (POLICY GROUPS)</span>
                  <Button size="sm" className="rounded-[2px] font-mono text-xs bg-white/[0.06] text-zinc-200 hover:text-white" startContent={<Plus size={12} />} onPress={() => edit({ device_groups: [...groups, { id: crypto.randomUUID(), name: '', policy: 'split', outbound: 'Proxy' }] })}>
                    添加分组
                  </Button>
                </div>
                <div className="p-4 space-y-3 font-mono text-xs">
                  {groups.map((group) => (
                    <div key={group.id} className="grid md:grid-cols-[1fr_1fr_1fr_auto] gap-3 p-3 rounded-[3px] bg-[#07080b] border border-white/[0.05] items-center">
                      <Input label="组名" value={group.name} onValueChange={(name) => edit({ device_groups: groups.map((item) => item.id === group.id ? { ...item, name } : item) })} variant="bordered" classNames={{ inputWrapper: "bg-black/60 border-white/[0.1] rounded-[2px]" }} />
                      <AppSelect label="路由策略" disabledKeys={dnsBypass ? ['strict'] : []} selectedKeys={[group.policy]} onChange={(event) => edit({ device_groups: groups.map((item) => item.id === group.id ? { ...item, policy: event.target.value as DevicePolicy } : item) })}>
                        {Object.entries(policyLabels).map(([value, label]) => <SelectItem key={value}>{label}</SelectItem>)}
                      </AppSelect>
                      <Input label="指定代理出站" placeholder="Proxy" isDisabled={group.policy !== 'strict'} value={group.outbound || ''} onValueChange={(outbound) => edit({ device_groups: groups.map((item) => item.id === group.id ? { ...item, outbound } : item) })} variant="bordered" classNames={{ inputWrapper: "bg-black/60 border-white/[0.1] rounded-[2px]" }} />
                      <Button aria-label={`删除分组 ${group.name}`} isIconOnly size="sm" className="size-8 rounded-[2px] bg-rose-500/10 text-rose-400 hover:bg-rose-500/20" isDisabled={devices.some((device) => device.group_id === group.id) || reservations.some((reservation) => reservation.group === group.id)} onPress={() => edit({ device_groups: groups.filter((item) => item.id !== group.id) })}>
                        <Trash2 size={14} />
                      </Button>
                    </div>
                  ))}
                </div>
              </div>

              {/* 设备列表 */}
              <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] overflow-hidden">
                <div className="h-9 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between">
                  <span className="font-mono text-xs text-white font-semibold">// 受管设备列表 (LAN HOSTS)</span>
                  <Button size="sm" className="rounded-[2px] font-mono text-xs bg-white/[0.06] text-zinc-200 hover:text-white" startContent={<Plus size={12} />} onPress={addDevice}>
                    添加设备
                  </Button>
                </div>
                <div className="p-4 space-y-3 font-mono text-xs">
                  {devices.map((device) => (
                    <div key={device.id} className="p-3 rounded-[3px] bg-[#07080b] border border-white/[0.05] space-y-3">
                      <div className="grid md:grid-cols-3 gap-3">
                        <Input label="设备名称" value={device.name} onValueChange={(name) => edit({ devices: devices.map((item) => item.id === device.id ? { ...item, name } : item) })} variant="bordered" classNames={{ inputWrapper: "bg-black/60 border-white/[0.1] rounded-[2px]" }} />
                        <ListInput label="IP / CIDR" values={device.addresses || []} onChange={(addresses) => edit({ devices: devices.map((item) => item.id === device.id ? { ...item, addresses } : item) })} />
                        <AppSelect label="所属分组" selectedKeys={[device.group_id]} onChange={(event) => edit({ devices: devices.map((item) => item.id === device.id ? { ...item, group_id: event.target.value } : item) })}>
                          {groups.map((group) => <SelectItem key={group.id}>{group.name || '未命名分组'}</SelectItem>)}
                        </AppSelect>
                      </div>
                      <div className="flex justify-between items-center pt-2 border-t border-white/[0.04]">
                        <Switch size="sm" isSelected={device.enabled} onValueChange={(enabled) => edit({ devices: devices.map((item) => item.id === device.id ? { ...item, enabled } : item) })} classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}>
                          启用来源策略
                        </Switch>
                        <Button size="sm" className="rounded-[2px] font-mono text-xs bg-rose-500/10 text-rose-400 hover:bg-rose-500/20" onPress={() => edit({ devices: devices.filter((item) => item.id !== device.id) })}>
                          删除设备
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </Tab>

          <Tab key="dns" title="// 局域网 DNS (LAN DNS)">
            <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] p-5 space-y-4 mt-3 font-mono text-xs">
              <div className="grid md:grid-cols-2 gap-4">
                <Input label="LAN DNS 监听端口" type="number" min={1} max={65535} isReadOnly={dnsBypass} value={String(gateway.dns_port)} description={dnsBypass ? 'DHCP 下发的 DNS 地址使用标准 UDP 53 端口' : undefined} onValueChange={(value) => editGateway({ dns_port: Number(value) })} variant="bordered" classNames={{ inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]" }} />
                <div className="p-3 rounded-[3px] bg-black/40 border border-white/[0.06] text-zinc-400">
                  <p>监听配置的 LAN 地址。启用前检查 53 端口与已有 mosdns / dnsmasq 冲突。</p>
                </div>
              </div>

              {dnsBypass && (
                <div className="space-y-3 pt-2">
                  <AppSelect label="终端 DNS 接入路径" selectedKeys={[gateway.dns_source || 'client']} onChange={(event) => editGateway({ dns_source: event.target.value as 'client' | 'router' })}>
                    <SelectItem key="client" description="保留终端真实来源，可使用设备差异策略">DHCP 直接下发旁路 DNS（推荐）</SelectItem>
                    <SelectItem key="router" description="DNS 来源合并为主路由，不支持终端来源差异策略">主路由作为 DNS 转发器</SelectItem>
                  </AppSelect>
                </div>
              )}
            </div>
          </Tab>
        </Tabs>
      </div>

      {/* 检查与安全应用控制台 */}
      <div className="rounded-[4px] border border-white/[0.09] bg-[#0b0c10] overflow-hidden shadow-lg">
        <div className="h-10 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between">
          <span className="font-mono text-xs text-white font-semibold uppercase tracking-wider">
            // 安全验证与系统应用流水线 (EXECUTION PIPELINE)
          </span>
          <span className="font-mono text-[10px] text-zinc-500">STAGE: PRE-FLIGHT VERIFIED</span>
        </div>

        <div className="p-5 space-y-4 font-mono text-xs">
          <p className="text-zinc-400 text-xs">
            标准发布流程：保存草案 &rarr; 预览变更 &rarr; 预检内核环境 &rarr; 确认启用应用。应用操作会自动重检端口、策略路由与防火墙规则。
          </p>

          <div className="flex flex-wrap items-center gap-2.5 pt-2">
            <Button
              size="sm"
              className="rounded-[3px] font-mono text-xs border border-white/[0.1] bg-[#12141d] hover:bg-[#181c28] text-zinc-200"
              isDisabled={!!busy}
              isLoading={busy === 'preview'}
              onPress={() => action('preview')}
            >
              预览变更差异
            </Button>
            <Button
              size="sm"
              className="rounded-[3px] font-mono text-xs border border-cyan-500/30 bg-cyan-500/10 text-cyan-300 hover:bg-cyan-500/20"
              isDisabled={!!busy || !saved || !linux || !gatewayRole || !dnsPrerequisites}
              isLoading={busy === 'check'}
              onPress={() => action('check')}
            >
              启用前安全预检
            </Button>
            <Button
              size="sm"
              className="rounded-[3px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-black font-semibold uppercase tracking-wider shadow-geek-glow"
              isDisabled={!!busy || !saved || !checked || !linux || !gatewayRole || !dnsPrerequisites}
              onPress={() => setConfirmation('apply')}
            >
              确认启用并应用
            </Button>
            <Button
              size="sm"
              className="rounded-[3px] font-mono text-xs border border-rose-500/20 bg-rose-500/10 text-rose-300 hover:bg-rose-500/20"
              isDisabled={!!busy || !linux}
              onPress={() => setConfirmation('rollback')}
            >
              恢复接管资源
            </Button>
            <Button
              size="sm"
              className="rounded-[3px] font-mono text-xs border border-white/[0.08] bg-white/[0.04] text-zinc-400 hover:text-white"
              isDisabled={!!busy || !linux || !status?.enabled}
              onPress={() => setConfirmation('restart')}
            >
              重启服务
            </Button>
          </div>

          {result && (
            <div className="mt-4 rounded-[3px] border border-white/[0.08] bg-[#050608] overflow-hidden">
              <div className="h-8 px-3 border-b border-white/[0.06] bg-[#0c0d12] flex items-center justify-between text-[11px] text-[#ff5722]">
                <span>{result.title}</span>
                <span className="text-zinc-600">JSON STREAM</span>
              </div>
              <pre className="p-4 text-[11px] text-zinc-300 max-h-96 overflow-auto whitespace-pre-wrap break-all selection:bg-[#ff5722]">
                {JSON.stringify(result.data, null, 2)}
              </pre>
            </div>
          )}
        </div>
      </div>

      {/* 设备出站策略预期卡片 */}
      <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] overflow-hidden">
        <div className="h-9 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between">
          <span className="font-mono text-xs text-white font-semibold">// 设备出站预期解析 (EXPECTED POLICIES)</span>
        </div>
        <div className="p-4 space-y-2.5 font-mono text-xs">
          {status?.devices?.length ? (
            status.devices.map((device) => (
              <div key={device.id} className="p-3 rounded-[3px] bg-[#07080b] border border-white/[0.05] flex flex-col md:flex-row md:items-center justify-between gap-2">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-bold text-white">{device.name}</span>
                    <span className="text-[10px] px-1.5 py-0.5 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-400">
                      {device.addresses?.join(', ')}
                    </span>
                    {device.enabled === false && <span className="text-[10px] text-zinc-600">DISABLED</span>}
                  </div>
                  <p className="text-[11px] text-zinc-500 mt-1">{device.explanation}</p>
                </div>
                <div className="shrink-0 text-right">
                  <span className="text-xs px-2 py-0.5 rounded-[2px] bg-[#ff5722]/10 border border-[#ff5722]/30 text-[#ff5722]">
                    {policyLabels[device.policy] || device.policy} &rarr; {device.outbound}
                  </span>
                </div>
              </div>
            ))
          ) : (
            <p className="text-zinc-500 text-center py-4">暂无配置的具体设备策略，所有流量走默认路由规则。</p>
          )}
        </div>
      </div>

      <ConfirmModal
        isOpen={confirmation !== null}
        title={confirmation === 'restart' ? '重启系统管理服务' : confirmation === 'apply' ? `启用${modeLabel}` : '恢复接管资源'}
        busy={!!busy}
        onClose={() => setConfirmation(null)}
        onConfirm={() => { if (confirmation) void action(confirmation); }}
        confirmLabel={confirmation === 'restart' ? '重启管理服务' : confirmation === 'apply' ? '启用并应用' : '执行恢复'}
      >
        <p className="font-mono text-xs leading-relaxed text-zinc-300">
          {confirmation === 'restart'
            ? '将通过特权辅助服务重启固定的管理服务。受管内核继续运行，管理会话将失效，请稍后重新登录。'
            : confirmation === 'apply'
            ? (dnsBypass ? `将在本机启用 LAN DNS 与仅针对 ${gateway.fakeip_range} 的 TCP / UDP TProxy、专用防火墙和策略路由。主路由须下发旁路 DNS 并已有 FakeIP 静态路由；终端默认网关保持原值。` : '这会修改此管理服务所在 Linux 主机的网关接管、转发、项目防火墙及可选 DHCP 服务。请确认测试设备、回程路径和维护窗口已经准备好。')
            : '将恢复此项目记录的系统状态并关闭接管。请安排主路由 DNS / 路由回退与终端缓存更新。'}
        </p>
      </ConfirmModal>
    </div>
  );
}
