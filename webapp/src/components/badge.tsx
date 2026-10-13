import * as React from 'react'
import { cn } from '../lib/utils'

type BadgeProps = React.HTMLAttributes<HTMLSpanElement> & {
  tone?: 'neutral' | 'accent' | 'success' | 'warning'
}

export function Badge({ className, tone = 'neutral', ...props }: BadgeProps) {
  const tones = {
    neutral: 'border-border bg-muted text-muted-foreground',
    accent: 'border-border bg-accent-soft text-accent',
    success: 'border-border bg-muted text-success',
    warning: 'border-border bg-highlight text-warning'
  }
  return (
    <span
      className={cn(
        'inline-flex items-center rounded border px-1.5 py-0.5 text-xs font-medium tabular-nums',
        tones[tone],
        className
      )}
      {...props}
    />
  )
}
