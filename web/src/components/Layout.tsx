import { useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import {
  LayoutDashboard,
  Globe,
  FileText,
  Settings,
  ScrollText,
  Network,
  FileCheck,
  LogOut,
  Route,
  Activity,
  Menu,
  X,
  Cpu,
} from 'lucide-react';
import { useStore } from '../store';
import { authApi, errorMessage } from '../api';
import { toast } from './Toast';
import ThemeToggle from './ThemeToggle';

const menuItems = [
  { path: '/', icon: LayoutDashboard, label: '仪表盘', code: '01' },
  { path: '/proxies', icon: Route, label: '代理', code: '02' },
  { path: '/connections', icon: Activity, label: '连接', code: '03' },
  { path: '/subscriptions', icon: Globe, label: '节点管理', code: '04' },
  { path: '/rules', icon: FileText, label: '分流规则', code: '05' },
  { path: '/gateway', icon: Network, label: '部署与设备', code: '06' },
  { path: '/configuration', icon: FileCheck, label: '配置审阅', code: '07' },
  { path: '/logs', icon: ScrollText, label: '实时日志', code: '08' },
  { path: '/settings', icon: Settings, label: '系统设置', code: '09' },
];

interface LayoutProps {
  children: React.ReactNode;
}

export default function Layout({ children }: LayoutProps) {
  const [navigationOpen, setNavigationOpen] = useState(false);
  const location = useLocation();
  const { settings, fetchSettings, serviceStatus, fetchServiceStatus } = useStore();

  useEffect(() => {
    if (!settings) {
      fetchSettings();
    }
    if (!serviceStatus) {
      fetchServiceStatus();
    }
  }, [settings, serviceStatus, fetchSettings, fetchServiceStatus]);

  const isRunning = serviceStatus?.running ?? false;

  return (
    <div className="flex min-h-screen bg-[#f6f7fb] dark:bg-[#070709] text-zinc-900 dark:text-[#ededed] transition-colors duration-150">
      {/* 侧边栏 */}
      {navigationOpen && <button aria-label="关闭导航" className="fixed inset-0 z-20 bg-black/40 md:hidden" onClick={() => setNavigationOpen(false)} />}
      <aside className={`w-64 bg-white dark:bg-[#090a0f] border-r border-zinc-200/80 dark:border-white/[0.08] fixed h-full flex flex-col z-30 select-none transition-transform duration-150 shadow-sm dark:shadow-none ${navigationOpen ? 'translate-x-0' : '-translate-x-full'} md:translate-x-0`}>
        {/* 顶部 Logo 终端区块 */}
        <div className="p-5 border-b border-zinc-200/80 dark:border-white/[0.08] bg-white/90 dark:bg-[#090a0f]/80 backdrop-blur-md">
          <div className="flex items-center justify-between">
            <Link to="/" onClick={() => setNavigationOpen(false)} className="flex items-center gap-2.5 group">
              <div className="size-7 rounded-[3px] bg-[#ff5722] flex items-center justify-center text-white font-black font-mono text-sm shadow-[0_0_12px_rgba(255,87,34,0.35)] group-hover:bg-[#ff6e40] transition-colors">
                SB
              </div>
              <div className="flex flex-col">
                <span className="font-mono text-xs font-bold tracking-wider text-zinc-900 dark:text-white uppercase flex items-center gap-1.5">
                  SingBox
                  <span className="text-[#ff5722] text-[10px] tracking-normal font-normal">[DEV]</span>
                </span>
                <span className="text-[10px] font-mono text-zinc-500 tracking-tight">manager // console</span>
              </div>
            </Link>

            {/* 极客状态指示器 */}
            <Link
              to="/"
              title={isRunning ? '服务运行中 (点击查看)' : '服务未启动 (点击启动)'}
              className="flex items-center gap-1.5 px-2 py-0.5 rounded-[2px] border border-zinc-200 dark:border-white/[0.08] bg-zinc-100 dark:bg-black/40 hover:border-[#ff5722]/50 transition-colors"
            >
              <span className="relative flex size-2">
                {isRunning ? (
                  <>
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full size-2 bg-emerald-500"></span>
                  </>
                ) : (
                  <span className="relative inline-flex rounded-full size-2 bg-rose-500"></span>
                )}
              </span>
              <span className="font-mono text-[9px] uppercase tracking-wider text-zinc-600 dark:text-zinc-400 font-semibold">
                {isRunning ? 'UP' : 'OFF'}
              </span>
            </Link>
          </div>
        </div>

        {/* 导航菜单 */}
        <nav className="p-3 space-y-1 overflow-y-auto flex-1 font-mono text-xs">
          <div className="px-3 py-2 text-[10px] uppercase tracking-widest text-zinc-400 dark:text-zinc-600 font-mono">
            // navigation
          </div>
          {menuItems.map((item) => {
            const isActive =
              location.pathname === item.path ||
              (item.path === '/configuration' && location.pathname.startsWith('/configuration/'));
            const Icon = item.icon;

            return (
              <Link
                key={item.path}
                to={item.path}
                onClick={() => setNavigationOpen(false)}
                className={`group flex items-center justify-between px-3 py-2.5 rounded-[3px] transition-all duration-150 border ${
                  isActive
                    ? 'bg-orange-500/10 text-[#ff5722] border-orange-500/30 border-l-2 border-l-[#ff5722] font-semibold dark:bg-white/[0.06] dark:text-white dark:border-white/[0.12]'
                    : 'text-zinc-600 hover:text-zinc-950 hover:bg-zinc-100/80 dark:text-zinc-400 dark:hover:text-zinc-100 dark:hover:bg-white/[0.03] border-transparent'
                }`}
              >
                <div className="flex items-center gap-3">
                  <Icon
                    className={`size-4 transition-colors ${
                      isActive
                        ? 'text-[#ff5722]'
                        : 'text-zinc-400 group-hover:text-zinc-600 dark:text-zinc-500 dark:group-hover:text-zinc-300'
                    }`}
                  />
                  <span className="font-sans font-medium text-sm">{item.label}</span>
                </div>
                <span
                  className={`text-[10px] font-mono tracking-wider ${
                    isActive
                      ? 'text-[#ff5722]'
                      : 'text-zinc-400 group-hover:text-zinc-600 dark:text-zinc-600 dark:group-hover:text-zinc-500'
                  }`}
                >
                  {item.code}
                </span>
              </Link>
            );
          })}
        </nav>

        {/* 底部极客控制台与系统元数据 */}
        <div className="p-3 border-t border-zinc-200/80 dark:border-white/[0.08] bg-zinc-50/80 dark:bg-black/40 space-y-2">
          {/* 退出与版本 */}
          <div className="pt-1 flex items-center justify-between px-1">
            <button
              onClick={async () => {
                try {
                  await authApi.logout();
                  window.dispatchEvent(new Event('sbm:unauthorized'));
                } catch (error) {
                  toast.error(errorMessage(error, '退出失败'));
                }
              }}
              className="flex items-center gap-1.5 text-[11px] font-mono text-zinc-500 hover:text-rose-600 dark:hover:text-rose-400 transition-colors cursor-pointer py-1"
            >
              <LogOut className="size-3" />
              <span>LOGOUT</span>
            </button>
            {serviceStatus?.sbm_version && (
              <span className="font-mono text-[10px] text-zinc-500 dark:text-zinc-500 bg-zinc-200/60 dark:bg-white/[0.03] px-1.5 py-0.5 rounded-[2px] border border-zinc-200 dark:border-white/[0.05]">
                v{serviceStatus.sbm_version}
              </span>
            )}
          </div>
        </div>
      </aside>

      {/* 主界面区域 */}
      <div className="min-w-0 flex-1 md:ml-64 flex flex-col min-h-screen">
        {/* 顶部极客状态条 */}
        <header className="h-12 border-b border-zinc-200/80 dark:border-white/[0.08] bg-white/90 dark:bg-[#070709]/80 backdrop-blur-md px-3 md:px-6 flex items-center justify-between sticky top-0 z-10 select-none transition-colors duration-150">
          <div className="flex items-center gap-2 text-xs font-mono text-zinc-500 dark:text-zinc-400">
            <button aria-label={navigationOpen ? '关闭导航菜单' : '打开导航菜单'} aria-expanded={navigationOpen} className="p-1.5 md:hidden" onClick={() => setNavigationOpen(!navigationOpen)}>{navigationOpen ? <X className="size-4" /> : <Menu className="size-4" />}</button>
            <span className="text-zinc-400 dark:text-zinc-600">SBM</span>
            <span className="text-zinc-300 dark:text-zinc-700">/</span>
            <span className="text-[#ff5722] font-semibold">{location.pathname === '/' ? 'dashboard' : location.pathname.slice(1)}</span>
          </div>

          <div className="flex items-center gap-3 sm:gap-4 text-xs font-mono">
            {/* 部署角色标签 */}
            <div className="hidden md:flex items-center gap-2 px-2.5 py-1 rounded-[2px] border border-zinc-200 dark:border-white/[0.06] bg-zinc-100/80 dark:bg-[#0d0e14] text-zinc-500 dark:text-zinc-400">
              <span className="size-1.5 rounded-full bg-[#ff5722]"></span>
              <span className="text-[11px]">
                ROLE: <span className="text-zinc-900 dark:text-zinc-200 font-semibold">{settings?.deployment_role === 'gateway' ? 'GATEWAY' : 'STANDALONE'}</span>
              </span>
            </div>

            {/* 内核状态胶囊 */}
            <Link
              to="/"
              className="flex items-center gap-1.5 text-[11px] px-2 py-0.5 rounded-[2px] border border-zinc-200 dark:border-white/[0.08] bg-zinc-100/80 dark:bg-black/30 hover:border-[#ff5722]/50 text-zinc-700 dark:text-zinc-400 transition-colors"
            >
              <Cpu className="size-3 text-zinc-400 dark:text-zinc-500" />
              <span>
                CORE: <b className={isRunning ? 'text-emerald-600 dark:text-emerald-400 font-semibold' : 'text-rose-600 dark:text-rose-400 font-semibold'}>{isRunning ? (serviceStatus?.version ? 'SING-BOX OK' : 'ONLINE') : 'OFFLINE'}</b>
              </span>
            </Link>

            {/* 主题切换器 */}
            <ThemeToggle />
          </div>
        </header>

        {/* 页面主内容 */}
        <main className="min-w-0 flex-1 p-3 sm:p-6 md:p-8 bg-[#f6f7fb] dark:bg-[#070709] bg-grid-tech transition-colors duration-150">
          <div className="max-w-7xl mx-auto">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
