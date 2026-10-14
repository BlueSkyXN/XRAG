import * as React from 'react'
import { cn } from '../lib/utils'

export function Panel({ className, ...props }: React.HTMLAttributes<HTMLElement>) {
  return <section className={cn('flex min-w-0 flex-col rounded-[10px] border border-line bg-panel', className)} {...props} />
}

type PanelHeaderProps = {
  title: React.ReactNode
  meta?: React.ReactNode
  actions?: React.ReactNode
  className?: string
}

export function PanelHeader({ title, meta, actions, className }: PanelHeaderProps) {
  return (
    <header className={cn('flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-2', className)}>
      <h2 className="text-[13px] font-semibold text-fg">{title}</h2>
      {meta && <div className="flex min-w-0 flex-wrap items-center gap-x-3 text-xs text-dim">{meta}</div>}
      {actions && <div className="ml-auto flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}

export function PanelBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('p-4', className)} {...props} />
}

/** 空状态：说明现在是什么情况、下一步可以做什么。 */
export function Empty({ title, children, action, className }: { title: React.ReactNode; children?: React.ReactNode; action?: React.ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col items-start gap-2 px-4 py-8 text-[13px]', className)}>
      <p className="font-medium text-fg">{title}</p>
      {children && <div className="max-w-xl leading-relaxed text-dim">{children}</div>}
      {action && <div className="mt-1 flex flex-wrap gap-2">{action}</div>}
    </div>
  )
}

export function ErrorLine({ children, className }: { children: React.ReactNode; className?: string }) {
  if (!children) return null
  return (
    <p role="alert" className={cn('rounded-md border border-err/30 bg-err/[0.06] px-3 py-2 text-[13px] text-err', className)}>
      {children}
    </p>
  )
}

export function Note({ children, className }: { children: React.ReactNode; className?: string }) {
  return <p className={cn('text-xs leading-relaxed text-faint', className)}>{children}</p>
}

/** 键值清单。值为空时显示破折号，不隐藏字段。 */
export function KeyValues({ rows, className }: { rows: [React.ReactNode, React.ReactNode][]; className?: string }) {
  return (
    <dl className={cn('grid grid-cols-[minmax(6rem,auto)_minmax(0,1fr)] gap-x-4 gap-y-2 text-[13px]', className)}>
      {rows.map(([key, value], index) => (
        <React.Fragment key={index}>
          <dt className="text-dim">{key}</dt>
          <dd className="min-w-0 break-words text-fg">{value ?? '—'}</dd>
        </React.Fragment>
      ))}
    </dl>
  )
}

export function Loading({ label = '读取中…' }: { label?: string }) {
  return <p className="px-4 py-6 text-[13px] text-dim">{label}</p>
}
