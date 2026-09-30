import { create } from 'zustand';

export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

interface ThemeState {
  themeMode: ThemeMode;
  resolvedTheme: ResolvedTheme;
  setThemeMode: (mode: ThemeMode) => void;
}

const STORAGE_KEY = 'sbm-theme-mode';

const getSystemTheme = (): ResolvedTheme => {
  if (typeof window === 'undefined') return 'dark';
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
};

const resolveTheme = (mode: ThemeMode): ResolvedTheme => {
  if (mode === 'system') {
    return getSystemTheme();
  }
  return mode;
};

const applyThemeToDOM = (resolved: ResolvedTheme) => {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (resolved === 'dark') {
    root.classList.add('dark');
    root.classList.remove('light');
    root.setAttribute('data-theme', 'dark');
    root.style.colorScheme = 'dark';
  } else {
    root.classList.add('light');
    root.classList.remove('dark');
    root.setAttribute('data-theme', 'light');
    root.style.colorScheme = 'light';
  }
};

export const useTheme = create<ThemeState>((set, get) => {
  const initialMode =
    typeof window !== 'undefined'
      ? (localStorage.getItem(STORAGE_KEY) as ThemeMode) || 'system'
      : 'system';
  const initialResolved = resolveTheme(initialMode);

  if (typeof window !== 'undefined') {
    applyThemeToDOM(initialResolved);

    // 监听系统主题变化
    if (window.matchMedia) {
      const media = window.matchMedia('(prefers-color-scheme: dark)');
      const listener = () => {
        if (get().themeMode === 'system') {
          const newResolved = getSystemTheme();
          applyThemeToDOM(newResolved);
          set({ resolvedTheme: newResolved });
        }
      };
      if (media.addEventListener) {
        media.addEventListener('change', listener);
      } else if ((media as any).addListener) {
        (media as any).addListener(listener);
      }
    }
  }

  return {
    themeMode: initialMode,
    resolvedTheme: initialResolved,
    setThemeMode: (mode: ThemeMode) => {
      if (typeof window !== 'undefined') {
        localStorage.setItem(STORAGE_KEY, mode);
      }
      const newResolved = resolveTheme(mode);
      applyThemeToDOM(newResolved);
      set({ themeMode: mode, resolvedTheme: newResolved });
    },
  };
});
