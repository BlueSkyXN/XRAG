import { useEffect, useMemo, useRef, useState } from 'react'
import { BookmarkPlus, MessageSquareText } from 'lucide-react'
import { api, QueryResponse, ReadData } from '../lib/api'
import { useData } from '../lib/feedback'
import { useSession } from '../lib/session'
import { cn, lineRange, tail } from '../lib/utils'
import { Badge } from '../components/badge'
import { Button } from '../components/button'
import { JsonBlock } from '../components/code'
import { Dialog, DialogContent, DialogTitle } from '../components/dialog'
import { ErrorLine, Loading } from '../components/panel'
import { Marked } from '../components/marked'
import { FeedbackDialog } from './feedback-dialog'

export type ReadTarget = {
  ref: string
  kb: string
  title?: string
  path?: string
  /** 结果项自带的行号，读取前先显示在标题里。 */
  lineStart?: number
  lineEnd?: number
}

const CONTEXT_STEPS = [12, 60, 500]

function freshnessLabel(value?: string) {
  if (value === 'current') return '当前版本'
  if (value === 'historical') return '历史版本'
  return value || ''
}

type LinesProps = { text: string; first: number; cite?: [number, number]; terms: string[]; numbered?: boolean; scrollRoot?: React.RefObject<HTMLDivElement | null> }

function Lines({ text, first, cite, terms, numbered = true, scrollRoot }: LinesProps) {
  const citeRef = useRef<HTMLDivElement>(null)
  const rows = useMemo(() => text.replace(/\r\n/g, '\n').split('\n'), [text])
  let inFrontMatter = first === 1 && rows[0]?.trim() === '---'

  // 只滚动阅读器自己的区域，把引用行放到上三分之一处；不能用 scrollIntoView，它会连带滚动整页
  useEffect(() => {
    const el = citeRef.current
    const root = scrollRoot?.current
    if (!root) return
    if (!el) {
      root.scrollTop = 0
      return
    }
    root.scrollTop += el.getBoundingClientRect().top - root.getBoundingClientRect().top - root.clientHeight / 3
  }, [text, cite?.[0], scrollRoot])

  return (
    <div className="py-2 text-[13.5px]">
      {rows.map((row, index) => {
        const no = first + index
        const cited = cite ? no >= cite[0] && no <= cite[1] : false
        const frontMatter = inFrontMatter
        if (inFrontMatter && index > 0 && row.trim() === '---') inFrontMatter = false
        return (
          <div
            key={no}
            ref={cite && no === cite[0] ? citeRef : undefined}
            className={cn('grid grid-cols-[3rem_minmax(0,1fr)] gap-3 pr-4', cited && 'bg-white/[0.035] shadow-[inset_2px_0_0_var(--color-fg)]')}
          >
            <span aria-hidden="true" className={cn('select-none text-right font-mono text-[11.5px] leading-[26px] tabular-nums', cited ? 'text-fg' : 'text-faint')}>
              {numbered ? no : ''}
            </span>
            <span
              className={cn(
                'whitespace-pre-wrap break-words leading-[26px] text-fg-soft',
                cited && 'text-fg',
                frontMatter && 'font-mono text-[12px] text-faint',
                !frontMatter && /^#{1,6}\s/.test(row) && 'font-semibold text-fg'
              )}
            >
              {row ? <Marked text={row} terms={terms} /> : ' '}
            </span>
          </div>
        )
      })}
    </div>
  )
}

