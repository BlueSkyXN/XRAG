import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import {
  BookOpen,
  Boxes,
  Database,
  FileText,
  GitBranch,
  LayoutDashboard,
  LogOut,
  Menu,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
  X
} from 'lucide-react'
import { api, KnowledgeBase, LoginMode, ResultItem, SearchMode, SearchResult, SystemInfo } from './lib/api'
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
  meta: string
  text: string
  loading: boolean
}

const navGroups = [
  {
    label: '工作台',
    items: [
      { name: '检索', icon: Search, active: true },
      { name: '概览', icon: LayoutDashboard, href: '/' }
    ]
  },
  {
    label: '知识',
    items: [
      { name: '资料', icon: Database, href: '/' },
      { name: '知识沉淀', icon: BookOpen, href: '/' },
      { name: '关系', icon: GitBranch, href: '/' }
    ]
  },
  {
    label: '质量',
    items: [
      { name: '评测', icon: ShieldCheck, href: '/' },
      { name: '任务', icon: Boxes, href: '/' }
    ]
  },
  {
    label: '系统',
    items: [{ name: '设置', icon: Settings, href: '/' }]
  }
]

function StatusPill({ label, enabled }: { label: string; enabled: boolean }) {
  return <Badge tone={enabled ? 'success' : 'neutral'}>{label}</Badge>
}

