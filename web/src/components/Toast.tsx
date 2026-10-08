import { CheckCircle, XCircle, AlertCircle, X } from 'lucide-react';
import { create } from 'zustand';

interface Toast {
  id: string;
  type: 'success' | 'error' | 'info';
  message: string;
  duration?: number;
}

interface ToastStore {
  toasts: Toast[];
  addToast: (toast: Omit<Toast, 'id'>) => void;
  removeToast: (id: string) => void;
}

export const useToast = create<ToastStore>((set) => ({
  toasts: [],
  addToast: (toast) => {
    const id = Math.random().toString(36).substring(7);
    set((state) => ({ toasts: [...state.toasts, { ...toast, id }] }));
    setTimeout(() => {
      set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) }));
    }, toast.duration || 3000);
  },
  removeToast: (id) => set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) })),
}));

// 便捷方法
export const toast = {
  success: (message: string) => useToast.getState().addToast({ type: 'success', message }),
  error: (message: string, duration = 5000) => useToast.getState().addToast({ type: 'error', message, duration }),
  info: (message: string) => useToast.getState().addToast({ type: 'info', message }),
};

// Toast 单个项目组件
const ToastItem = ({ toast, onClose }: { toast: Toast; onClose: () => void }) => {
  const icons = {
    success: <CheckCircle className="size-4 text-emerald-500 dark:text-emerald-400 shrink-0" />,
    error: <XCircle className="size-4 text-rose-500 dark:text-rose-400 shrink-0" />,
    info: <AlertCircle className="size-4 text-[#ff5722] shrink-0" />,
  };

  const borderStyles = {
    success:
      'border-emerald-500/30 dark:border-emerald-500/40 shadow-[0_4px_16px_rgba(16,185,129,0.12),0_1px_3px_rgba(0,0,0,0.05)] dark:shadow-[0_0_15px_rgba(16,185,129,0.15)]',
    error:
      'border-rose-500/30 dark:border-rose-500/40 shadow-[0_4px_16px_rgba(244,63,94,0.12),0_1px_3px_rgba(0,0,0,0.05)] dark:shadow-[0_0_15px_rgba(244,63,94,0.15)]',
    info:
      'border-[#ff5722]/30 dark:border-[#ff5722]/40 shadow-[0_4px_16px_rgba(255,87,34,0.12),0_1px_3px_rgba(0,0,0,0.05)] dark:shadow-[0_0_15px_rgba(255,87,34,0.15)]',
  };

  const tagColors = {
    success: 'text-emerald-600 dark:text-emerald-400',
    error: 'text-rose-600 dark:text-rose-400',
    info: 'text-[#ff5722]',
  };

  const tagLabels = {
    success: 'OK',
    error: 'ERR',
    info: 'SYS',
  };

  return (
    <div
      className={`flex items-center gap-3 px-3.5 py-2.5 rounded-[4px] border bg-white/95 dark:bg-[#0d0e14]/95 backdrop-blur-md ${borderStyles[toast.type]} animate-slide-in select-none`}
    >
      {icons[toast.type]}
      <span className={`font-mono text-[10px] uppercase tracking-wider font-bold shrink-0 ${tagColors[toast.type]}`}>
        [{tagLabels[toast.type]}]
      </span>
      <span className="flex-1 text-xs text-zinc-800 dark:text-zinc-100 font-medium whitespace-pre-wrap leading-relaxed">
        {toast.message}
      </span>
      <button
        onClick={onClose}
        aria-label="关闭提示"
        className="p-1 hover:bg-zinc-100 dark:hover:bg-white/[0.1] rounded-[2px] transition-colors cursor-pointer text-zinc-400 hover:text-zinc-700 dark:text-zinc-400 dark:hover:text-white shrink-0"
      >
        <X className="size-3" />
      </button>
    </div>
  );
};

// Toast 容器组件
export const ToastContainer = () => {
  const { toasts, removeToast } = useToast();

  if (toasts.length === 0) return null;

  return (
    <div className="fixed top-5 right-5 z-[9999] flex flex-col gap-2 max-w-md w-full pointer-events-auto">
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} onClose={() => removeToast(t.id)} />
      ))}
    </div>
  );
};
