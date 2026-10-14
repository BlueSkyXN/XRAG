import { SyntheticEvent, useEffect, useState, type CSSProperties } from 'react'
import { ArrowRight, Plus } from 'lucide-react'
import { api } from '../lib/api'
import { useData, useRun } from '../lib/feedback'
import { navigate, useLocation } from '../lib/router'
import { roleLabels, useSession } from '../lib/session'
import { size } from '../lib/utils'
import { Dot } from '../components/badge'
import { Button } from '../components/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Field, Input } from '../components/input'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, KeyValues, Loading, Panel, PanelBody, PanelHeader } from '../components/panel'
import { Table, Td, Th } from '../components/table'

const faq: [string, string][] = [
  ['配置保存后，为什么能力没变？', '当前进程继续使用启动时的配置。重启 Server 和所有 Worker 后再看“设置 · 能力”；带 --config 启动时优先使用外部文件。改变向量模型后还需要显式重建向量。'],
  ['任务一直排队怎么办？', '检查内置执行器是否关闭；需要时单独运行 xrag-server worker --data 你的数据目录。关闭浏览器不会停止后台任务。'],
  ['为什么图里有关系，不代表已经找到根因？', '默认图来自原文的明确标注或已配置的抽取器。路径只说明关联及其出处，不能代替因果分析。'],
  ['备份为什么不包含来源目录？', '数据库快照保留入库正文与历史。Git 工作区、未导入的文件、Key 文件、环境变量和外部数据库由各自的存储管理，需要单独备份。恢复后先确认来源，再重新构建。'],
  ['统计有 37 条，却只展示 10 条？', '总数是在完整条件范围上计算的，10 条是当前页。翻页读取已保存的结果，不重新检索。语义候选不提供整个知识库的精确总数。']
]

