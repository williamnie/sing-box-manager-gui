import { useTheme, type ThemeMode } from '../utils/theme';
import { Sun, Moon, Laptop } from 'lucide-react';

export default function ThemeToggle() {
  const { themeMode, resolvedTheme, setThemeMode } = useTheme();

  const options: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
    { mode: 'system', label: '跟随系统', icon: Laptop },
    { mode: 'light', label: '浅色模式', icon: Sun },
    { mode: 'dark', label: '深色模式', icon: Moon },
  ];

  return (
    <div className="flex items-center p-0.5 rounded-[3px] border border-zinc-200 dark:border-white/[0.08] bg-zinc-100/90 dark:bg-black/40">
      {options.map((opt) => {
        const Icon = opt.icon;
        const isActive = themeMode === opt.mode;

        return (
          <button
            key={opt.mode}
            type="button"
            title={`${opt.label} ${opt.mode === 'system' ? `(${resolvedTheme === 'dark' ? '系统暗色' : '系统浅色'})` : ''}`}
            onClick={() => setThemeMode(opt.mode)}
            className={`flex items-center gap-1.5 px-2 py-1 rounded-[2px] text-xs font-mono transition-all cursor-pointer ${
              isActive
                ? 'bg-[#ff5722] text-white font-semibold shadow-xs'
                : 'text-zinc-600 hover:text-zinc-950 dark:text-zinc-400 dark:hover:text-zinc-200'
            }`}
          >
            <Icon className="size-3.5" />
            <span className="hidden xl:inline text-[11px]">{opt.label}</span>
          </button>
        );
      })}
    </div>
  );
}
