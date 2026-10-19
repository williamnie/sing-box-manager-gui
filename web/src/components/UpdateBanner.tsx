import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@nextui-org/react';
import { ArrowUpCircle, X } from 'lucide-react';
import { useUpdateStore } from '../store/update';
import UpdateInstallButton from './UpdateInstallButton';

const POLL_MS = 30 * 60 * 1000;

// 后端定时检查 GitHub 最新发布；这里只读取结果并提示，忽略按版本记住。
export default function UpdateBanner() {
  const { status, dismissed, fetchStatus, dismiss } = useUpdateStore();

  useEffect(() => {
    void fetchStatus();
    const poll = window.setInterval(fetchStatus, POLL_MS);
    return () => window.clearInterval(poll);
  }, [fetchStatus]);

  const latest = status?.latest;
  if (!status?.available || !latest || dismissed === latest.version) return null;

  return (
    <div role="status" className="mb-4 rounded border border-primary/40 bg-primary/10 px-4 py-3 text-xs">
      <div className="flex flex-wrap items-center gap-3">
        <ArrowUpCircle className="size-4 shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <p className="font-semibold text-zinc-900 dark:text-zinc-100">
            发现新版本 v{latest.version}
            <span className="ml-2 font-mono font-normal text-zinc-500">当前 v{status.current_version}</span>
          </p>
          <p className="mt-0.5 text-zinc-600 dark:text-zinc-400">
            {status.install_blocked || '不会自动下载，点击“立即更新”后才会安装。'}
          </p>
        </div>
        <div className="flex gap-2">
          <Button as={Link} to="/settings#section-update" size="sm" variant="flat">查看更新</Button>
          <UpdateInstallButton />
          <Button isIconOnly size="sm" variant="light" aria-label="忽略此版本" title="忽略此版本" onPress={() => dismiss(latest.version)}>
            <X className="size-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
