import { useState } from 'react';
import type { FormEvent } from 'react';
import { Button, Input } from '@nextui-org/react';
import { authApi, errorMessage } from '../api';
import { toast } from './Toast';

export default function PasswordSettings() {
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const currentPassword = String(data.get('current_password') || '');
    const newPassword = String(data.get('new_password') || '');
    const passwordBytes = new TextEncoder().encode(newPassword).length;
    if (passwordBytes < 12 || passwordBytes > 72) {
      setError('新密码需为 12–72 字节（中文等字符可能占多个字节）');
      return;
    }
    if (newPassword !== data.get('confirm_password')) {
      setError('两次输入的新密码不一致');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await authApi.changePassword(currentPassword, newPassword);
      toast.success('密码已修改，请使用新密码重新登录');
      window.dispatchEvent(new Event('sbm:unauthorized'));
    } catch (cause) {
      setError(errorMessage(cause, '修改密码失败'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded border border-zinc-200 bg-white p-5 dark:border-white/10 dark:bg-zinc-950">
      <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">管理密码</h2>
      <p className="mt-1 text-xs leading-relaxed text-zinc-500">
        可以设置自己好记的密码或短语，长度为 12–72 字节。修改后所有设备需重新登录。
      </p>
      <form id="change-password" method="post" onSubmit={submit} className="mt-4 space-y-4">
        <input type="text" name="username" autoComplete="username" value="admin" readOnly hidden />
        <div className="grid gap-4 sm:grid-cols-3">
          <Input name="current_password" label="当前密码" type="password" autoComplete="current-password" isRequired isDisabled={busy} />
          <Input name="new_password" label="新密码" type="password" autoComplete="new-password" isRequired isDisabled={busy} />
          <Input name="confirm_password" label="确认新密码" type="password" autoComplete="new-password" isRequired isDisabled={busy} />
        </div>
        {error && <p role="alert" className="text-xs text-danger">{error}</p>}
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" size="sm" color="primary" isLoading={busy}>修改管理密码</Button>
          <p className="text-xs text-zinc-500">单独生效；可由浏览器密码管理器保存和自动填充。</p>
        </div>
      </form>
    </section>
  );
}
