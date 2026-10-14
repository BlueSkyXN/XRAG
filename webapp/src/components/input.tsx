import * as React from 'react'
import { cn } from '../lib/utils'

const control =
  'w-full rounded-md border border-line-strong bg-bg text-[13px] text-fg outline-none transition-colors placeholder:text-faint focus:border-dim disabled:cursor-not-allowed disabled:opacity-50'

const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement> & { mono?: boolean }>(
  ({ className, mono, ...props }, ref) => <input ref={ref} className={cn(control, 'h-8 px-2.5', mono && 'font-mono', className)} {...props} />
)
Input.displayName = 'Input'

const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement> & { mono?: boolean }>(
  ({ className, mono, ...props }, ref) => (
    <textarea ref={ref} className={cn(control, 'min-h-16 px-2.5 py-2 leading-relaxed', mono && 'font-mono text-[12.5px]', className)} {...props} />
  )
)
Textarea.displayName = 'Textarea'

const Select = React.forwardRef<HTMLSelectElement, React.SelectHTMLAttributes<HTMLSelectElement>>(({ className, ...props }, ref) => (
  <select ref={ref} className={cn(control, 'h-8 px-2', className)} {...props} />
))
Select.displayName = 'Select'

type FieldProps = {
  label: React.ReactNode
  hint?: React.ReactNode
  className?: string
  children: React.ReactNode
}

/** 标签在上、说明在下的表单项。整块是 label，点击标签即聚焦控件。 */
function Field({ label, hint, className, children }: FieldProps) {
  return (
    <label className={cn('block space-y-1.5', className)}>
      <span className="block text-xs text-dim">{label}</span>
      {children}
      {hint && <span className="block text-xs leading-relaxed text-faint">{hint}</span>}
    </label>
  )
}

function Check({ label, className, ...props }: React.InputHTMLAttributes<HTMLInputElement> & { label: React.ReactNode }) {
  return (
    <label className={cn('flex items-start gap-2 text-[13px] text-fg-soft', props.disabled && 'opacity-50', className)}>
      <input type="checkbox" className="mt-[3px] h-3.5 w-3.5 shrink-0" {...props} />
      <span>{label}</span>
    </label>
  )
}

export { Input, Textarea, Select, Field, Check }
