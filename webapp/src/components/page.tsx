import * as React from 'react'
import { CurrentPageContext, pageById } from '../shell/nav'
import { cn } from '../lib/utils'
import { CopyButton } from './code'

type PageHeaderProps = {
  title: React.ReactNode
  desc?: React.ReactNode
  /** 同一操作的 CLI 写法。网页和 CLI/MCP 调用的是同一组接口。 */
  cli?: string
  actions?: React.ReactNode
}

export function PageHeader({ title, desc, cli, actions }: PageHeaderProps) {
  const current = React.useContext(CurrentPageContext)
  const Icon = pageById[current].icon
  return (
    <header className="flex flex-wrap items-start gap-x-6 gap-y-3">
      <span
        aria-hidden="true"
        className="page-title-icon mt-0.5 shrink-0"
        data-icon-tone={current === 'sources' ? 'dark' : 'light'}
        style={{ '--hue': `var(--hue-${current})` } as React.CSSProperties}
      >
        <Icon className="h-3.5 w-3.5" strokeWidth={1.8} />
      </span>
      <div className="min-w-0 flex-1">
        <h1 className="text-[17px] font-semibold tracking-tight">{title}</h1>
        {desc && <p className="mt-0.5 max-w-3xl text-[13px] leading-relaxed text-dim">{desc}</p>}
        {cli && (
          <div className="mt-2 flex max-w-full items-center gap-2 font-mono text-[12px] text-faint">
            <span aria-hidden="true">❯</span>
            <code className="min-w-0 truncate text-dim" title={cli}>
              {cli}
            </code>
            <CopyButton text={cli} className="h-5 px-1.5" />
          </div>
        )}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}

export function Page({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-4 py-4 sm:px-5', className)} {...props} />
}
