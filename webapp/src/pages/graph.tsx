import { SyntheticEvent, useEffect, useMemo, useState } from 'react'
import { X } from 'lucide-react'
import { api, ApiError, Item, QueryResponse } from '../lib/api'
import { errorText, useRun } from '../lib/feedback'
import { invocation } from '../lib/invocation'
import { useLocation } from '../lib/router'
import { useSession } from '../lib/session'
import { lineRange, tail } from '../lib/utils'
import { Badge } from '../components/badge'
import { Button } from '../components/button'
import { Segmented } from '../components/chip'
import { CodeBlock } from '../components/code'
import { Field, Input, Select } from '../components/input'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, Note, Panel, PanelBody, PanelHeader } from '../components/panel'
import { Pipeline } from '../components/pipeline'
import { GraphDiagram, GraphEdge } from '../features/graph-diagram'
import { ReaderSheet, ReadTarget } from '../features/reader'
import { Completeness, itemView, pipelineStages } from '../features/result-meta'

type GraphAction = 'graph.neighbors' | 'graph.path' | 'graph.entity' | 'graph.chunks' | '+context'

const actionInfo: Record<GraphAction, string> = {
  'graph.neighbors': '邻居',
  'graph.path': '路径',
  'graph.entity': '实体',
  'graph.chunks': '出处块',
  '+context': '上下文'
}

const originLabels: Record<string, string> = { explicit: '显式标注', llm: '模型抽取', plugin: '抽取插件' }

function edgesOf(items: Item[]): GraphEdge[] {
  const out: GraphEdge[] = []
  const seen = new Set<string>()
  for (const item of items) {
    const list = item.edges?.length ? item.edges : item.from && item.to ? [{ from: item.from, to: item.to, relation: item.relation ?? '' }] : []
    for (const e of list) {
      const key = `${e.from}|${e.relation}|${e.to}`
      if (!seen.has(key)) {
        seen.add(key)
        out.push({ from: e.from, to: e.to, relation: e.relation })
      }
    }
  }
  return out
}

