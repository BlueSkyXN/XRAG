import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { api, ActionSpec, KnowledgeBase, SystemInfo } from './lib/api'
import { FeedbackProvider } from './lib/feedback'
import { navigate, useLocation } from './lib/router'
import { roleLabels, SessionProvider, useSession } from './lib/session'
import { Login } from './shell/login'
import { Shell } from './shell/shell'
import { pageById, PageId } from './shell/nav'
import { Page } from './components/page'
import { Empty, Loading } from './components/panel'
import { Button } from './components/button'

const views: Record<PageId, React.LazyExoticComponent<() => React.JSX.Element>> = {
  search: lazy(() => import('./pages/search')),
  graph: lazy(() => import('./pages/graph')),
  research: lazy(() => import('./pages/research')),
  sources: lazy(() => import('./pages/sources')),
  jobs: lazy(() => import('./pages/jobs')),
  knowledge: lazy(() => import('./pages/knowledge')),
  feedback: lazy(() => import('./pages/feedback')),
  eval: lazy(() => import('./pages/eval')),
  overview: lazy(() => import('./pages/overview')),
  access: lazy(() => import('./pages/access')),
  settings: lazy(() => import('./pages/settings'))
}

/** 这些页面都在当前知识库里工作；没有知识库时先引导创建。 */
const needsKb: PageId[] = ['search', 'graph', 'research', 'sources', 'knowledge', 'feedback', 'eval']

function Workbench() {
  const location = useLocation()
  const { kbs, may } = useSession()
  const first = location.segments[0] as PageId | undefined
  const page = first && first in views ? first : undefined

  useEffect(() => {
    if (!first) navigate(kbs.length ? '/search' : '/overview', true)
  }, [first, kbs.length])

  if (!page) {
    return (
      <Shell current="search">
        <Page>
          {first && (
            <Empty title="没有这个页面" action={<Button onClick={() => navigate('/search')}>回到检索</Button>}>
              地址 <code>{location.path}</code> 不对应任何工作台页面。
            </Empty>
          )}
        </Page>
      </Shell>
    )
  }

  const def = pageById[page]
  const View = views[page]
  let body = (
    <Suspense fallback={<Loading />}>
      <View />
    </Suspense>
  )
  if (!may(def.min)) {
    body = (
      <Page>
        <Empty title={`${def.label}需要 ${roleLabels[def.min]} 或更高角色`}>当前身份看不到这一页。需要时请管理员调整你的访问 Key 角色。</Empty>
      </Page>
    )
  } else if (needsKb.includes(page) && !kbs.length) {
    body = (
      <Page>
        <Empty
          title="还没有可用的知识库"
          action={may('admin') ? <Button variant="primary" onClick={() => navigate('/overview?create=1')}>新建知识库</Button> : undefined}
        >
          {may('admin') ? '先新建一个知识库，再登记来源或上传资料。' : '当前身份没有任何知识库的访问权限，请联系管理员授权。'}
        </Empty>
      </Page>
    )
  }
  return <Shell current={page}>{body}</Shell>
}

type Boot = { system: SystemInfo; kbs: KnowledgeBase[]; actions: ActionSpec[] }

export default function App() {
  const [phase, setPhase] = useState<'loading' | 'login' | 'ready'>('loading')
  const [boot, setBoot] = useState<Boot | null>(null)

  const load = useCallback(async () => {
    const [system, kbs, schema] = await Promise.all([api.system(), api.kbs(), api.schema()])
    setBoot({ system, kbs: kbs.items, actions: schema.actions })
    setPhase('ready')
  }, [])

  useEffect(() => {
    load().catch(() => setPhase('login'))
  }, [load])

  const expire = useCallback(() => {
    setBoot(null)
    setPhase('login')
  }, [])

  return (
    <FeedbackProvider onExpire={expire}>
      {phase === 'loading' && <p className="grid h-dvh place-items-center text-[13px] text-dim">正在连接 XRAG 服务…</p>}
      {phase === 'login' && <Login onDone={load} />}
      {phase === 'ready' && boot && (
        <SessionProvider key={boot.system.identity.id} initial={boot} onLogout={expire}>
          <Workbench />
        </SessionProvider>
      )}
    </FeedbackProvider>
  )
}
