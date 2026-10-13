import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { LucideIcon } from 'lucide-react'
import { FileText, LayoutDashboard, LogOut, Menu, RefreshCw, Search } from 'lucide-react'
import { ApiError, api, KnowledgeBase, LoginMode, ResultItem, SearchMode, SearchResult, SystemInfo } from './lib/api'
import { Badge } from './components/badge'
import { Button } from './components/button'
import { Card, CardContent, CardHeader, CardTitle } from './components/card'
import { Dialog, DialogContent, DialogTitle } from './components/dialog'
import { Input } from './components/input'
import { cn } from './lib/utils'

type Phase = 'loading' | 'login' | 'ready'

type ReaderState = {
  open: boolean
  title: string
  path: string
  edition: string
  freshness: string
  text: string
  lineStart: number
  loading: boolean
}

type NavItem = {
  name: string
  icon: LucideIcon
  active?: boolean
  href?: string
}

type SubmittedQuery = {
  query: string
  mode: SearchMode
  graph: boolean
}

const emptyReader: ReaderState = {
  open: false,
  title: '',
  path: '',
  edition: '',
  freshness: '',
  text: '',
  lineStart: 1,
  loading: false
}

const navGroups: { label: string; items: NavItem[] }[] = [
  {
    label: '新版工作台',
    items: [{ name: '检索', icon: Search, active: true }]
  },
  {
    label: '经典工作台',
    items: [{ name: '完整功能', icon: LayoutDashboard, href: '/' }]
  }
]

const kindLabels: Record<string, string> = {
  chunk: '文本块',
  document: '文档',
  entity: '实体',
  graph: '关系'
}

const rankingLabels: Record<string, string> = {
  rrf: '组合排序',
  lexical: '词法排序',
  semantic: '语义排序'
}

const modeLabels: Record<string, string> = {
  ask: '组合',
  keyword: '关键词',
  semantic: '语义',
  server: '服务端'
}