function formatTotal(result: SearchResult) {
  if (result.total?.value == null) return '无全集总数'
  return `${result.total.value} ${result.total.unit === 'chunks' ? '块' : result.total.unit ?? '项'}`
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
  const [query, setQuery] = useState('日志丢失')
  const [mode, setMode] = useState<SearchMode>('ask')
  const [includeGraph, setIncludeGraph] = useState(true)
  const [showTrace, setShowTrace] = useState(false)
  const [searching, setSearching] = useState(false)
  const [result, setResult] = useState<SearchResult | null>(null)
  const [error, setError] = useState('')
  const [reader, setReader] = useState<ReaderState>({
    open: false,
    title: '',
    meta: '',
    text: '',
    loading: false
  })

  const selectedKb = useMemo(() => kbs.find(item => item.id === kb), [kb, kbs])

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
        const [modeInfo] = await Promise.all([api.loginMode(), api.system()])
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

  async function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!kb) return
    setSearching(true)
    setError('')
    try {
      const body: Record<string, unknown> = {
        kbs: [kb],
        q: query,
        limit: 10,
        trace: showTrace
      }
      if (mode === 'ask') body.graph = includeGraph
      setResult(await api.search(mode, body))
    } catch (err) {
      setResult(null)
      setError(err instanceof Error ? err.message : '检索失败')
    } finally {
      setSearching(false)
    }
  }

  async function openReader(item: ResultItem) {
    if (!item.ref) return
    setReader({
      open: true,
      title: item.title ?? item.path ?? '原文',
      meta: [item.path, item.line_start ? `L${item.line_start}-${item.line_end ?? ''}` : null].filter(Boolean).join(' · '),
      text: '',
      loading: true
    })
    try {
      const response = await api.read(kb, item.ref)
      setReader({
        open: true,
        title: response.data.path ?? item.title ?? '原文',
        meta: [response.data.freshness, response.data.edition_id].filter(Boolean).join(' · '),
        text: response.data.text ?? '',
        loading: false
      })
    } catch (err) {
      setReader({
        open: true,
        title: item.title ?? '原文',
        meta: '',
        text: err instanceof Error ? err.message : '读取失败',
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
          <div className="mb-8 flex items-center gap-3">
            <div className="grid h-9 w-9 place-items-center rounded-md bg-accent text-sm font-semibold text-accent-foreground">
              X
            </div>
            <div>
              <div className="text-lg font-semibold">XRAG Workbench</div>
              <div className="text-xs text-muted-foreground">Next UI · 第一阶段</div>
            </div>
          </div>
          <Card>
            <CardHeader>
              <CardTitle>{loginMode === 'password' ? '账号登录' : 'API Key 登录'}</CardTitle>
              <p className="text-sm text-muted-foreground">连接当前服务，不发起外部模型调用。</p>
            </CardHeader>
            <CardContent>
              <form className="space-y-4" onSubmit={submitLogin}>
                {loginMode === 'password' ? (
                  <>
                    <div className="space-y-2">
                      <label className="text-sm font-medium" htmlFor="username">
                        用户名
                      </label>
                      <Input id="username" value={username} onChange={event => setUsername(event.target.value)} required autoComplete="username" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium" htmlFor="password">
                        密码
                      </label>
                      <Input id="password" type="password" value={password} onChange={event => setPassword(event.target.value)} required autoComplete="current-password" />
                    </div>
                  </>
                ) : (
                  <div className="space-y-2">
                    <label className="text-sm font-medium" htmlFor="api-key">
                      API Key
                    </label>
                    <Input id="api-key" type="password" value={apiKey} onChange={event => setApiKey(event.target.value)} required autoComplete="off" placeholder="xrag_..." />
                  </div>
                )}
                {loginError && <p className="text-sm text-red-700">{loginError}</p>}
                <Button className="w-full" type="submit">
                  进入工作台
                </Button>
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
    <div className="flex h-full flex-col border-r bg-white">
      <div className="flex h-14 items-center justify-between border-b px-4">
        <div className="flex items-center gap-2">
          <div className="grid h-7 w-7 place-items-center rounded bg-accent text-xs font-semibold text-accent-foreground">X</div>
          <span className="font-semibold">XRAG</span>
        </div>
        <button className="rounded p-1 text-muted-foreground lg:hidden" onClick={() => setNavOpen(false)} aria-label="关闭导航">
          <X className="h-4 w-4" />
        </button>
      </div>
      <nav className="flex-1 overflow-y-auto px-3 py-4" aria-label="工作台导航">
        {navGroups.map(group => (
          <div key={group.label} className="mb-5">
            <div className="px-2 pb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">{group.label}</div>
            <div className="space-y-1">
              {group.items.map(item => {
                const Icon = item.icon
                const content = (
                  <>
                    <Icon className="h-4 w-4" />
                    <span>{item.name}</span>
                  </>
                )
                return item.active ? (
                  <span key={item.name} className="flex h-9 items-center gap-2 rounded-md bg-blue-50 px-2 text-sm font-medium text-blue-700">
                    {content}
                  </span>
                ) : (
                  <a key={item.name} href={item.href} className="flex h-9 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
                    {content}
                  </a>
                )
              })}
            </div>
          </div>
        ))}
      </nav>
      <div className="border-t p-4 text-xs text-muted-foreground">
        <div className="mb-2 flex items-center gap-2">
          <span className={cn('h-2 w-2 rounded-full', system?.workers ? 'bg-emerald-500' : 'bg-slate-400')} />
          {system?.workers ? '内置执行器运行中' : '内置执行器关闭'}
        </div>
        <div className="font-mono">{system?.identity.name} / {system?.identity.role}</div>
      </div>
    </div>
  )

  return (
    <div className="min-h-screen bg-background">
      <div className="fixed inset-y-0 left-0 z-30 hidden w-64 lg:block">{sidebar}</div>
      {navOpen && (
        <>
          <div className="fixed inset-0 z-30 bg-slate-900/35 lg:hidden" onClick={() => setNavOpen(false)} />
          <div className="fixed inset-y-0 left-0 z-40 w-72 lg:hidden">{sidebar}</div>
        </>
      )}

      <header className="sticky top-0 z-20 border-b bg-white/95 backdrop-blur lg:pl-64">
        <div className="flex h-14 items-center gap-3 px-4">
          <Button className="lg:hidden" size="icon" variant="ghost" onClick={() => setNavOpen(true)} aria-label="展开导航">
            <Menu className="h-4 w-4" />
          </Button>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{selectedKb ? `${selectedKb.name} / ${selectedKb.id}` : 'XRAG'}</div>
            <div className="text-xs text-muted-foreground">知识检索</div>
          </div>
          <label className="sr-only" htmlFor="kb">知识库</label>
          <select id="kb" className="h-9 max-w-[190px] rounded-md border bg-white px-2 text-sm" value={kb} onChange={event => { setKb(event.target.value); setResult(null) }}>
            {kbs.map(item => (
              <option key={item.id} value={item.id}>{item.name} / {item.id}</option>
            ))}
          </select>
          <Button size="icon" variant="outline" onClick={() => void loadWorkbench()} aria-label="刷新">
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button asChild size="sm" variant="outline" className="hidden sm:inline-flex">
            <a href="/">经典界面</a>
          </Button>
          <Button size="sm" variant="ghost" onClick={() => void logout()}>
            <LogOut className="h-4 w-4" />退出
          </Button>
        </div>
      </header>

      <main className="lg:pl-64">
        <div className="mx-auto w-full max-w-6xl px-4 py-6">
          <section className="mb-5 flex flex-wrap items-center justify-between gap-3">
            <div>
              <h1 className="text-xl font-semibold">检索</h1>
              <p className="text-sm text-muted-foreground">返回证据、路径和版本；不生成聊天答案。</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <StatusPill label={system?.embedding ? '语义可用' : '关键词模式'} enabled={Boolean(system?.embedding)} />
              <StatusPill label={system?.workers ? '任务执行中' : '任务排队'} enabled={Boolean(system?.workers)} />
              <Badge tone="neutral">{system?.graph_backend ?? '显式关系'}</Badge>
            </div>
          </section>

          <Card className="mb-5 rounded-xl border-transparent shadow-[0_10px_30px_rgb(15_23_42_/_0.05)]">
            <form onSubmit={submitSearch}>
              <div className="flex flex-col gap-3 p-4 pb-3 md:flex-row md:items-center">
                <div className="flex rounded-lg bg-slate-100 p-1" role="group" aria-label="检索方式">
                  {([
                    ['ask', '组合'],
                    ['keyword', '关键词'],
                    ['semantic', '语义']
                  ] as [SearchMode, string][]).map(([value, label]) => (
                    <button
                      key={value}
                      type="button"
                      disabled={value === 'semantic' && !system?.embedding}
                      onClick={() => setMode(value)}
                      className={cn(
                        'h-8 rounded-md px-3 text-sm font-medium transition-colors disabled:opacity-40',
                        mode === value ? 'bg-white text-blue-700 shadow-panel' : 'text-muted-foreground hover:bg-white/60 hover:text-foreground'
                      )}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <div className="flex flex-1 gap-2">
                  <label className="sr-only" htmlFor="query">查询</label>
                  <Input id="query" value={query} onChange={event => setQuery(event.target.value)} placeholder="输入编号、术语或问题" required />
                  <Button type="submit" disabled={searching}>
                    <Search className="h-4 w-4" />{searching ? '检索中' : '检索'}
                  </Button>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-5 px-5 pb-5 pt-1 text-sm text-muted-foreground">
                {mode === 'ask' && (
                  <label className="flex h-8 items-center gap-2">
                    <input type="checkbox" className="h-4 w-4" checked={includeGraph} onChange={event => setIncludeGraph(event.target.checked)} />
                    图关系
                  </label>
                )}
                <label className="flex h-8 items-center gap-2">
                  <input type="checkbox" className="h-4 w-4" checked={showTrace} onChange={event => setShowTrace(event.target.checked)} />
                  执行过程
                </label>
                <span className="rounded-md bg-slate-100 px-2 py-1 text-xs">结果保留路径、行号与版本</span>
              </div>
            </form>
          </Card>

          {error && (
            <Card className="mb-5 border-red-200 bg-red-50">
              <CardContent className="pt-4 text-sm text-red-800">{error}</CardContent>
            </Card>
          )}

          {result && (
            <div className="mb-3 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <span>返回 {result.returned} 项</span>
              <span>{formatTotal(result)}</span>
              <Badge tone="neutral">{result.mode}</Badge>
              <span className="rounded-md bg-slate-100 px-2 py-1 font-mono text-xs">{result.query_id}</span>
            </div>
          )}

          <div className="space-y-3">
            {result?.items.length ? (
              <ul className="space-y-3">
                {result.items.map((item, index) => (
                  <li key={`${item.ref ?? item.path ?? index}`} className="rounded-xl bg-white p-5 shadow-[0_8px_24px_rgb(15_23_42_/_0.04)] transition-shadow hover:shadow-[0_12px_32px_rgb(15_23_42_/_0.07)]">
                    <div className="flex flex-wrap items-start justify-between gap-4">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-[15px] font-semibold">{item.title ?? item.name ?? item.path ?? '结果'}</span>
                          {item.kind && <Badge tone="accent">{item.kind}</Badge>}
                          {item.ranking && <Badge>{item.ranking}</Badge>}
                        </div>
                        <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                          {item.path && <span className="max-w-full break-all rounded-md bg-slate-100 px-2 py-1 font-mono">{item.path}</span>}
                          {item.line_start && <span className="rounded-md bg-slate-100 px-2 py-1 font-mono">L{item.line_start}-{item.line_end ?? ''}</span>}
                          {item.edition_id && <span className="rounded-md bg-slate-100 px-2 py-1 font-mono">{item.edition_id.slice(-9)}</span>}
                        </div>
                      </div>
                      <Button size="sm" variant="ghost" className="bg-slate-100 hover:bg-slate-200" onClick={() => void openReader(item)}>
                        <FileText className="h-4 w-4" />原文
                      </Button>
                    </div>
                    <p className="mt-4 max-w-4xl whitespace-pre-wrap rounded-lg bg-slate-50/80 p-3 text-sm leading-6 text-slate-700">
                      {(item.text ?? '').slice(0, 420)}{(item.text ?? '').length > 420 ? '…' : ''}
                    </p>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="rounded-xl bg-white p-10 text-center shadow-[0_8px_24px_rgb(15_23_42_/_0.04)]">
                <Search className="mx-auto mb-3 h-8 w-8 text-slate-300" />
                <div className="font-medium">尚无检索结果</div>
                <p className="mt-1 text-sm text-muted-foreground">输入明确编号、术语或问题后执行检索。</p>
              </div>
            )}
          </div>
        </div>
      </main>

      <Dialog open={reader.open} onOpenChange={open => setReader(current => ({ ...current, open }))}>
        <DialogContent>
          <div className="border-b p-4 pr-10">
            <DialogTitle>{reader.title}</DialogTitle>
            <div className="mt-1 break-all font-mono text-xs text-muted-foreground">{reader.meta}</div>
          </div>
          <pre className="max-h-[60vh] overflow-auto bg-slate-50 p-4 font-mono text-xs leading-6">{reader.loading ? '读取中...' : reader.text}</pre>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export default App
