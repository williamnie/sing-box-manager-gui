import { useState } from 'react';
import { Button } from '@nextui-org/react';
import { Download } from 'lucide-react';
import ConfirmModal from './ConfirmModal';
import { useUpdateStore } from '../store/update';
import { t } from '../i18n';

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
        {installing ? t('正在更新') : t('立即更新')}
      </Button>
      <ConfirmModal
        isOpen={confirm}
        busy={installing}
        title={t('更新到 v{p1}', { p1: status.latest.version })}
        confirmLabel={t('下载并重启')}
        onClose={() => setConfirm(false)}
        onConfirm={async () => {
          await install();
          setConfirm(false);
        }}
      >
        <p>
          {t('将从 GitHub Releases 下载 v{p1}，校验 SHA-256 后替换当前管理器（v{p2}）并自动重启。', { p1: status.latest.version, p2: status.current_version })}
        </p>
        <p className="mt-2 text-zinc-500">{t('重启期间管理界面会短暂断开，sing-box 代理保持运行。')}</p>
      </ConfirmModal>
    </>
  );
}