function stripMarkdownHeading(value: string) {
  return value.replace(/^#{1,6}\s*/gm, '').trim()
}

function normalizeSnippet(value: string, title = '') {
  const normalized = value
    .replace(/^#{1,6}\s*/gm, '')
    .replace(/\n{2,}/g, '\n')
    .trim()
  const heading = stripMarkdownHeading(title)
  const firstLine = normalized.split('\n')[0]?.trim() ?? ''
  return firstLine && firstLine === heading ? normalized.slice(firstLine.length).trimStart() : normalized
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function highlightTerms(text: string, query: string): ReactNode {
  const terms = query.split(/\s+/).filter(term => term.length >= 2)
  if (!terms.length) return text
  const pattern = new RegExp(`(${terms.map(escapeRegExp).join('|')})`, 'gi')
  const lowerTerms = terms.map(term => term.toLowerCase())
  return text.split(pattern).map((part, index) =>
    lowerTerms.some(term => part.toLowerCase().includes(term)) ? (
      <mark key={index} className="rounded-sm bg-highlight px-0.5 text-foreground">
        {part}
      </mark>
    ) : (
      <span key={index}>{part}</span>
    )
  )
}

function formatTotal(result: SearchResult) {
  if (result.total?.value == null) return ''
  return ` · 共 ${result.total.value} ${result.total.unit === 'chunks' ? '块' : '项'}`
}

function App() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [loginMode, setLoginMode] = useState<LoginMode>('key')
  const [apiKey, setApiKey] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loginError, setLoginError] = useState('')
  const [system, setSystem] = useState<SystemInfo | null>(null)
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [kb, setKb] = useState('')
  const [navOpen, setNavOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [mode, setMode] = useState<SearchMode>('ask')
  const [includeGraph, setIncludeGraph] = useState(true)
  const [showTrace, setShowTrace] = useState(false)
  const [searching, setSearching] = useState(false)
  const [result, setResult] = useState<SearchResult | null>(null)
  const [error, setError] = useState('')
  const [selectedRef, setSelectedRef] = useState('')
  const [submitted, setSubmitted] = useState<SubmittedQuery | null>(null)
  const [reader, setReader] = useState<ReaderState>(emptyReader)
  const queryInputRef = useRef<HTMLInputElement>(null)

  const selectedKbName = useMemo(() => kbs.find(item => item.id === kb)?.name ?? kb, [kb, kbs])
  const graphSupplementCount = useMemo(
    () => result?.trace?.find(step => step.stage === 'graph_supplement')?.count ?? 0,
    [result]
  )

  const loadWorkbench = useCallback(async () => {
    const [nextSystem, nextKbs] = await Promise.all([api.system(), api.kbs()])
    setSystem(nextSystem)
    setKbs(nextKbs.items)
    setKb(current => (nextKbs.items.some(item => item.id === current) ? current : nextKbs.items[0]?.id ?? ''))
    setPhase('ready')
  }, [])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const [modeInfo] = await Promise.all([api.loginMode()])
        if (cancelled) return
        setLoginMode(modeInfo.mode)
        await loadWorkbench()
      } catch {
        if (!cancelled) {
          try {
            const modeInfo = await api.loginMode()
            if (!cancelled) setLoginMode(modeInfo.mode)
          } catch {
            if (!cancelled) setLoginMode('key')
          }
          setPhase('login')
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [loadWorkbench])

  useEffect(() => {
    function onKeyDown(event: globalThis.KeyboardEvent) {
      if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return
      const target = event.target as HTMLElement | null
      if (target && ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) return
      event.preventDefault()
      queryInputRef.current?.focus()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  async function submitLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setLoginError('')
    try {
      if (loginMode === 'password') await api.loginWithPassword(username, password)
      else await api.loginWithKey(apiKey)
      setApiKey('')
      setPassword('')
      await loadWorkbench()
    } catch (err) {
      setLoginError(err instanceof Error ? err.message : '登录失败')
    }
  }

  async function runSearch(nextQuery = query) {
    if (!kb || !nextQuery.trim()) return
    setSearching(true)
    setError('')
    try {
      const body: Record<string, unknown> = {
        kbs: [kb],
        q: nextQuery,
        limit: 10,
        trace: showTrace
      }
      if (mode === 'ask') body.graph = includeGraph
      const nextResult = await api.search(mode, body)
      setResult(nextResult)
      setSubmitted({ query: nextQuery, mode, graph: mode === 'ask' && includeGraph })
      setSelectedRef('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setPhase('login')
        return
      }
      setResult(null)
      setError(err instanceof Error ? err.message : '检索失败')
    } finally {
      setSearching(false)
    }
  }

  async function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await runSearch()
  }

  async function runExample(example: string) {
    setQuery(example)
    await runSearch(example)
  }

  async function openReader(item: ResultItem) {
    if (!item.ref) return
    setSelectedRef(item.ref)
    setReader({
      open: true,
      title: item.title ?? item.path ?? '原文',
      path: item.path ?? '',
      edition: item.edition_id ?? '',
      freshness: '',
      text: '',
      lineStart: item.line_start ?? 1,
      loading: true
    })
    try {
      const response = await api.read(kb, item.ref)
      setReader({
        open: true,
        title: item.title ?? response.data.path ?? '原文',
        path: response.data.path ?? item.path ?? '',
        edition: response.data.edition_id ?? item.edition_id ?? '',
        freshness: response.data.freshness ?? '',
        text: response.data.text ?? '',
        lineStart: item.line_start ?? 1,
        loading: false
      })
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setPhase('login')
        return
      }
      setReader({
        open: true,
        title: item.title ?? item.path ?? '原文',
        path: item.path ?? '',
        edition: item.edition_id ?? '',
        freshness: '',
        text: err instanceof Error ? err.message : '读取失败',
        lineStart: item.line_start ?? 1,
        loading: false
      })
    }
  }

  async function logout() {
    await api.logout().catch(() => undefined)
    setResult(null)
    setSystem(null)
    setKbs([])
    setPhase('login')
  }

  if (phase === 'loading') {
    return (
      <div className="grid min-h-screen place-items-center bg-background">
        <div className="text-sm text-muted-foreground">正在连接 XRAG 服务...</div>
      </div>
    )
  }

  if (phase === 'login') {
    return (
      <main className="grid min-h-screen place-items-center bg-background px-4">
        <div className="w-full max-w-sm">
          <div className="mb-7">
            <div className="text-lg font-semibold">XRAG Workbench</div>
            <div className="text-xs text-muted-foreground">证据型知识检索</div>
          </div>
          <Card>
            <CardHeader>
              <CardTitle>{loginMode === 'password' ? '账号登录' : 'API Key 登录'}</CardTitle>
              <p className="text-sm text-muted-foreground">连接当前服务；登录过程不调用外部模型。</p>
            </CardHeader>
            <CardContent>
              <form className="space-y-4" onSubmit={submitLogin}>
                {loginMode === 'password' ? (
                  <>
                    <div className="space-y-2">
                      <label className="text-sm font-medium" htmlFor="username">用户名</label>
                      <Input id="username" value={username} onChange={event => setUsername(event.target.value)} required autoComplete="username" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium" htmlFor="password">密码</label>
                      <Input id="password" type="password" value={password} onChange={event => setPassword(event.target.value)} required autoComplete="current-password" />
                    </div>
                  </>
                ) : (
                  <div className="space-y-2">
                    <label className="text-sm font-medium" htmlFor="api-key">API Key</label>
                    <Input id="api-key" type="password" value={apiKey} onChange={event => setApiKey(event.target.value)} required autoComplete="off" placeholder="xrag_..." />
                  </div>
                )}
                {loginError && <p className="text-sm text-destructive" role="alert">{loginError}</p>}
                <Button className="w-full" type="submit">进入工作台</Button>
              </form>
            </CardContent>
          </Card>
          <div className="mt-4 text-center">
            <Button asChild variant="ghost" size="sm">
              <a href="/">返回经典界面</a>
            </Button>
          </div>
        </div>
      </main>
    )
  }

  const sidebar = (
    <div className="flex h-full flex-col bg-surface">
      <div className="flex h-12 shrink-0 items-center border-b px-4">
        <span className="text-sm font-semibold tracking-tight">XRAG Workbench</span>
      </div>
      <nav className="flex-1 overflow-y-auto px-3 py-4" aria-label="工作台导航">
        {navGroups.map(group => (
          <div key={group.label} className="mb-5">
            <div className="px-2 pb-2 text-xs font-medium text-muted-foreground">{group.label}</div>
            <div className="space-y-1">
              {group.items.map(item => {
                const Icon = item.icon
                const content = (
                  <>
                    <Icon className="h-4 w-4" strokeWidth={1.5} />
                    <span>{item.name}</span>
                  </>
                )
                return item.active ? (
                  <span key={item.name} aria-current="page" className="flex h-8 items-center gap-2 rounded-md border border-border bg-accent-soft px-2 text-sm font-medium text-accent">
                    {content}
                  </span>
                ) : (
                  <a key={item.name} href={item.href} className="flex h-8 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
                    {content}
                  </a>
                )
              })}
            </div>
          </div>
        ))}
      </nav>
      <div className="border-t p-4 text-xs text-muted-foreground">
        <div className="font-mono">{system?.identity.name} / {system?.identity.role}</div>
        <Button className="mt-3 w-full" size="sm" variant="outline" onClick={() => void logout()}>
          <LogOut className="h-4 w-4" strokeWidth={1.5} />退出登录
        </Button>
      </div>
    </div>
  )

  return (
    <div className="min-h-screen bg-background">
      <div className="fixed inset-y-0 left-0 z-30 hidden w-56 border-r lg:block">{sidebar}</div>

      <Dialog open={navOpen} onOpenChange={setNavOpen}>
        <DialogContent className="left-0 top-0 flex h-dvh max-h-none w-[82vw] max-w-80 translate-x-0 translate-y-0 flex-col rounded-none border-r p-0">
          <DialogTitle className="sr-only">工作台导航</DialogTitle>
          {sidebar}
        </DialogContent>
      </Dialog>

      <header className="sticky top-0 z-20 border-b bg-surface lg:pl-56">
        <div className="flex h-12 items-center gap-2 px-3 sm:px-4">
          <Button className="lg:hidden" size="icon" variant="ghost" onClick={() => setNavOpen(true)} aria-label="展开导航">
            <Menu className="h-4 w-4" strokeWidth={1.5} />
          </Button>
          <span className="text-sm font-semibold lg:hidden">XRAG</span>
          <label className="sr-only" htmlFor="kb">知识库</label>
          <select
            id="kb"
            className="h-9 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-2 text-sm sm:max-w-72"
            value={kb}
            onChange={event => {
              setKb(event.target.value)
              setResult(null)
            }}
          >
            {kbs.map(item => (
              <option key={item.id} value={item.id}>{item.name}</option>
            ))}
          </select>
          <div className="ml-auto flex items-center gap-2">
            <span className="hidden items-center gap-2 text-xs text-muted-foreground md:flex">
              <span className={cn('h-1.5 w-1.5 rounded-full', system?.workers ? 'bg-success' : 'bg-muted-foreground')} />
              服务正常
            </span>
            <Button size="icon" variant="outline" onClick={() => void loadWorkbench()} aria-label="刷新服务状态">
              <RefreshCw className="h-4 w-4" strokeWidth={1.5} />
            </Button>
          </div>
        </div>
      </header>

      <main className="lg:pl-56">
        <div className="mx-auto w-full max-w-5xl px-3 py-5 sm:px-6">
          <section className="mb-4">
            <h1 className="text-base font-semibold">检索</h1>
            <p className="mt-1 text-sm text-muted-foreground">返回可核查的资料证据，不生成聊天答案。</p>
          </section>

          <form onSubmit={submitSearch} aria-busy={searching}>
            <div className="flex flex-col gap-2 border-b pb-3 md:flex-row md:items-center">
              <div className="flex h-10 shrink-0 items-center gap-1 rounded-md border bg-muted p-1" role="group" aria-label="检索方式">
                {([
                  ['ask', '组合'],
                  ['keyword', '关键词'],
                  ['semantic', '语义']
                ] as [SearchMode, string][]).map(([value, label]) => {
                  const disabled = value === 'semantic' && !system?.embedding
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={mode === value}
                      disabled={disabled}
                      title={disabled ? '语义模型未配置' : undefined}
                      onClick={() => setMode(value)}
                      className={cn(
                        'h-8 rounded px-3 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-45',
                        mode === value ? 'bg-surface text-accent shadow-panel' : 'text-muted-foreground hover:text-foreground'
                      )}
                    >
                      {label}
                    </button>
                  )
                })}
              </div>
              <div className="flex flex-1 gap-2">
                <label className="sr-only" htmlFor="query">查询</label>
                <Input
                  ref={queryInputRef}
                  id="query"
                  value={query}
                  onChange={event => setQuery(event.target.value)}
                  placeholder="输入编号、术语或问题，按 / 聚焦"
                  autoComplete="off"
                  required
                />
                <Button type="submit" disabled={searching}>
                  <Search className="h-4 w-4" strokeWidth={1.5} />{searching ? '检索中' : '检索'}
                </Button>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2 py-3 text-sm text-muted-foreground">
              {mode === 'ask' && (
                <label className="flex h-8 items-center gap-2">
                  <input type="checkbox" className="h-4 w-4 accent-accent" checked={includeGraph} onChange={event => setIncludeGraph(event.target.checked)} />
                  图关系
                </label>
              )}
              <label className="flex h-8 items-center gap-2">
                <input type="checkbox" className="h-4 w-4 accent-accent" checked={showTrace} onChange={event => setShowTrace(event.target.checked)} />
                执行过程
              </label>
            </div>
          </form>

          {error && (
            <div className="mb-4 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive" role="alert">
              {error}
            </div>
          )}

          <div className="mb-2 min-h-6 text-xs text-muted-foreground" aria-live="polite">
            {result && (
              <>
                {result.returned} 条证据 · {modeLabels[submitted?.mode ?? mode] ?? result.mode}
                {submitted?.graph && graphSupplementCount > 0 ? ` · 图关系补充 ${graphSupplementCount} 条` : ''}
                {formatTotal(result)}
              </>
            )}
          </div>

          {result?.items.length ? (
            <div className="overflow-hidden rounded-lg border bg-surface">
              <ol className="divide-y">
                {result.items.map((item, index) => {
                  const snippet = normalizeSnippet(item.text ?? '', item.title ?? item.name ?? item.path ?? '')
                  return (
                    <li key={item.ref ?? item.path ?? index} className={cn('px-4 py-3', selectedRef === item.ref && 'bg-muted/50')}>
                      <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                            <h2 className="min-w-0 text-[15px] font-semibold">
                              <span className="mr-2 font-mono text-xs text-muted-foreground">{String(index + 1).padStart(2, '0')}</span>
                              {normalizeSnippet(item.title ?? item.name ?? item.path ?? '结果')}
                            </h2>
                            {item.line_start && (
                              <span className="font-mono text-xs text-muted-foreground">L{item.line_start}–{item.line_end ?? item.line_start}</span>
                            )}
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                            {item.path && <span className="min-w-0 break-all font-mono">{item.path}</span>}
                            {item.edition_id && <span>版本 {item.edition_id.slice(-8)}</span>}
                            {item.kind && <span>{kindLabels[item.kind] ?? item.kind}</span>}
                            {item.ranking && <span>{rankingLabels[item.ranking] ?? item.ranking}</span>}
                          </div>
                          <p className="mt-2 line-clamp-4 whitespace-pre-wrap text-sm leading-7 text-foreground">
                            {highlightTerms(snippet, submitted?.query ?? query)}
                          </p>
                        </div>
                        <div className="shrink-0">
                          <Button size="sm" variant="outline" onClick={() => void openReader(item)}>
                            <FileText className="h-4 w-4" strokeWidth={1.5} />查看原文
                          </Button>
                        </div>
                      </div>
                    </li>
                  )
                })}
              </ol>
            </div>
          ) : (
            result ? (
              <div className="rounded-lg border bg-surface p-6">
                <div className="font-medium">没有匹配证据</div>
                <p className="mt-2 text-sm text-muted-foreground">
                  “{submitted?.query}”在 {selectedKbName} 中没有返回可展示内容。可以换用文件中的完整编号或更具体的词。
                </p>
              </div>
            ) : (
              <div className="rounded-lg border bg-surface p-6">
                <div className="font-medium">输入一个明确问题</div>
                <p className="mt-2 text-sm text-muted-foreground">结果会保留路径、行号和版本。</p>
                {kb === 'demo' && (
                  <div className="mt-4 flex flex-wrap gap-2">
                    {['E203', '日志丢失', '组件B'].map(example => (
                      <Button key={example} size="sm" variant="outline" onClick={() => void runExample(example)}>
                        {example}
                      </Button>
                    ))}
                  </div>
                )}
              </div>
            )
          )}

          {showTrace && result?.trace?.length ? (
            <details className="mt-4 rounded-lg border bg-surface">
              <summary className="cursor-pointer px-4 py-3 text-sm font-medium">执行过程</summary>
              <ol className="divide-y border-t">
                {result.trace.map((step, index) => (
                  <li key={`${step.stage ?? 'step'}-${index}`} className="flex flex-wrap gap-x-4 gap-y-1 px-4 py-2 text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">{step.stage}</span>
                    {step.count !== undefined && <span>{step.count} 项</span>}
                    {step.duration_ms !== undefined && <span>{step.duration_ms} ms</span>}
                    {step.note && <span>{step.note}</span>}
                  </li>
                ))}
              </ol>
            </details>
          ) : null}

          {result && (
            <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span className="font-mono">{result.query_id}</span>
            </div>
          )}
        </div>
      </main>

      <Dialog open={reader.open} onOpenChange={open => setReader(current => ({ ...current, open }))}>
        <DialogContent className="left-auto right-0 top-0 flex h-dvh max-h-none w-[min(760px,100vw)] translate-x-0 translate-y-0 flex-col rounded-none border-l p-0">
          <div className="border-b p-4 pr-10">
            <DialogTitle className="truncate text-base">{reader.title}</DialogTitle>
            <div className="mt-2 space-y-1 text-xs text-muted-foreground">
              {reader.path && <div className="break-all font-mono">{reader.path}</div>}
              <div>
                {reader.freshness === 'current' ? '当前版本' : reader.freshness || '版本待确认'}
                {reader.edition ? ` · ${reader.edition.slice(-8)}` : ''}
              </div>
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-auto bg-surface">
            {reader.loading ? (
              <div className="p-4 text-sm text-muted-foreground">读取原文中...</div>
            ) : (
              <div>
                {reader.text.replace(/\n+$/, '').split(/\r?\n/).map((line, index) => (
                  <div key={index} className="grid grid-cols-[3rem_minmax(0,1fr)] gap-3 px-4 py-1.5 text-sm leading-6">
                    <span className="select-none text-right font-mono text-xs leading-6 text-muted-foreground">{reader.lineStart + index}</span>
                    <span className="whitespace-pre-wrap">{line || ' '}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export default App
