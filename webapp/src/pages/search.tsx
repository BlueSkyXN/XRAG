import { KeyboardEvent, SyntheticEvent, useEffect, useMemo, useRef, useState } from 'react'
import { BookmarkPlus, ChevronDown, ChevronLeft, ChevronRight, Filter, X } from 'lucide-react'
import { api, ApiError, Item, QueryResponse } from '../lib/api'
import { errorText, useData, useRun } from '../lib/feedback'
import { invocation } from '../lib/invocation'
import { useMediaQuery } from '../lib/media'
import { navigate, useLocation } from '../lib/router'
import { useSession } from '../lib/session'
import { cn, lineRange, tail } from '../lib/utils'
import { Badge, Kbd, RouteTag } from '../components/badge'
import { Button } from '../components/button'
import { Chip } from '../components/chip'
import { CodeBlock, JsonBlock } from '../components/code'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Field, Input } from '../components/input'
import { Marked, queryTerms } from '../components/marked'
import { Menu, MenuContent, MenuItem, MenuLabel, MenuTrigger } from '../components/menu'
import { Page } from '../components/page'
import { Empty, ErrorLine, Panel, PanelHeader } from '../components/panel'
import { Pipeline } from '../components/pipeline'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/tabs'
import { FeedbackDialog } from '../features/feedback-dialog'
import { EmptyKb } from '../features/empty-kb'
import { Reader, ReaderSheet } from '../features/reader'
import { Completeness, itemRoutes, itemView, pipelineStages, rankingLabels, totalDuration, translateNote } from '../features/result-meta'

type SearchAction = '+ask' | 'search.keyword' | 'search.semantic' | 'files.grep' | 'search.external'

const actionLabels: Record<SearchAction, { label: string; hint: string }> = {
  '+ask': { label: '组合检索', hint: '词法、语义、图按需组合，不生成答案' },
  'search.keyword': { label: '关键词', hint: '编号与关键词，完成时给出精确块数' },
  'search.semantic': { label: '语义', hint: '向量近邻，结果是候选而非全集' },
  'files.grep': { label: '原文正则', hint: '在已发布原文里逐行匹配正则' },
  'search.external': { label: '外部服务', hint: '调用已登记的外部 XRAG 检索' }
}

type Filters = Record<string, string>
type Submitted = { action: SearchAction; body: Record<string, unknown>; text: string }

const examples = ['E203', '日志丢失', '组件B', '分页样本记录']

