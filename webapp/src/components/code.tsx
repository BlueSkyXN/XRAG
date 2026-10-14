import * as React from 'react'
import { Check, Copy } from 'lucide-react'
import { cn, copyText } from '../lib/utils'
import { useFeedback } from '../lib/feedback'

export function CopyButton({ text, label = '复制', className }: { text: string; label?: string; className?: string }) {
  const { toast } = useFeedback()
  const [done, setDone] = React.useState(false)
  return (
    <button
      type="button"
      className={cn('inline-flex h-6 items-center gap-1 rounded border border-line-strong bg-raised px-2 text-[11.5px] text-dim hover:text-fg', className)}
      onClick={async event => {
        // 放在 <summary> 里时，阻止点击顺带展开或收起
        event.preventDefault()
        event.stopPropagation()
        try {
          await copyText(text)
          setDone(true)
          setTimeout(() => setDone(false), 1500)
        } catch {
          toast('浏览器没有允许写入剪贴板，请手动选中复制。', 'err')
        }
      }}
    >
      {done ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      {done ? '已复制' : label}
    </button>
  )
}

type CodeBlockProps = {
  label: React.ReactNode
  meta?: React.ReactNode
  code: string
  children?: React.ReactNode
  className?: string
}

/** 带标题和复制按钮的命令/请求示例。children 可传入着色后的内容，复制的始终是 code。 */
export function CodeBlock({ label, meta, code, children, className }: CodeBlockProps) {
  return (
    <div className={cn('min-w-0 rounded-lg border border-line bg-bg', className)}>
      <div className="flex h-8 items-center gap-2 border-b px-3 text-xs text-dim">
        <b className="font-semibold text-fg">{label}</b>
        {meta && <span className="truncate">{meta}</span>}
        <CopyButton text={code} className="ml-auto" />
      </div>
      <pre className="overflow-x-auto whitespace-pre-wrap break-all px-3 py-2.5 font-mono text-[12.5px] leading-relaxed text-fg">{children ?? code}</pre>
    </div>
  )
}

/** 原始 JSON。默认折叠，展开后限制高度。 */
export function JsonBlock({ value, summary = '原始 JSON', open, className }: { value: unknown; summary?: string; open?: boolean; className?: string }) {
  const text = React.useMemo(() => JSON.stringify(value, null, 2), [value])
  return (
    <details open={open} className={cn('group rounded-lg border border-line bg-bg', className)}>
      <summary className="flex h-8 cursor-pointer list-none items-center gap-2 px-3 text-xs text-dim hover:text-fg">
        <span className="font-mono text-faint transition-transform group-open:rotate-90">›</span>
        {summary}
        <CopyButton text={text} className="ml-auto" />
      </summary>
      <pre className="max-h-[420px] overflow-auto border-t px-3 py-2.5 font-mono text-[12px] leading-relaxed text-fg-soft">{text}</pre>
    </details>
  )
}