/** 读取一个引用并按行显示。文本块和 grep 匹配会带出同一版本里的上下文行。 */
export function Reader({ target, terms = [], className }: { target: ReadTarget; terms?: string[]; className?: string }) {
  const { may } = useSession()
  const { addCite, cites } = useSession()
  const [step, setStep] = useState(0)
  const [feedbackOpen, setFeedbackOpen] = useState(false)
  const [offset, setOffset] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setStep(0)
    setOffset(0)
  }, [target.ref])

  const { data, error, loading } = useData<QueryResponse<ReadData>>(
    () => api.readRef(target.kb, target.ref, { context_lines: CONTEXT_STEPS[step], offset, length: 20000 }),
    [target.ref, target.kb, step, offset]
  )
  const read = data?.data
  const isText = typeof read?.text === 'string'
  const window = read?.window
  const cited = cites.some(c => c.ref === target.ref)
  const lines = lineRange(window?.cite_start ?? target.lineStart, window?.cite_end ?? target.lineEnd)

  return (
    <div className={cn('flex min-h-0 flex-col', className)}>
      <div className="shrink-0 border-b px-4 py-3">
        <p className="truncate font-mono text-[13px] font-semibold" title={read?.path ?? target.path}>
          {read?.path || target.path || target.title || '引用'}
        </p>
        {target.title && <p className="mt-0.5 truncate text-[12.5px] text-dim">{target.title}</p>}
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-dim">
          {lines && <Badge className="border-line-strong bg-raised font-mono text-fg">引用 L{lines}</Badge>}
          {read?.freshness && <span>{freshnessLabel(read.freshness)}</span>}
          {read?.edition_id && (
            <span className="font-mono" title={read.edition_id}>
              版本 {tail(read.edition_id)}
            </span>
          )}
          {window && (
            <span>
              显示 L{window.line_start}–{window.line_end} / 共 {window.total_lines} 行
            </span>
          )}
          <span className="ml-auto flex gap-1.5">
            {may('contributor') && (
              <>
                <Button size="sm" variant="ghost" onClick={() => setFeedbackOpen(true)}>
                  <MessageSquareText />
                  反馈
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={cited}
                  onClick={() => addCite({ ref: target.ref, kb: target.kb, label: `${target.path ?? read?.path ?? ''}${lines ? `:${lines}` : ''}` })}
                >
                  <BookmarkPlus />
                  {cited ? '已加入依据' : '用作草稿依据'}
                </Button>
              </>
            )}
          </span>
        </div>
      </div>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        {loading && !data && <Loading label="正在读取原文…" />}
        <ErrorLine className="m-4">{error}</ErrorLine>
        {read && isText && window && <Lines text={window.text} first={window.line_start} cite={[window.cite_start, window.cite_end]} terms={terms} scrollRoot={scrollRef} />}
        {read && isText && !window && <Lines text={read.text} first={1} numbered={offset === 0} terms={terms} scrollRoot={scrollRef} />}
        {read && !isText && (
          <div className="p-4">
            <p className="mb-3 text-[13px] text-dim">这是一条结构化依据（{String((read as Record<string, unknown>).kind ?? '归档结果')}），没有正文行。</p>
            <JsonBlock value={read} open />
          </div>
        )}
        {read && (
          <div className="flex flex-wrap items-center gap-2 border-t px-4 py-3 text-xs text-faint">
            {window && step < CONTEXT_STEPS.length - 1 && (window.line_start > 1 || window.line_end < window.total_lines) && (
              <Button size="sm" onClick={() => setStep(s => s + 1)}>
                展开更多上下文
              </Button>
            )}
            {!window && read.next_offset != null && (
              <Button size="sm" onClick={() => setOffset(read.next_offset ?? 0)}>
                继续读取后文
              </Button>
            )}
            {!window && offset > 0 && <span>从第 {offset} 个字符开始显示，这一段不标行号。</span>}
            <span className="ml-auto">内容来自引用固定的文件版本，不受之后的更新影响。</span>
          </div>
        )}
        {data && isText && <JsonBlock value={data} summary="原始结构" className="mx-4 mb-4" />}
      </div>
      {may('contributor') && <FeedbackDialog open={feedbackOpen} onOpenChange={setFeedbackOpen} target={{ kb: target.kb, ref: target.ref, label: target.path ?? read?.path ?? '' }} />}
    </div>
  )
}

/** 其他页面用的原文抽屉。 */
export function ReaderSheet({ target, onClose, terms }: { target: ReadTarget | null; onClose: () => void; terms?: string[] }) {
  return (
    <Dialog open={Boolean(target)} onOpenChange={open => !open && onClose()}>
      <DialogContent side="sheet">
        <DialogTitle>原文</DialogTitle>
        {target && <Reader target={target} terms={terms} className="flex-1" />}
      </DialogContent>
    </Dialog>
  )
}
