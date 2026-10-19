import { useState } from 'react';
import { Button } from '@nextui-org/react';
import { Download } from 'lucide-react';
import ConfirmModal from './ConfirmModal';
import { useUpdateStore } from '../store/update';

// 唯一的安装入口：必须经用户确认后才下载替换管理器。
export default function UpdateInstallButton({ className = '' }: { className?: string }) {
  const { status, installing, install } = useUpdateStore();
  const [confirm, setConfirm] = useState(false);
  if (!status?.available || !status.latest) return null;

  return (
    <>
      <Button
        size="sm"
        color="primary"
        className={className}
        isDisabled={!!status.install_blocked}
        isLoading={installing}
        startContent={installing ? null : <Download className="size-3.5" />}
        onPress={() => setConfirm(true)}
      >
        {installing ? '正在更新' : '立即更新'}
      </Button>
      <ConfirmModal
        isOpen={confirm}
        busy={installing}
        title={`更新到 v${status.latest.version}`}
        confirmLabel="下载并重启"
        onClose={() => setConfirm(false)}
        onConfirm={async () => {
          await install();
          setConfirm(false);
        }}
      >
        <p>
          将从 GitHub Releases 下载 v{status.latest.version}，校验 SHA-256 后替换当前管理器（v{status.current_version}）并自动重启。
        </p>
        <p className="mt-2 text-zinc-500">
          重启期间管理界面会短暂断开，sing-box 代理保持运行。
        </p>
      </ConfirmModal>
    </>
  );
}
