import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader } from '@nextui-org/react';
import type { ReactNode } from 'react';

export default function ConfirmModal({ title, children, isOpen, busy = false, onClose, onConfirm, confirmLabel = '确认' }: {
  title: string; children: ReactNode; isOpen: boolean; busy?: boolean; onClose: () => void; onConfirm: () => void; confirmLabel?: string;
}) {
  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      isDismissable={!busy}
      hideCloseButton={busy}
      classNames={{
        base: "bg-[#0b0c10] border border-white/[0.12] text-zinc-100 rounded-[4px] shadow-[0_16px_50px_rgba(0,0,0,0.85)]",
        header: "border-b border-white/[0.08] font-mono text-sm tracking-wide text-white py-3.5 px-5",
        body: "py-5 px-5 font-sans text-xs text-zinc-300 leading-relaxed",
        footer: "border-t border-white/[0.08] py-3 px-5 bg-black/30",
      }}
    >
      <ModalContent>
        <ModalHeader className="flex items-center gap-2">
          <span className="size-2 rounded-full bg-[#ff5722]"></span>
          <span>{title}</span>
        </ModalHeader>
        <ModalBody>{children}</ModalBody>
        <ModalFooter className="gap-2">
          <Button
            size="sm"
            variant="flat"
            isDisabled={busy}
            onPress={onClose}
            className="rounded-[2px] font-mono text-xs border border-white/[0.1] bg-white/[0.05] text-zinc-300 hover:text-white"
          >
            取消 (ESC)
          </Button>
          <Button
            size="sm"
            isLoading={busy}
            onPress={onConfirm}
            className="rounded-[2px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-black font-semibold uppercase tracking-wider"
          >
            {confirmLabel}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}

