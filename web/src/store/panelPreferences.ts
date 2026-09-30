import { create } from 'zustand';

export interface PanelPreferences {
  refreshInterval: 1000 | 2000 | 5000;
  proxySort: 'default' | 'name' | 'delay';
  hiddenGroups: string[];
  collapsedGroups: string[];
  testURL: string;
  testTimeout: number;
  connectionColumns: string[];
  connectionColumnWidths: Record<string, number>;
  logBuffer: number;
  logFollow: boolean;
}
export const defaultPanelPreferences: PanelPreferences = {
  refreshInterval: 2000, proxySort: 'default', hiddenGroups: [], collapsedGroups: [],
  testURL: 'https://www.gstatic.com/generate_204', testTimeout: 5000,
  connectionColumns: ['source', 'destination', 'network', 'chains', 'speed', 'traffic', 'started_at'],
  connectionColumnWidths: {}, logBuffer: 1000, logFollow: true,
};
const key = 'sbm-panel-preferences-v1';
function normalize(value: Partial<PanelPreferences>): PanelPreferences {
  const result = { ...defaultPanelPreferences, ...value };
  if (![1000, 2000, 5000].includes(result.refreshInterval)) result.refreshInterval = 2000;
  if (!['default', 'name', 'delay'].includes(result.proxySort)) result.proxySort = 'default';
  for (const field of ['hiddenGroups', 'collapsedGroups', 'connectionColumns'] as const) {
    result[field] = Array.isArray(result[field]) ? result[field].filter((v): v is string => typeof v === 'string').slice(0, 2000) : defaultPanelPreferences[field];
  }
  result.testTimeout = Number.isFinite(result.testTimeout) ? Math.min(10000, Math.max(100, result.testTimeout)) : 5000;
  result.logBuffer = Number.isFinite(result.logBuffer) ? Math.min(10000, Math.max(100, result.logBuffer)) : 1000;
  result.logFollow = typeof result.logFollow === 'boolean' ? result.logFollow : true;
  if (typeof result.testURL !== 'string' || result.testURL.length > 2048) result.testURL = defaultPanelPreferences.testURL;
  result.connectionColumnWidths = Object.fromEntries(Object.entries(result.connectionColumnWidths || {}).filter(([, v]) => typeof v === 'number' && Number.isFinite(v)).map(([k, v]) => [k, Math.min(480, Math.max(80, v))]));
  return result;
}
function read(): PanelPreferences {
  try { return normalize(JSON.parse(localStorage.getItem(key) || '{}')); } catch { return { ...defaultPanelPreferences }; }
}
const usePreferencesStore = create<{ preferences: PanelPreferences; update: (patch: Partial<PanelPreferences>) => void }>((set) => ({
  preferences: read(),
  update: (patch) => set((state) => {
    const preferences = normalize({ ...state.preferences, ...patch });
    try { localStorage.setItem(key, JSON.stringify(preferences)); } catch { /* 隐私模式下继续使用本次会话偏好。 */ }
    return { preferences };
  }),
}));
export function usePanelPreferences(): [PanelPreferences, (patch: Partial<PanelPreferences>) => void] {
  return [usePreferencesStore((s) => s.preferences), usePreferencesStore((s) => s.update)];
}
