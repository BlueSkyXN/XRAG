import * as React from 'react'
import { cn } from '../lib/utils'

type BadgeProps = React.HTMLAttributes<HTMLSpanElement> & {
  tone?: 'neutral' | 'accent' | 'success' | 'warning'
}

export function Badge({ className, tone = 'neutral', ...props }: BadgeProps) {
  const tones = {
    neutral: 'border-slate-200 bg-slate-50 text-slate-600',
    accent: 'border-blue-200 bg-blue-50 text-blue-700',
    success: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    warning: 'border-amber-200 bg-amber-50 text-amber-800'
  }
  return (
    <span
      className={cn(
        'inline-flex items-center rounded border px-2 py-0.5 text-xs font-medium',
        tones[tone],
        className
      )}
      {...props}
    />
  )
}
