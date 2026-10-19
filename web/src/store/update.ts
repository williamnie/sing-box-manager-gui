import { create } from 'zustand';
import { authApi, errorMessage, updateApi } from '../api';
import { toast } from '../components/Toast';

export interface UpdateStatus {
  current_version: string;
  auto_check: boolean;
  checking: boolean;
  installing: boolean;
  error: string;
  available: boolean;
  install_blocked: string;
  checked_at?: string;
  latest?: { version: string; name: string; notes: string; url: string; published_at: string };
}

const DISMISS_KEY = 'sbm.update.dismissed';

interface UpdateState {
  status: UpdateStatus | null;
  checking: boolean;
  installing: boolean;
  dismissed: string;
  fetchStatus: () => Promise<void>;
  check: () => Promise<void>;
  setAutoCheck: (autoCheck: boolean) => Promise<void>;
  install: () => Promise<void>;
  dismiss: (version: string) => void;
}

function readDismissed() {
  try { return localStorage.getItem(DISMISS_KEY) || ''; } catch { return ''; }
}

// 安装后管理器会原地重启；等待服务恢复再刷新页面以加载新版前端。
async function waitForRestart() {
  await new Promise((resolve) => setTimeout(resolve, 1500));
  for (let i = 0; i < 60; i++) {
    try {
      await authApi.status();
      window.location.reload();
      return;
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
  toast.error('管理器未在预期时间内恢复，请检查服务状态后手动刷新');
}

export const useUpdateStore = create<UpdateState>((set, get) => ({
  status: null,
  checking: false,
  installing: false,
  dismissed: readDismissed(),

  fetchStatus: async () => {
    try {
      const res = await updateApi.status();
      set({ status: res.data.data });
    } catch {
      // 状态获取失败不打扰用户，下次轮询重试。
    }
  },

  check: async () => {
    set({ checking: true });
    try {
      const res = await updateApi.check();
      const status: UpdateStatus = res.data.data;
      set({ status });
      if (status.available) toast.info(`发现新版本 v${status.latest?.version}`);
      else toast.success('当前已是最新版本');
    } catch (error) {
      toast.error(errorMessage(error, '检查更新失败'));
      void get().fetchStatus();
    } finally {
      set({ checking: false });
    }
  },

  setAutoCheck: async (autoCheck) => {
    try {
      const res = await updateApi.setAutoCheck(autoCheck);
      set({ status: res.data.data });
      toast.success(autoCheck ? '已开启自动检查更新' : '已关闭自动检查更新');
    } catch (error) {
      toast.error(errorMessage(error, '保存失败'));
    }
  },

  install: async () => {
    const version = get().status?.latest?.version;
    if (!version) return;
    set({ installing: true });
    try {
      await updateApi.install(version);
      toast.success(`v${version} 已安装，管理器正在重启…`);
      await waitForRestart();
    } catch (error) {
      toast.error(errorMessage(error, '安装更新失败'));
      void get().fetchStatus();
    } finally {
      set({ installing: false });
    }
  },

  dismiss: (version) => {
    try { localStorage.setItem(DISMISS_KEY, version); } catch { /* 无痕模式下仅本次生效 */ }
    set({ dismissed: version });
  },
}));