export default function GraphPage() {
  const { kb, scope, setScope, actions, profile, system, caps } = useSession()
  const location = useLocation()
  const run = useRun()
  const initial = location.search.get('action') as GraphAction | null
  const [action, setAction] = useState<GraphAction>(initial && initial in actionInfo ? initial : 'graph.neighbors')
  const [name, setName] = useState(kb === 'demo' ? '设备A' : '')
  const [to, setTo] = useState(kb === 'demo' ? '日志事件' : '')
  const [direction, setDirection] = useState('out')
  const [hops, setHops] = useState(2)
  const [response, setResponse] = useState<QueryResponse | null>(null)
  const [body, setBody] = useState<Record<string, unknown> | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [reading, setReading] = useState<ReadTarget | null>(null)

  useEffect(() => {
    if (initial && initial in actionInfo) setAction(initial)
  }, [initial])
  useEffect(() => {
    setResponse(null)
    setError('')
  }, [kb])

  const spec = actions.find(a => a.action === action)

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (!spec) return
    const b: Record<string, unknown> = { kbs: [kb], limit: 20, trace: true }
    if (action === 'graph.path') {
      b.from_id = name
      b.to_id = to
      b.direction = direction
      b.max_hops = hops
    } else {
      b.name = name
      if (action === 'graph.neighbors') {
        b.direction = direction
        b.hops = hops
      }
    }
    if (scope) b.within = scope.handle
    setBusy(true)
    setError('')
    try {
      setResponse(await api.query(spec.path, b))
      setBody(b)
    } catch (err) {
      setResponse(null)
      if (err instanceof ApiError && err.status === 401) await run(() => Promise.reject(err))
      else setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  const items = response?.items ?? []
  const edges = useMemo(() => edgesOf(items), [items])
  const entities = items.filter(i => i.kind === 'entity')
  const chunks = items.filter(i => i.kind === 'chunk')
  const calls = body && spec ? invocation(spec, body, profile) : null

  return (
    <Page>
      <PageHeader
        title="关系"
        desc="查询实体关系，查看原文出处。"
        cli={spec ? invocation(spec, action === 'graph.path' ? { kbs: [kb], from_id: name || '起点', to_id: to || '终点', direction, max_hops: hops } : { kbs: [kb], name: name || '实体', ...(action === 'graph.neighbors' ? { direction, hops } : {}) }, profile).cli : undefined}
      />

      <form onSubmit={submit} className="rounded-[10px] border border-line-strong bg-panel" aria-label="关系查询">
        <div className="flex flex-wrap items-end gap-3 px-4 py-3">
          <Field label="查询">
            <Segmented label="关系查询动作" value={action} onChange={setAction} options={(Object.keys(actionInfo) as GraphAction[]).map(a => ({ value: a, label: actionInfo[a] }))} />
          </Field>
          <Field label={action === 'graph.path' ? '起点' : '实体名'} className="min-w-[160px] flex-1">
            <Input required value={name} onChange={e => setName(e.target.value)} />
          </Field>
          {action === 'graph.path' && (
            <Field label="终点" className="min-w-[160px] flex-1">
              <Input required value={to} onChange={e => setTo(e.target.value)} />
            </Field>
          )}
          {(action === 'graph.neighbors' || action === 'graph.path') && (
            <>
              <Field label="方向">
                <Select value={direction} onChange={e => setDirection(e.target.value)}>
                  <option value="out">沿实际方向</option>
                  <option value="both">允许双向</option>
                  <option value="in">反向</option>
                </Select>
              </Field>
              <Field label={action === 'graph.path' ? '最多跳数' : '展开层数'}>
                <Input type="number" min={1} max={5} value={hops} onChange={e => setHops(Number(e.target.value))} className="w-20" />
              </Field>
            </>
          )}
          <Button type="submit" variant="primary" size="lg" disabled={busy || !name.trim()}>
            {busy ? '查询中…' : '查询'}
          </Button>
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t px-4 py-1.5 text-xs text-faint">
          <span>
            关系后端 <b className="font-medium text-dim">{caps.graph_backend || system.graph_backend}</b>
          </span>
          {scope ? (
            <span className="inline-flex items-center gap-1.5 text-dim">
              检索范围 <code className="text-fg">{tail(scope.handle, 10)}</code> {scope.label}
              <button type="button" onClick={() => setScope(null)} className="rounded p-0.5 hover:text-fg" aria-label="清除范围">
                <X className="h-3.5 w-3.5" />
              </button>
            </span>
          ) : (
            <span>查询整个知识库</span>
          )}
        </div>
      </form>

      {response?.trace && response.trace.length > 0 && <Pipeline label="执行过程" stages={pipelineStages(response, Boolean(caps.embedding))} />}
      <ErrorLine>{error}</ErrorLine>

      {response && (
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] lg:items-start">
          <div className="space-y-3">
            {edges.length > 0 && (
              <Panel>
                <PanelHeader title="关系图" meta={<span>本次返回 {edges.length} 条关系</span>} />
                <PanelBody>
                  <GraphDiagram edges={edges} start={action === 'graph.path' ? name : name} />
                </PanelBody>
              </Panel>
            )}
            <Panel>
              <PanelHeader title={`${response.returned} 项`} meta={<span>{actionInfo[action]}</span>} />
              {items.length === 0 && (
                <Empty title="未找到匹配结果">
                  {scope
                    ? '调整实体名称，或清除范围后再查。'
                    : action === 'graph.neighbors' || action === 'graph.path'
                      ? '调整实体名称、方向或展开层数后再查。'
                      : '换一个实体名再查。'}
                </Empty>
              )}
              <ul>
                {items.map((item, i) => (
                  <li key={item.ref ?? i} className="border-b px-4 py-3">
                    {item.kind === 'entity' && (
                      <p className="flex items-center gap-2 text-[13px]">
                        <Badge tone="gr">实体</Badge>
                        <b className="font-semibold">{item.name}</b>
                        <span className="text-xs text-dim">提及 {String((item as Record<string, unknown>).mentions ?? '—')} 次</span>
                      </p>
                    )}
                    {(item.kind === 'relation' || item.kind === 'path') && (
                      <div className="space-y-2">
                        {(item.edges ?? []).map(e => (
                          <div key={e.support_id} className="flex flex-wrap items-center gap-2 text-[13px]">
                            <span className="rounded-md border border-line-strong bg-raised px-2 py-0.5">{e.from}</span>
                            <span className="text-gr">— {e.relation} →</span>
                            <span className="rounded-md border border-line-strong bg-raised px-2 py-0.5">{e.to}</span>
                            <span className="font-mono text-xs text-dim">{e.path}</span>
                            {e.origin && <Badge>{originLabels[e.origin] ?? e.origin}</Badge>}
                          </div>
                        ))}
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="text-xs text-faint">
                            {item.kind === 'path' ? `${item.edges?.length ?? 0} 段关系` : `${item.support_count ?? 0} 个支撑块`}
                          </span>
                          {(item.support_refs ?? []).map((ref, k) => (
                            <Button key={ref} size="sm" variant="ghost" onClick={() => setReading({ ref, kb, title: `出处 ${k + 1}` })}>
                              读取出处 {k + 1}
                            </Button>
                          ))}
                        </div>
                      </div>
                    )}
                    {item.kind !== 'entity' && item.kind !== 'relation' && item.kind !== 'path' && (
                      <button type="button" className="w-full text-left" onClick={() => item.ref && setReading({ ref: item.ref, kb, path: item.path, title: itemView(item).title, lineStart: item.line_start, lineEnd: item.line_end })}>
                        <span className="flex items-center gap-2 text-[13px]">
                          <Badge>{item.kind === 'chunk' ? '文本块' : item.kind}</Badge>
                          <b className="truncate font-semibold">{itemView(item).title}</b>
                        </span>
                        <span className="mt-1 block font-mono text-xs text-dim">
                          {item.path}
                          {item.line_start ? `:${lineRange(item.line_start, item.line_end)}` : ''}
                        </span>
                        {itemView(item).snippet && <span className="mt-1 line-clamp-2 block text-[13px] text-fg-soft">{itemView(item).snippet}</span>}
                      </button>
                    )}
                  </li>
                ))}
              </ul>
              <Completeness response={response} className="px-4 py-2.5" />
            </Panel>
          </div>
          <Panel>
            <PanelHeader title="调用" />
            <PanelBody className="space-y-2.5">
              {calls && (
                <>
                  <CodeBlock label="CLI" meta={`profile ${profile}`} code={calls.cli} />
                  <CodeBlock label="HTTP" meta={spec?.path} code={calls.http} />
                  <CodeBlock label="MCP" meta={`tools/call · ${spec?.tool}`} code={calls.mcp} />
                </>
              )}
              {response.handle && (
                <div className="flex items-center gap-2 rounded-lg border border-line p-3 text-xs text-dim">
                  范围 <code className="text-fg">{tail(response.handle, 10)}</code>
                  <Button size="sm" variant="subtle" className="ml-auto" onClick={() => setScope({ handle: response.handle!, label: `${actionInfo[action]}“${name}”的结果` })}>
                    在此范围内继续查
                  </Button>
                </div>
              )}
              {entities.length + chunks.length > 0 && <Note>实体与文本块可以继续用“检索”页在同一范围内查询。</Note>}
            </PanelBody>
          </Panel>
        </div>
      )}
      {!response && !error && (
        <Panel>
          <Empty mark title="输入一个实体名开始" />
        </Panel>
      )}
      <ReaderSheet target={reading} onClose={() => setReading(null)} />
    </Page>
  )
}