function FilterDialog({ open, onOpenChange, fields, value, onApply }: { open: boolean; onOpenChange: (v: boolean) => void; fields: string[]; value: Filters; onApply: (f: Filters) => void }) {
  const [draft, setDraft] = useState<Filters>(value)
  useEffect(() => {
    if (open) setDraft(value)
  }, [open, value])
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogTitle>按字段筛选</DialogTitle>
        <DialogBody className="space-y-3">
          <p className="text-xs leading-relaxed text-dim">字段来自资料的 front matter。同一字段只填一个值；多个字段之间同时满足。留空表示不筛选。</p>
          {fields.length === 0 && <p className="text-[13px] text-dim">这个知识库没有登记字段。</p>}
          {fields.map(field => (
            <Field key={field} label={<code>{field}</code>}>
              <Input value={draft[field] ?? ''} onChange={e => setDraft(d => ({ ...d, [field]: e.target.value }))} />
            </Field>
          ))}
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onApply({})}>
            清除筛选
          </Button>
          <Button
            variant="primary"
            onClick={() => onApply(Object.fromEntries(Object.entries(draft).filter(([, v]) => v.trim())))}
          >
            应用
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ResultRow({
  item,
  index,
  action,
  terms,
  active,
  onSelect,
  onFeedback,
  onCite,
  cited,
  canWrite
}: {
  item: Item
  index: number
  action: string
  terms: string[]
  active: boolean
  onSelect: () => void
  onFeedback: () => void
  onCite: () => void
  cited: boolean
  canWrite: boolean
}) {
  const view = itemView(item)
  const routes = itemRoutes(item, action)
  const lines = lineRange(item.line_start, item.line_end)
  return (
    <li>
      <div
        role="button"
        tabIndex={active ? 0 : -1}
        data-row={index}
        aria-current={active ? 'true' : undefined}
        onClick={onSelect}
        onFocus={onSelect}
        className={cn(
          'grid cursor-pointer grid-cols-[1.75rem_minmax(0,1fr)_auto] gap-x-2.5 gap-y-1 border-b px-3.5 py-3 outline-none transition-colors hover:bg-hover focus-visible:bg-hover',
          active && 'bg-raised shadow-[inset_2px_0_0_var(--color-fg)] hover:bg-raised'
        )}
      >
        <span className="row-span-3 pt-px text-right font-mono text-xs leading-6 text-faint">{index + 1}</span>
        <span className="min-w-0 truncate leading-6">
          <b className="font-semibold">{view.title}</b>
          {view.section && (
            <>
              <span className="mx-1.5 text-faint">›</span>
              <span className="text-dim">{view.section}</span>
            </>
          )}
        </span>
        <span className="text-right font-mono text-xs leading-6 text-faint">
          {item.score !== undefined && item.score !== 0 && `${rankingLabels[item.ranking ?? ''] ?? item.ranking ?? ''} ${item.score.toFixed(4)}`}
        </span>
        <span className="col-span-2 flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 font-mono text-xs text-dim">
          <span className="min-w-0 truncate">
            {item.path}
            {lines && `:${lines}`}
          </span>
          {routes.map(route => (
            <RouteTag key={route} route={route} />
          ))}
          {item.kind !== 'chunk' && <Badge>{item.kind}</Badge>}
        </span>
        {view.snippet && (
          <p className="col-span-2 line-clamp-2 text-[13px] leading-relaxed text-fg-soft">
            <Marked text={view.snippet} terms={terms} />
          </p>
        )}
        {active && canWrite && (
          <div className="col-span-2 flex flex-wrap gap-1.5 pt-1">
            <Button size="sm" variant="outline" onClick={e => (e.stopPropagation(), onFeedback())}>
              <Kbd>F</Kbd>
              反馈这条结果
            </Button>
            <Button size="sm" variant="outline" disabled={cited} onClick={e => (e.stopPropagation(), onCite())}>
              <Kbd>D</Kbd>
              {cited ? '已加入草稿依据' : '用作草稿依据'}
            </Button>
          </div>
        )}
      </div>
    </li>
  )
}

