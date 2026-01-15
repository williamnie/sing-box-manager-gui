import { useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { LayoutDashboard, Globe, FileText, Settings, Activity, ScrollText, Network, Sun, Moon, Monitor } from 'lucide-react';
import { Button, Dropdown, DropdownTrigger, DropdownMenu, DropdownItem } from '@nextui-org/react';
import { useStore } from '../store';
import { useTheme } from '../hooks/useTheme';

const menuItems = [
  { path: '/', icon: LayoutDashboard, label: '仪表盘' },
  { path: '/subscriptions', icon: Globe, label: '节点' },
  { path: '/connections', icon: Network, label: '连接' },
  { path: '/rules', icon: FileText, label: '规则' },
  { path: '/logs', icon: ScrollText, label: '日志' },
  { path: '/settings', icon: Settings, label: '设置' },
];

interface LayoutProps {
  children: React.ReactNode;
}

export default function Layout({ children }: LayoutProps) {
  const location = useLocation();
  const { settings, fetchSettings, serviceStatus, fetchServiceStatus } = useStore();
  const { theme, setTheme, isDark } = useTheme();

  useEffect(() => {
    // 并行请求，加速初始化
    Promise.all([
      !settings && fetchSettings(),
      !serviceStatus && fetchServiceStatus(),
    ]);
  }, []);

  const clashApiPort = settings?.clash_api_port || 9091;

  const themeIcon = theme === 'system' ? Monitor : isDark ? Moon : Sun;

  return (
    <div className="flex min-h-screen bg-gray-50 dark:bg-gray-900">
      {/* 侧边栏 */}
      <aside className="w-64 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 fixed h-full overflow-y-auto">
        <div className="p-6">
          <h1 className="text-xl font-bold text-gray-800 dark:text-white flex items-center gap-2">
            <Activity className="w-6 h-6 text-primary" />
            SingBox Manager
          </h1>
        </div>

        <nav className="px-4">
          {menuItems.map((item) => {
            const isActive = location.pathname === item.path;
            const Icon = item.icon;

            return (
              <Link
                key={item.path}
                to={item.path}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg mb-1 transition-colors ${
                  isActive
                    ? 'bg-primary text-white'
                    : 'text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
                }`}
              >
                <Icon className="w-5 h-5" />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>

        {/* 底部链接 */}
        <div className="absolute bottom-0 left-0 right-0 p-4 border-t border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
          <div className="flex items-center justify-between mb-3">
            <a
              href={(() => {
                const baseUrl = `http://127.0.0.1:${clashApiPort}/ui/`;
                const params = new URLSearchParams({
                  hostname: '127.0.0.1',
                  port: String(clashApiPort),
                  label: 'SingBox Manager'
                });
                if (settings?.clash_api_secret) {
                  params.set('secret', settings.clash_api_secret);
                }
                return `${baseUrl}?${params.toString()}`;
              })()}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-gray-500 dark:text-gray-400 hover:text-primary transition-colors"
            >
              打开 Zashboard
            </a>
            <Dropdown>
              <DropdownTrigger>
                <Button isIconOnly size="sm" variant="light">
                  {themeIcon === Monitor ? <Monitor className="w-4 h-4" /> : 
                   themeIcon === Moon ? <Moon className="w-4 h-4" /> : 
                   <Sun className="w-4 h-4" />}
                </Button>
              </DropdownTrigger>
              <DropdownMenu
                aria-label="主题选择"
                selectionMode="single"
                selectedKeys={new Set([theme])}
                onSelectionChange={(keys) => {
                  const selected = Array.from(keys)[0] as 'light' | 'dark' | 'system';
                  if (selected) setTheme(selected);
                }}
              >
                <DropdownItem key="light" startContent={<Sun className="w-4 h-4" />}>
                  浅色
                </DropdownItem>
                <DropdownItem key="dark" startContent={<Moon className="w-4 h-4" />}>
                  深色
                </DropdownItem>
                <DropdownItem key="system" startContent={<Monitor className="w-4 h-4" />}>
                  跟随系统
                </DropdownItem>
              </DropdownMenu>
            </Dropdown>
          </div>
          {serviceStatus?.sbm_version && (
            <p className="text-center text-xs text-gray-400 dark:text-gray-500">
              v{serviceStatus.sbm_version}
            </p>
          )}
        </div>
      </aside>

      {/* 主内容区 */}
      <main className="flex-1 p-8 overflow-auto ml-64 h-screen">
        <div className="h-full">
          {children}
        </div>
      </main>
    </div>
  );
}