function CreateKb({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { refresh, setKb } = useSession()
  const run = useRun()
  const [id, setId] = useState('')
  const [name, setName] = useState('')
  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const ok = await run(() => api.createKb(id.trim(), name.trim()), '知识库已创建。接下来登记来源或上传资料。')
    if (ok === undefined) return
    await refresh()
    setKb(id.trim())
    onOpenChange(false)
    navigate('/sources')
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogTitle>新建知识库</DialogTitle>
        <form onSubmit={submit} className="contents">
          <DialogBody className="space-y-4">
            <Field label="标识" hint="英文、数字、下划线或短横线。用于 API、CLI 和引用，创建后不能修改。">
              <Input mono required pattern="[a-zA-Z0-9_-]+" value={id} onChange={e => setId(e.target.value)} placeholder="my-notes" />
            </Field>
            <Field label="显示名称">
              <Input required value={name} onChange={e => setName(e.target.value)} placeholder="我的资料" />
            </Field>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" variant="primary">
              创建知识库
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export default function OverviewPage() {
  const { system, kbs, kb, setKb, may, profile } = useSession()
  const location = useLocation()
  const overview = useData(() => api.overview(), [])
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    if (location.search.get('create') === '1' && may('admin')) setCreating(true)
  }, [location.search, may])

  const o = overview.data
  const c = o?.counts
  const dbBytes = o?.database_bytes ? Object.values(o.database_bytes).reduce((a, b) => a + b, 0) : undefined

  const steps: { title: string; done: boolean; desc: string; to: string; label: string }[] = c
    ? [
        { title: '准备资料', done: c.sources > 0, desc: c.sources > 0 ? `已登记 ${c.sources} 个来源` : '登记服务器目录、Git 仓库，或直接上传 MD/TXT', to: '/sources', label: '资料与来源' },
        { title: '完成构建', done: c.files > 0, desc: c.files > 0 ? `已有 ${c.files} 个可查询文件` : '构建成功后文件才进入检索', to: '/sources', label: '资料与来源' },
        { title: '运行一次明确的查询', done: false, desc: '用资料里的错误编号或术语，再打开原文核对行号和版本', to: '/search', label: '检索' },
        { title: '按需增加能力', done: system.embedding, desc: system.embedding ? '语义模型已配置' : '模型不是首次使用的前提；需要语义检索时再配置', to: '/settings/providers', label: '设置' },
        { title: '收集反馈、持续改进', done: false, desc: '处理结果反馈，再用同一组问题比较策略', to: '/feedback', label: '反馈' }
      ]
    : []

  return (
    <Page>
      <PageHeader
        title="概览"
        desc="当前服务的真实状态，只包括当前身份能访问的范围。"
        cli={`xrag overview --profile ${profile}`}
        actions={
          <>
            <Button onClick={() => void overview.reload()}>刷新</Button>
            {may('admin') && (
              <Button variant="primary" onClick={() => setCreating(true)}>
                <Plus />
                新建知识库
              </Button>
            )}
          </>
        }
      />
      {overview.loading && !o && <Loading />}
      <ErrorLine>{overview.error}</ErrorLine>

      {c && (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {[
            { label: '已发布文件', value: c.files, to: '/sources', hue: 'var(--hue-sources)' },
            { label: '资料来源', value: c.sources, to: '/sources', hue: 'var(--hue-sources)' },
            { label: '知识条目', value: c.knowledge, to: '/knowledge', hue: 'var(--hue-knowledge)' },
            { label: '来源待处理', value: c.source_errors, to: '/sources', hue: 'var(--hue-sources)' }
          ].map(card => (
            <button
              key={card.label}
              type="button"
              onClick={() => navigate(card.to)}
              className="overview-card rounded-[10px] border border-line bg-panel px-4 py-3 text-left transition-colors hover:border-line-strong"
              style={{ '--card-hue': card.hue } as CSSProperties}
            >
              <span className="block text-xs text-dim">{card.label}</span>
              <b className={`mt-1 block font-mono text-[22px] font-semibold tabular-nums ${card.label === '来源待处理' && Number(card.value) > 0 ? 'text-warn' : ''}`}>{card.value}</b>
            </button>
          ))}
        </div>
      )}

      <div className="grid gap-3 lg:grid-cols-2">
        <Panel>
          <PanelHeader title="服务" />
          <PanelBody>
            {o && (
              <KeyValues
                rows={[
                  ['版本', <code key="v">{o.version}</code>],
                  ['当前身份', <span key="i"><code>{system.identity.name}</code> · {roleLabels[system.identity.role]}</span>],
                  [
                    '执行器',
                    <span key="w" className="inline-flex items-center gap-1.5">
                      <Dot tone={o.workers ? 'ok' : 'off'} />
                      {o.workers ? '内置执行器运行中' : '未启用，任务会排队；可单独运行 worker'}
                    </span>
                  ],
                  ['任务', `排队 ${o.jobs.queued ?? 0} · 运行 ${o.jobs.running ?? 0} · 失败 ${o.jobs.failed ?? 0} · 成功 ${o.jobs.succeeded ?? 0}`],
                  ['SQLite', dbBytes !== undefined ? size(dbBytes) : '仅管理员可见'],
                  ['关键词检索', system.keyword ?? '—'],
                  ['关系后端', system.graph_backend]
                ]}
              />
            )}
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader title="开始使用" meta={<span>按顺序完成</span>} />
          <ol className="divide-y divide-line">
            {steps.map((step, index) => (
              <li key={step.title} className="flex items-center gap-3 px-4 py-2.5">
                <span className={`grid h-5 w-5 shrink-0 place-items-center rounded-full font-mono text-[11px] ${step.done ? 'bg-ok/15 text-ok' : 'bg-raised text-dim'}`}>{step.done ? '✓' : index + 1}</span>
                <div className="min-w-0 flex-1">
                  <p className="text-[13px] text-fg">{step.title}</p>
                  <p className="text-xs text-dim">{step.desc}</p>
                </div>
                <Button size="sm" variant="ghost" onClick={() => navigate(step.to)}>
                  {step.label}
                  <ArrowRight />
                </Button>
              </li>
            ))}
          </ol>
        </Panel>
      </div>

      <Panel>
        <PanelHeader title="知识库" meta={<span>{kbs.length} 个</span>} />
        {kbs.length === 0 ? (
          <Empty title="还没有知识库" action={may('admin') && <Button variant="primary" onClick={() => setCreating(true)}>新建知识库</Button>}>
            {may('admin') ? '新建一个知识库，然后登记来源或上传资料。' : '当前身份没有任何知识库的访问权限。'}
          </Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>标识</Th>
                <Th>名称</Th>
                <Th>已发布文件</Th>
                <Th className="text-right">操作</Th>
              </tr>
            </thead>
            <tbody>
              {kbs.map(item => (
                <tr key={item.id}>
                  <Td className="font-mono text-fg">
                    {item.id}
                    {item.id === kb && <span className="ml-2 font-sans text-xs text-dim">当前</span>}
                  </Td>
                  <Td>{item.name}</Td>
                  <Td className="font-mono">{item.files ?? '—'}</Td>
                  <Td className="text-right">
                    <Button size="sm" variant="ghost" disabled={item.id === kb} onClick={() => setKb(item.id)}>
                      切换到这个知识库
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
      <Panel>
        <PanelHeader title="常见问题" />
        <div className="divide-y divide-line">
          {faq.map(([q, a]) => (
            <details key={q} className="group px-4 py-2.5">
              <summary className="cursor-pointer list-none text-[13px] text-fg">
                <span className="mr-2 inline-block font-mono text-faint transition-transform group-open:rotate-90">›</span>
                {q}
              </summary>
              <p className="mt-1.5 pl-4 text-[13px] leading-relaxed text-fg-soft">{a}</p>
            </details>
          ))}
        </div>
      </Panel>
      <CreateKb open={creating} onOpenChange={open => (setCreating(open), !open && location.search.get('create') && navigate('/overview', true))} />
    </Page>
  )
}
