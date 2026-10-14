import * as React from 'react'
import { cn } from '../lib/utils'

type ChipProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  /** 传入 boolean 时是开关，带 aria-pressed；不传时是普通小按钮。 */
  pressed?: boolean
}

/** 命令卡上的选项开关：选中时显示方点。 */
export const Chip = React.forwardRef<HTMLButtonElement, ChipProps>(({ pressed, className, children, ...props }, ref) => (
  <button
    ref={ref}
    type="button"
    aria-pressed={pressed}
    className={cn(
      'inline-flex h-[26px] shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-line-strong px-2.5 text-[12.5px] text-dim transition-colors hover:text-fg disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:text-dim [&_code]:text-[12px]',
      pressed && 'bg-raised text-fg before:h-1.5 before:w-1.5 before:rounded-[2px] before:bg-fg before:content-[""]',
      className
    )}
    {...props}
  >
    {children}
  </button>
))
Chip.displayName = 'Chip'

type SegmentedProps<T extends string> = {
  value: T
  options: { value: T; label: React.ReactNode; disabled?: boolean; title?: string }[]
  onChange: (value: T) => void
  label: string
  className?: string
}

export function Segmented<T extends string>({ value, options, onChange, label, className }: SegmentedProps<T>) {
  return (
    <div role="group" aria-label={label} className={cn('inline-flex overflow-hidden rounded-md border border-line-strong', className)}>
      {options.map((option, index) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={option.value === value}
          disabled={option.disabled}
          title={option.title}
          onClick={() => onChange(option.value)}
          className={cn(
            'h-[26px] whitespace-nowrap px-2.5 text-[12.5px] text-dim transition-colors hover:text-fg disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:text-dim [&_code]:text-[12px]',
            index > 0 && 'border-l border-line-strong',
            option.value === value && 'bg-raised text-fg'
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}