export default function SearchPage() {
  const session = useSession()
  const { kb, caps, actions, scope, setScope, may, profile, addCite, cites } = session
  const location = useLocation()
  const run = useRun()
  const desktop = useMediaQuery('(min-width: 1024px)')

  const initialAction = (location.search.get('action') as SearchAction) || '+ask'
  const [action, setAction] = useState<SearchAction>(initialAction in actionLabels ? initialAction : '+ask')
  const [text, setText] = useState(location.search.get('q') ?? '')
  const [graph, setGraph] = useState(false)
  const [rerank, setRerank] = useState(false)
  const [trace, setTrace] = useState(true)
  const [limit, setLimit] = useState(10)
  const [filters, setFilters] = useState<Filters>({})
  const [filterOpen, setFilterOpen] = useState(false)

  const [pages, setPages] = useState<QueryResponse[]>([])
  const [submitted, setSubmitted] = useState<Submitted | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [selected, setSelected] = useState(0)
  const [tab, setTab] = useState('read')
  const [sheetOpen, setSheetOpen] = useState(false)
  const [feedbackItem, setFeedbackItem] = useState<Item | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLOListElement>(null)

  const discover = useData(() => api.discover(kb), [kb])
  const fields: string[] = discover.data?.data?.fields ?? []

  const response = pages[pages.length - 1]
  const offset = pages.slice(0, -1).reduce((n, p) => n + p.returned, 0)
  const items = response?.items ?? []
  const current = items[selected]
  const terms = useMemo(() => (submitted && submitted.action !== 'files.grep' ? queryTerms(submitted.text) : []), [submitted])
  const spec = actions.find(a => a.action === action)
  const submittedSpec = submitted ? actions.find(a => a.action === submitted.action) : undefined

  // 切换知识库后旧结果和范围都失效
  useEffect(() => {
    setPages([])
    setSubmitted(null)
    setError('')
  }, [kb])

  useEffect(() => {
    const a = location.search.get('action') as SearchAction | null
    if (a && a in actionLabels) setAction(a)
  }, [location.search])

  useEffect(() => {
    function onKey(event: globalThis.KeyboardEvent) {
      const target = event.target as HTMLElement | null
      if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return
      if (target && (['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable)) return
      event.preventDefault()
      inputRef.current?.focus()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const unavailable = (a: SearchAction) => {
    if (a === 'search.semantic' && !caps.embedding) return '未配置向量模型'
    if (a === 'search.external' && !caps.external) return '未登记外部检索服务'
    return undefined
  }

  function buildBody(q: string): Record<string, unknown> {
    const body: Record<string, unknown> = { kbs: [kb] }
    body[action === 'files.grep' ? 'pattern' : 'q'] = q
    body.limit = limit
    if (trace) body.trace = true
    if (scope) body.within = scope.handle
    if (action === '+ask' && graph) body.graph = true
    if (action === '+ask' && rerank) body.rerank = true
    const f = Object.entries(filters)
    if (f.length) body.filters = f.map(([field, value]) => ({ field, op: 'eq', value }))
    return body
  }

  const preview = spec ? invocation(spec, buildBody(text || '…'), profile).cli : ''

  async function search(q = text) {
    if (!q.trim() || !spec) return
    const body = buildBody(q)
    setBusy(true)
    setError('')
    try {
      const r = await api.query(spec.path, body)
      setPages([r])
      setSubmitted({ action, body, text: q })
      setSelected(0)
      setTab('read')
    } catch (err) {
      setPages([])
      // 会话过期交给 run 处理（回到登录页）；其他错误留在检索框下方，便于对照修改查询
      if (err instanceof ApiError && err.status === 401) await run(() => Promise.reject(err))
      else setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  function submit(event: SyntheticEvent) {
    event.preventDefault()
    void search()
  }

  async function nextPage() {
    if (!response?.result_id || !response.next_cursor) return
    const r = await run(() => api.page(kb, response.result_id!, response.next_cursor!, limit))
    if (r) {
      setPages(p => [...p, r])
      setSelected(0)
    }
  }

  function select(index: number) {
    setSelected(index)
    if (!desktop) setSheetOpen(true)
  }

  function cite(item: Item) {
    if (!item.ref) return
    const v = itemView(item)
    addCite({ ref: item.ref, kb, label: `${item.path ?? v.title}${item.line_start ? `:${lineRange(item.line_start, item.line_end)}` : ''}` })
  }

  function onListKey(event: KeyboardEvent<HTMLOListElement>) {
    const move = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    if (move) {
      event.preventDefault()
      const next = Math.min(items.length - 1, Math.max(0, selected + move))
      setSelected(next)
      listRef.current?.querySelector<HTMLElement>(`[data-row="${next}"]`)?.focus()
    } else if (event.key === 'Enter' && !desktop) {
      setSheetOpen(true)
    } else if ((event.key === 'f' || event.key === 'F') && current?.ref && may('contributor')) {
      setFeedbackItem(current)
    } else if ((event.key === 'd' || event.key === 'D') && current?.ref && may('contributor')) {
      cite(current)
    }
  }

  const calls = submitted && submittedSpec ? invocation(submittedSpec, submitted.body, profile) : null
  const readTarget = current?.ref
    ? { ref: current.ref, kb, path: current.path, title: itemView(current).title + (itemView(current).section ? ` › ${itemView(current).section}` : ''), lineStart: current.line_start, lineEnd: current.line_end }
    : null
  const disc = discover.data?.data
  const emptyKb = disc !== undefined && disc.documents === 0

  return (
    <Page>
      {/* 命令卡 */}
      <form onSubmit={submit} className="rounded-[10px] border border-line-strong bg-panel" aria-label="检索">
        <div className="flex min-h-[52px] items-center gap-2.5 px-3.5 font-mono text-[15px]">
          <span aria-hidden="true" className="text-faint">
            ❯
          </span>
          <span className="hidden text-dim sm:inline">xrag</span>
          <Menu>
            <MenuTrigger asChild>
              <button type="button" className="inline-flex shrink-0 items-center gap-1 rounded-md border border-line-strong bg-raised px-2 py-0.5 font-semibold text-fg hover:bg-hover" aria-label="切换检索动作">
                {spec?.cli ?? action}
                <ChevronDown className="h-3 w-3 text-faint" />
              </button>
            </MenuTrigger>
            <MenuContent align="start" className="min-w-80">
              <MenuLabel>检索动作</MenuLabel>
              {(Object.keys(actionLabels) as SearchAction[]).map(a => {
                const s = actions.find(x => x.action === a)
                const off = unavailable(a)
                return (
                  <MenuItem key={a} disabled={Boolean(off)} onSelect={() => setAction(a)} className="h-auto items-start py-1.5">
                    <span className="w-32 shrink-0 font-mono text-[12.5px]">{s?.cli ?? a}</span>
                    <span className="text-xs leading-relaxed text-dim">
                      {actionLabels[a].label} · {off ?? actionLabels[a].hint}
                    </span>
                  </MenuItem>
                )
              })}
            </MenuContent>
          </Menu>
          <input
            ref={inputRef}
            value={text}
            onChange={e => setText(e.target.value)}
            placeholder={action === 'files.grep' ? '正则，例如 E20[0-9]' : '编号、术语或问题'}
            aria-label={action === 'files.grep' ? '正则表达式' : '查询'}
            autoComplete="off"
            spellCheck={false}
            className="min-w-0 flex-1 bg-transparent text-fg outline-none placeholder:text-faint"
          />
          <Button type="submit" variant="primary" size="lg" disabled={busy || !text.trim() || Boolean(unavailable(action))}>
            {busy ? '检索中…' : '检索'}
            {!busy && <Kbd className="border-bg/20 bg-bg/10 text-bg/70">↵</Kbd>}
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-1.5 border-t px-3.5 py-2">
          <span className="mr-0.5 text-xs text-faint">选项</span>
          {action === '+ask' && (
            <Chip pressed={graph} onClick={() => setGraph(v => !v)} title="把显式关系涉及的文本块作为补充候选">
              <code>--graph</code> 图关系补充
            </Chip>
          )}
          {action === '+ask' && (
            <Chip pressed={rerank} disabled={!caps.rerank} onClick={() => setRerank(v => !v)} title={caps.rerank ? undefined : '未配置重排模型'}>
              <code>--rerank</code> 重排
            </Chip>
          )}
          <Chip pressed={trace} onClick={() => setTrace(v => !v)}>
            <code>--trace</code> 执行过程
          </Chip>
          <Chip onClick={() => setLimit(l => (l === 10 ? 20 : l === 20 ? 5 : 10))} title="每页条数，点击切换 5 / 10 / 20">
            <code>--limit</code> {limit}
          </Chip>
          {action !== 'files.grep' && action !== 'search.external' && (
            <Chip pressed={Object.keys(filters).length > 0} onClick={() => setFilterOpen(true)}>
              <Filter className="h-3 w-3" />
              字段{Object.keys(filters).length ? ` ${Object.entries(filters).map(([k, v]) => `${k}=${v}`).join(' ')}` : ''}
            </Chip>
          )}
          {scope && (
            <span className="inline-flex h-[26px] items-center gap-1.5 rounded-md border border-line-strong bg-raised pl-2.5 pr-1 text-[12.5px] text-fg" title={scope.label}>
              <code className="text-[12px]">--within</code>
              <span className="font-mono text-[12px]">{tail(scope.handle, 10)}</span>
              <span className="max-w-[220px] truncate text-dim">{scope.label}</span>
              <button type="button" className="rounded p-0.5 text-faint hover:bg-hover hover:text-fg" onClick={() => setScope(null)} aria-label="清除范围">
                <X className="h-3.5 w-3.5" />
              </button>
            </span>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t px-3.5 py-1.5 text-xs text-faint">
          {disc ? (
            <>
              <span>
                <b className="font-medium text-dim">{kb}</b> {disc.documents} 个文件 · {disc.chunks} 块
              </span>
              {disc.fields?.length > 0 && (
                <span>
                  字段 <b className="font-medium text-dim">{disc.fields.join(' · ')}</b>
                </span>
              )}
              <span>
                关系 <b className="font-medium text-dim">{disc.relation_types?.length ?? 0} 类</b>
              </span>
              <span className={caps.embedding ? undefined : 'opacity-70'}>语义 {caps.embedding ? <b className="font-medium text-dim">已配置</b> : '未配置向量模型'}</span>
              <span className={caps.rerank ? undefined : 'opacity-70'}>重排 {caps.rerank ? <b className="font-medium text-dim">已配置</b> : '未配置'}</span>
            </>
          ) : (
            <span>{discover.error || '读取知识库能力…'}</span>
          )}
          <span className="ml-auto hidden max-w-[50%] truncate font-mono lg:inline" title={preview}>
            {preview}
          </span>
        </div>
      </form>

      <FilterDialog
        open={filterOpen}
        onOpenChange={setFilterOpen}
        fields={fields}
        value={filters}
        onApply={f => {
          setFilters(f)
          setFilterOpen(false)
        }}
      />

      {response?.trace && response.trace.length > 0 && (
        <Pipeline
          label="执行过程"
          stages={pipelineStages(response, Boolean(caps.embedding))}
          total={{ label: response.notes.length ? translateNote(response.notes[0]) : '总耗时', value: totalDuration(response) }}
        />
      )}

      <ErrorLine>{error}</ErrorLine>

      <div className="grid gap-3 lg:grid-cols-[minmax(0,1.12fr)_minmax(0,1fr)] lg:items-start">
        <Panel aria-label="结果">
          <PanelHeader
            title={response ? `${response.returned} 条结果` : '结果'}
            meta={
              response && (
                <>
                  <span>{submitted ? actionLabels[submitted.action].label : ''}</span>
                  {pages.length > 1 && <span>第 {pages.length} 页</span>}
                  {response.notes.slice(response.trace?.length ? 1 : 0).map(note => (
                    <span key={note}>{translateNote(note)}</span>
                  ))}
                </>
              )
            }
            actions={
              response && (
                <>
                  {response.summary_ref && response.total.value != null && may('contributor') && (
                    <Button size="sm" variant="ghost" onClick={() => addCite({ ref: response.summary_ref!, kb, label: `统计：${submitted?.text ?? ''}` })}>
                      <BookmarkPlus />
                      统计作依据
                    </Button>
                  )}
                  {response.handle && (
                    <span className="inline-flex h-7 items-center gap-2 rounded-md border border-line-strong pl-2.5 pr-1 text-xs" title="保存的是这次结果的集合；组合与语义检索是候选集合，不是知识库全集。24 小时内有效。">
                      范围 <code className="text-fg">{tail(response.handle, 10)}</code>
                      <Button
                        size="sm"
                        variant="subtle"
                        className="h-5 px-2"
                        disabled={scope?.handle === response.handle}
                        onClick={() =>
                          setScope({ handle: response.handle!, label: `${actionLabels[submitted?.action ?? '+ask'].label}“${submitted?.text ?? ''}”的结果` })
                        }
                      >
                        {scope?.handle === response.handle ? '正在使用' : '在此范围内继续查'}
                      </Button>
                    </span>
                  )}
                </>
              )
            }
          />
          {!response && !busy && emptyKb && <EmptyKb kb={kb} />}
          {!response && !busy && !emptyKb && (
            <Empty mark title="从一个编号、术语或问题开始" action={
                kb === 'demo'
                  ? examples.map(x => (
                      <Button
                        key={x}
                        size="sm"
                        onClick={() => {
                          setText(x)
                          void search(x)
                        }}
                      >
                        {x}
                      </Button>
                    ))
                  : undefined
              }
            >
              结果保留文件路径、行号和版本，可以直接对照原文核查。按 <Kbd>/</Kbd> 回到检索框，<Kbd>⌘K</Kbd> 查看全部动作。
            </Empty>
          )}
          {response && items.length === 0 && (emptyKb ? <EmptyKb kb={kb} /> : <Empty title="当前范围内没有命中">没有命中不代表现实中不存在。可以换用文件中的完整编号、放宽字段筛选，或清除范围后再查。</Empty>)}
          {items.length > 0 && (
            <ol ref={listRef} onKeyDown={onListKey} aria-label="结果列表">
              {items.map((item, index) => (
                <ResultRow
                  key={item.ref ?? `${item.path}-${index}`}
                  item={item}
                  index={offset + index}
                  action={submitted?.action ?? action}
                  terms={terms}
                  active={index === selected}
                  onSelect={() => select(index)}
                  onFeedback={() => setFeedbackItem(item)}
                  onCite={() => cite(item)}
                  cited={cites.some(c => c.ref === item.ref)}
                  canWrite={may('contributor') && Boolean(item.ref)}
                />
              ))}
            </ol>
          )}
          {response && (pages.length > 1 || response.next_cursor) && (
            <div className="flex items-center gap-2 border-b px-3.5 py-2 text-xs text-faint">
              <Button size="sm" variant="ghost" disabled={pages.length < 2} onClick={() => (setPages(p => p.slice(0, -1)), setSelected(0))}>
                <ChevronLeft />
                上一页
              </Button>
              <Button size="sm" variant="ghost" disabled={!response.next_cursor} onClick={() => void nextPage()}>
                下一页
                <ChevronRight />
              </Button>
              <span>翻页读取已保存的结果，不重新检索。</span>
            </div>
          )}
          {response && <Completeness response={response} className="px-3.5 py-2.5" />}
        </Panel>

        <Panel aria-label="检查器" className="hidden lg:sticky lg:top-4 lg:flex lg:max-h-[calc(100dvh-7rem)]">
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col">
            <TabsList>
              <TabsTrigger value="read">原文</TabsTrigger>
              <TabsTrigger value="call">调用</TabsTrigger>
              <TabsTrigger value="json">响应 JSON</TabsTrigger>
              {response && <span className="ml-auto self-center pr-2 font-mono text-xs text-faint">{tail(response.query_id, 12)}</span>}
            </TabsList>
            <TabsContent value="read" className="flex min-h-0 flex-1 flex-col">
              {readTarget ? (
                <Reader target={readTarget} terms={terms} className="min-h-[360px] flex-1" />
              ) : (
                <Empty mark={false} title="选中一条结果，在这里对照原文">原文来自结果引用固定的文件版本，引用行会被标出。</Empty>
              )}
            </TabsContent>
            <TabsContent value="call" className="min-h-0 flex-1 space-y-2.5 overflow-y-auto p-3">
              {calls ? (
                <>
                  <p className="text-xs leading-relaxed text-dim">这次检索的等价调用。网页、CLI、HTTP 和 MCP 走同一组接口，结果一致。</p>
                  <CodeBlock label="CLI" meta={`xrag Client · profile ${profile}`} code={calls.cli} />
                  <CodeBlock label="HTTP" meta={submittedSpec?.path} code={calls.http} />
                  <CodeBlock label="MCP" meta={`tools/call · ${submittedSpec?.tool} · ${submittedSpec?.group} 工具组`} code={calls.mcp} />
                  <p className="text-xs text-faint">
                    profile 名称可在
                    <button type="button" className="mx-1 text-dim underline underline-offset-2 hover:text-fg" onClick={() => navigate('/access')}>
                      接入
                    </button>
                    页修改。Key 用环境变量占位，不会出现在页面里。
                  </p>
                </>
              ) : (
                <Empty mark={false} title="检索之后显示等价调用" />
              )}
            </TabsContent>
            <TabsContent value="json" className="min-h-0 flex-1 overflow-y-auto p-3">
              {response ? <JsonBlock value={response} open summary="完整响应" /> : <Empty mark={false} title="检索之后显示完整响应" />}
            </TabsContent>
          </Tabs>
        </Panel>
      </div>

      {!desktop && <ReaderSheet target={sheetOpen ? readTarget : null} onClose={() => setSheetOpen(false)} terms={terms} />}
      <FeedbackDialog
        open={Boolean(feedbackItem)}
        onOpenChange={open => !open && setFeedbackItem(null)}
        target={feedbackItem?.ref ? { kb, ref: feedbackItem.ref, label: `${feedbackItem.path ?? ''}${feedbackItem.line_start ? `:${lineRange(feedbackItem.line_start, feedbackItem.line_end)}` : ''}` } : null}
      />
    </Page>
  )
}
