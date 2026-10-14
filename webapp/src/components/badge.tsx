import * as React from 'react'
import type { Route } from '../lib/api'
import { cn } from '../lib/utils'

export type Tone = 'neutral' | 'ok' | 'warn' | 'err' | 'off' | 'kw' | 'sem' | 'gr'

const tones: Record<Tone, string> = {
  neutral: 'border-line-strong text-dim',
  ok: 'border-ok/30 bg-ok/10 text-ok',
  warn: 'border-warn/30 bg-warn/10 text-warn',
  err: 'border-err/35 bg-err/10 text-err',
  off: 'border-line-strong text-faint',
  kw: 'border-transparent bg-kw/10 text-kw',
  sem: 'border-transparent bg-sem/10 text-sem',
  gr: 'border-transparent bg-gr/10 text-gr'
}

export function Badge({ tone = 'neutral', dot, className, children, ...props }: React.HTMLAttributes<HTMLSpanElement> & { tone?: Tone; dot?: boolean }) {
  return (
    <span className={cn('inline-flex h-[18px] shrink-0 items-center gap-1.5 whitespace-nowrap rounded border px-1.5 text-[11.5px] leading-none', tones[tone], className)} {...props}>
      {dot && <span className="h-1.5 w-1.5 rounded-full bg-current" />}
      {children}
    </span>
  )
}

const routeInfo: Record<Route, { label: string; tone: Tone }> = {
  keyword: { label: '关键词', tone: 'kw' },
  semantic: { label: '语义', tone: 'sem' },
  graph: { label: '图', tone: 'gr' },
  external: { label: '外部', tone: 'neutral' }
}

/** 结果由哪条检索路线找到。颜色只在这里和执行流水线里使用。 */
export function RouteTag({ route }: { route: Route }) {
  const info = routeInfo[route] ?? { label: route, tone: 'neutral' as Tone }
  return (
    <Badge tone={info.tone} dot>
      {info.label}
    </Badge>
  )
}

export const jobStates: Record<string, { label: string; tone: Tone }> = {
  queued: { label: '排队中', tone: 'warn' },
  running: { label: '运行中', tone: 'warn' },
  succeeded: { label: '成功', tone: 'ok' },
  partial: { label: '部分完成', tone: 'warn' },
  failed: { label: '失败', tone: 'err' },
  cancelled: { label: '已取消', tone: 'off' }
}

export function StateBadge({ state }: { state: string }) {
  const info = jobStates[state] ?? { label: state, tone: 'neutral' as Tone }
  return (
    <Badge tone={info.tone} dot className={state === 'running' ? '[&>span:first-child]:animate-pulse' : undefined}>
      {info.label}
    </Badge>
  )
}

export function Dot({ tone, className }: { tone: 'ok' | 'warn' | 'err' | 'off'; className?: string }) {
  const color = { ok: 'bg-ok', warn: 'bg-warn', err: 'bg-err', off: 'bg-off' }[tone]
  return <span aria-hidden="true" className={cn('inline-block h-1.5 w-1.5 shrink-0 rounded-full', color, className)} />
}

export function Kbd({ className, ...props }: React.HTMLAttributes<HTMLElement>) {
  return (
    <kbd
      className={cn('inline-flex h-[18px] min-w-[18px] items-center justify-center rounded border border-line-strong bg-raised px-1 font-mono text-[10.5px] leading-none text-dim', className)}
      {...props}
    />
  )
}
