import React from 'react';
import { Select as NextUISelect, type SelectProps as NextUISelectProps, SelectItem } from '@nextui-org/react';

export { SelectItem };

export interface AppSelectProps<T extends object = object> extends Omit<NextUISelectProps<T>, 'children'> {
  children: React.ReactNode | ((item: T) => React.ReactNode);
}

/**
 * 统一优化的 Select 组件：
 * 1. 禁用 Popover 弹跳动画 (disableAnimation: true)，彻底根除下拉框展开时的抖动、晃动与重新测量闪烁
 * 2. 统一内外字体族、字号与行高规范，消除内部选项字体与外层表单脱节的问题
 * 3. 完美适配浅色/深色主题，支持高对比度清晰展现
 */
export function AppSelect<T extends object = object>({
  classNames,
  popoverProps,
  listboxProps,
  size = 'sm',
  variant = 'bordered',
  children,
  ...props
}: AppSelectProps<T>) {
  const mergedPopoverProps = {
    disableAnimation: true,
    offset: 4,
    ...popoverProps,
  };

  const mergedListboxProps = {
    disableAnimation: true,
    itemClasses: {
      base: [
        'rounded-[3px]',
        'text-xs',
        'font-sans',
        'transition-colors',
        'data-[hover=true]:bg-default-200/60',
        'dark:data-[hover=true]:bg-white/[0.08]',
        'data-[selected=true]:bg-[#ff5722]/15',
        'data-[selected=true]:text-[#ff5722]',
        'data-[selected=true]:font-medium',
      ],
      title: 'text-xs font-sans text-foreground font-medium',
      description: 'text-[11px] font-sans text-default-500',
    },
    ...listboxProps,
  };

  const mergedClassNames = {
    ...classNames,
    base: `max-w-full font-sans ${classNames?.base || ''}`,
    label: `text-xs font-sans font-medium text-default-600 dark:text-zinc-400 ${classNames?.label || ''}`,
    trigger: `rounded-[3px] border border-default-300 dark:border-white/[0.1] bg-default-100/50 dark:bg-black/40 hover:border-[#ff5722]/50 data-[focus=true]:border-[#ff5722] text-foreground ${classNames?.trigger || ''}`,
    value: `text-xs font-sans text-foreground ${classNames?.value || ''}`,
    popoverContent: `rounded-[4px] border border-default-200 dark:border-white/[0.1] bg-content1 shadow-xl text-foreground font-sans ${classNames?.popoverContent || ''}`,
    listbox: `p-1 font-sans ${classNames?.listbox || ''}`,
  };

  return (
    <NextUISelect
      size={size}
      variant={variant}
      popoverProps={mergedPopoverProps}
      listboxProps={mergedListboxProps}
      classNames={mergedClassNames}
      {...(props as any)}
    >
      {children as any}
    </NextUISelect>
  );
}

export default AppSelect;
