import { SyntheticEvent, useEffect, useMemo, useState } from 'react'
import { Download, FlaskConical, Play, Plus, RefreshCw, Upload, X } from 'lucide-react'
import { api, downloadJSON, EvalDraft, EvalReport, QueryRecord } from '../lib/api'
import { useData, usePolling, useRun } from '../lib/feedback'
import { useSession } from '../lib/session'
import { pct, shortTime, tail } from '../lib/utils'
import { Badge, StateBadge } from '../components/badge'
import { Button } from '../components/button'
import { Chip } from '../components/chip'
import { JsonBlock } from '../components/code'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Check, Field, FilePicker, Input, Select, Textarea } from '../components/input'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, Loading, Note, Panel, PanelBody, PanelHeader } from '../components/panel'
import { Table, Td, Th } from '../components/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/tabs'
import { activeStates } from '../features/jobs'

const strategies = [
  { id: 'keyword', label: '关键词' },
  { id: 'ask', label: '组合检索' },
  { id: 'ask_graph', label: '组合＋图' },
  { id: 'semantic', label: '纯语义', needs: 'embedding' as const },
  { id: 'ask_rerank', label: '组合＋重排', needs: 'rerank' as const }
]
const strategyLabel = (id: string) => strategies.find(s => s.id === id)?.label ?? id

function Report({ report }: { report: EvalReport }) {
  if (!report.summaries) return <ErrorLine>{report.message || '这份报告不可用'}</ErrorLine>
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 text-xs text-dim">
        <span>Top {report.k}</span>
        <span className="font-mono">文件版清单 {report.manifest_hash.slice(0, 16)}</span>
        <span className="font-mono">配置 {report.settings_hash.slice(0, 12)}</span>
        <Button size="sm" className="ml-auto" onClick={() => downloadJSON(report, `xrag-eval-${report.set_id}.json`)}>
          <Download />
          导出完整 JSON
        </Button>
      </div>
      <Table>
        <thead>
          <tr>
            <Th>策略</Th>
            <Th>Hit@K</Th>
            <Th>Recall@K</Th>
            <Th>MRR</Th>
            <Th>无结果题正确率</Th>
            <Th>错误 / 题数</Th>
            <Th>P95</Th>
          </tr>
        </thead>
        <tbody>
          {report.summaries.map(s => (
            <tr key={s.strategy}>
              <Td className="text-fg">{strategyLabel(s.strategy)}</Td>
              <Td className="font-mono">{pct(s.hit_at_k)}</Td>
              <Td className="font-mono">{pct(s.recall_at_k)}</Td>
              <Td className="font-mono">{s.mrr.toFixed(3)}</Td>
              <Td className="font-mono">{s.negative_accuracy == null ? <span className="font-sans text-faint">无此类题</span> : pct(s.negative_accuracy)}</Td>
              <Td className={`font-mono ${s.errors ? 'text-err' : ''}`}>
                {s.errors} / {s.cases}
              </Td>
              <Td className="font-mono">{s.p95_ms} ms</Td>
            </tr>
          ))}
        </tbody>
      </Table>
      <Note>错误计入分母；未配置向量模型时，组合检索使用关键词。</Note>
      {report.pairs.length > 0 && (
        <div className="space-y-1.5">
          <h3 className="text-[13px] font-semibold">命中差异</h3>
          {report.pairs.map(p => (
            <p key={`${p.a}-${p.b}`} className="text-[13px] text-fg-soft">
              <b className="font-medium text-fg">
                {strategyLabel(p.a)} ↔ {strategyLabel(p.b)}
              </b>
              ：仅前者命中 {p.only_a.join('、') || '无'}；仅后者命中 {p.only_b.join('、') || '无'}
            </p>
          ))}
        </div>
      )}
      <div className="space-y-1.5">
        <h3 className="text-[13px] font-semibold">逐题结果与过程</h3>
        {report.rows.map((row, i) => {
          const verdict = row.error ? '执行失败' : row.expected_empty ? (row.score.empty_correct ? '无结果，符合预期' : '出现非预期结果') : row.score.hit ? '命中' : '未命中'
          const tone = row.error ? 'err' : (row.expected_empty ? row.score.empty_correct : row.score.hit) ? 'ok' : 'warn'
          return (
            <details key={i} className="rounded-lg border border-line bg-bg">
              <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2 text-[13px]">
                <code className="text-fg">{row.case_id}</code>
                <span className="text-dim">{strategyLabel(row.strategy)}</span>
                <Badge tone={tone} dot>
                  {verdict}
                </Badge>
                <span className="min-w-0 truncate text-fg-soft">{row.question}</span>
              </summary>
              <div className="space-y-2 border-t px-3 py-2.5 text-[13px]">
                {row.error && <ErrorLine>{`${row.error.code} · ${row.error.message}`}</ErrorLine>}
                <p className="text-dim">未命中的资料：{row.score.missed_paths.join('、') || '无'}</p>
                <JsonBlock value={{ actual_strategy: row.actual_strategy, items: row.items, trace: row.trace }} summary="实际策略、结果与过程" />
              </div>
            </details>
          )
        })}
      </div>
    </div>
  )
}

function Compare() {
  const { kb, caps } = useSession()
  const run = useRun()
  const sets = useData(() => api.evalSets(), [kb])
  const runs = useData(() => api.jobs({ kb, kind: 'eval' }), [kb])
  usePolling(() => void runs.reload(), 2500, Boolean(runs.data?.items.some(j => activeStates.includes(j.state))))
  const kbSets = useMemo(() => (sets.data?.items ?? []).filter(s => s.kb === kb), [sets.data, kb])
  const [setId, setSetId] = useState('')
  const [chosen, setChosen] = useState<string[]>(['keyword', 'ask'])
  const [k, setK] = useState(5)
  const [file, setFile] = useState<File | null>(null)
  const [report, setReport] = useState<{ id: string; report: EvalReport } | null>(null)

  useEffect(() => {
    if (!kbSets.some(s => s.id === setId)) setSetId(kbSets[0]?.id ?? '')
  }, [kbSets, setId])

  async function importFile() {
    if (!file) return
    const s = await run(async () => {
      if (file.size > 256 * 1024) throw new Error('题集不能超过 256 KiB')
      return api.importEvalSet({ ...JSON.parse(await file.text()), kb })
    }, '题集已导入；每次导入保存为独立版本')
    if (s) {
      await sets.reload()
      setSetId(s.id)
    }
  }

  async function loadExample() {
    const s = await run(async () => {
      const r = await fetch('/eval-example.json')
      if (!r.ok) throw new Error('无法读取示例题集')
      return api.importEvalSet({ ...(await r.json()), kb })
    }, '已载入合成样例（对应 demo.sh 导入的 manual/ 文档）')
    if (s) {
      await sets.reload()
      setSetId(s.id)
    }
  }

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const usable = chosen.filter(id => {
      const need = strategies.find(s => s.id === id)?.needs
      return !need || caps[need]
    })
    const job = await run(async () => {
      if (!setId) throw new Error('先导入或载入一个题集')
      if (!usable.length) throw new Error('至少选择一种已配置的策略')
      return api.runEval({ set_id: setId, k, strategies: usable })
    }, '对比任务已提交')
    if (job) void runs.reload()
  }

  async function openReport(id: string) {
    const j = await run(() => api.job(id))
    if (!j) return
    if (!['succeeded', 'partial'].includes(j.state)) {
      setReport(null)
      return
    }
    setReport({ id, report: j.result as EvalReport })
  }

  const selectedSet = kbSets.find(s => s.id === setId)

  return (
    <div className="space-y-3">
      <div className="grid gap-3 lg:grid-cols-2">
        <Panel>
          <PanelHeader title="题集" meta={<span>{kbSets.length} 个版本</span>} />
          <PanelBody className="space-y-3">
            <Field label="选择题集">
              <Select value={setId} onChange={e => setSetId(e.target.value)}>
                {kbSets.length === 0 && <option value="">还没有题集</option>}
                {kbSets.map(s => (
                  <option key={s.id} value={s.id}>
                    {s.name} · {s.cases.length} 题 · {s.purpose === 'holdout' ? '留出集' : '调试集'} · {s.revision.slice(0, 8)}
                  </option>
                ))}
              </Select>
            </Field>
            <div className="flex flex-wrap items-center gap-2">
              <FilePicker accept=".json" file={file} onChange={setFile} className="max-w-xs" />
              <Button size="sm" disabled={!file} onClick={() => void importFile()}>
                <Upload />
                导入
              </Button>
              <div className="ml-auto flex flex-wrap items-center gap-2">
                <Button size="sm" variant="ghost" onClick={() => void loadExample()}>
                  <FlaskConical />
                  载入合成样例
                </Button>
                <Button size="sm" variant="ghost" disabled={!selectedSet} onClick={async () => selectedSet && downloadJSON(await api.evalSet(selectedSet.id), `xrag-eval-set-${selectedSet.id}.json`)}>
                  <Download />
                  导出
                </Button>
              </div>
            </div>
            <Note>每题填写期望文件；预期无结果的题设置 <code>expected_empty: true</code>。</Note>
          </PanelBody>
        </Panel>
        <Panel>
          <PanelHeader title="检索策略" />
          <PanelBody>
            <form onSubmit={submit} className="space-y-3">
              <div className="flex flex-wrap gap-1.5">
                {strategies.map(s => {
                  const off = s.needs && !caps[s.needs]
                  return (
                    <Chip
                      key={s.id}
                      pressed={chosen.includes(s.id) && !off}
                      disabled={Boolean(off)}
                      title={off ? (s.needs === 'embedding' ? '未配置向量模型' : '未配置重排模型') : undefined}
                      onClick={() => setChosen(c => (c.includes(s.id) ? c.filter(x => x !== s.id) : [...c, s.id]))}
                    >
                      {s.label}
                    </Chip>
                  )
                })}
              </div>
              <Field label="比较前 K 项">
                <Input type="number" min={1} max={50} value={k} onChange={e => setK(Number(e.target.value))} className="w-24" />
              </Field>
              <Button type="submit" variant="primary">
                <Play />
                运行对比
              </Button>
              <Note>对比与构建依次执行；模型调用计入本库预算。</Note>
            </form>
          </PanelBody>
        </Panel>
      </div>

      <Panel>
        <PanelHeader title="对比任务" meta={<span>{runs.data?.items.length ?? 0} 个</span>} />
        {runs.data && runs.data.items.length === 0 && <Empty mark title="还没有对比任务">先导入题集，再选择策略运行。</Empty>}
        {runs.data && runs.data.items.length > 0 && (
          <Table>
            <thead>
              <tr>
                <Th>任务</Th>
                <Th>题集</Th>
                <Th>状态</Th>
                <Th>创建</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {runs.data.items.map(j => (
                <tr key={j.id} className={report?.id === j.id ? 'bg-raised' : undefined}>
                  <Td className="font-mono text-xs text-fg">{tail(j.id, 12)}</Td>
                  <Td>{j.payload?.set?.name ?? '—'}</Td>
                  <Td>
                    <StateBadge state={j.state} />
                    {j.error && <p className="mt-1 text-xs text-err">{j.error}</p>}
                  </Td>
                  <Td className="font-mono text-xs">{shortTime(j.created)}</Td>
                  <Td className="text-right">
                    <Button size="sm" variant="ghost" disabled={!['succeeded', 'partial'].includes(j.state)} onClick={() => void openReport(j.id)}>
                      查看报告
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      {report && (
        <Panel>
          <PanelHeader title={`同题比较 · ${tail(report.id, 12)}`} />
          <PanelBody>
            <Report report={report.report} />
          </PanelBody>
        </Panel>
      )}
    </div>
  )
}

function Drafts() {
  const { kb } = useSession()
  const run = useRun()
  const drafts = useData(() => api.evalDrafts(kb), [kb])
  const [selected, setSelected] = useState<string[]>([])
  const [approving, setApproving] = useState<EvalDraft | null>(null)
  const [form, setForm] = useState({ question: '', expected_empty: false, note: '' })
  const [targets, setTargets] = useState<{ path: string; anchor: string }[]>([])
  const files = useData(() => api.tree(kb), [kb])
  const paths = (files.data?.items ?? []).map(i => i.path ?? '').filter(Boolean)
  const [pub, setPub] = useState({ name: '', family: 'default', purpose: 'development' })

  function startApprove(d: EvalDraft) {
    setForm({ question: d.case.question, expected_empty: Boolean(d.case.expected_empty), note: '' })
    setTargets((d.case.expected ?? []).map(t => ({ path: t.path, anchor: t.anchor ?? '' })))
    setApproving(d)
  }

  async function approve(event: SyntheticEvent) {
    event.preventDefault()
    if (!approving) return
    if (!form.expected_empty && !targets.some(t => t.path)) {
      await run(() => Promise.reject(new Error('至少选择一份期望资料，或勾选“明确应当没有检索结果”')))
      return
    }
    const ok = await run(
      async () =>
        api.approveDraft(approving.id, {
          expected_revision: approving.revision,
          case: {
            id: approving.case.id,
            question: form.question,
            expected_empty: form.expected_empty,
            expected: form.expected_empty ? [] : targets.filter(t => t.path).map(t => (t.anchor.trim() ? { path: t.path, anchor: t.anchor.trim() } : { path: t.path }))
          },
          note: form.note
        }),
      '已确认；还没有加入任何题集'
    )
    if (ok !== undefined) {
      setApproving(null)
      void drafts.reload()
    }
  }

  async function publish(event: SyntheticEvent) {
    event.preventDefault()
    const set = await run(async () => {
      if (!selected.length) throw new Error('至少选择一个已确认的问题')
      return api.setFromDrafts({ kb, ...pub, draft_ids: selected })
    }, '独立题集已保存')
    if (set) setSelected([])
  }

  const items = drafts.data?.items ?? []
  return (
    <div className="space-y-3">
      <Panel>
        <PanelHeader title="从反馈整理的测试问题" meta={<span>先确认期望资料，再发布为独立题集</span>} />
        {drafts.loading && !drafts.data && <Loading />}
        {drafts.data && items.length === 0 && <Empty title="还没有待确认的问题">在“反馈”页对一条反馈点“整理成测试问题”。</Empty>}
        <ul>
          {items.map(d => (
            <li key={d.id} className="flex items-start gap-3 border-b px-4 py-3">
              <input
                type="checkbox"
                className="mt-1 h-3.5 w-3.5"
                aria-label="选入题集"
                disabled={d.status !== 'approved'}
                checked={selected.includes(d.id)}
                onChange={e => setSelected(s => (e.target.checked ? [...s, d.id] : s.filter(x => x !== d.id)))}
              />
              <div className="min-w-0 flex-1">
                <p className="text-[13px] text-fg">{d.case.question || '原资料不可用'}</p>
                <p className="mt-0.5 font-mono text-xs text-dim">
                  {d.id} · 修订 {d.revision} · 反馈 {d.feedback_id}
                </p>
              </div>
              <Badge tone={d.status === 'approved' ? 'ok' : d.status === 'unavailable' ? 'off' : 'warn'}>{d.status === 'approved' ? '已确认' : d.status === 'unavailable' ? '不可用' : '待确认'}</Badge>
              {d.status !== 'unavailable' && (
                <Button size="sm" variant="ghost" onClick={() => startApprove(d)}>
                  确认问题与期望
                </Button>
              )}
            </li>
          ))}
        </ul>
      </Panel>
      <Panel>
        <PanelHeader title="发布为题集" meta={<span>已选 {selected.length} 题</span>} />
        <PanelBody>
          <form onSubmit={publish} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem_10rem_auto] sm:items-end">
            <Field label="题集名称">
              <Input required maxLength={160} value={pub.name} onChange={e => setPub({ ...pub, name: e.target.value })} />
            </Field>
            <Field label="系列">
              <Input mono required value={pub.family} onChange={e => setPub({ ...pub, family: e.target.value })} />
            </Field>
            <Field label="用途">
              <Select value={pub.purpose} onChange={e => setPub({ ...pub, purpose: e.target.value })}>
                <option value="development">调试集</option>
                <option value="holdout">留出集</option>
              </Select>
            </Field>
            <Button type="submit" variant="primary" disabled={!selected.length}>
              发布题集
            </Button>
          </form>
          <Note className="mt-3">同一系列中，调试集与留出集不能包含规范化后相同的问题。语义相近问题需人工核对；留出题答案可见。</Note>
        </PanelBody>
      </Panel>
      <Dialog open={Boolean(approving)} onOpenChange={o => !o && setApproving(null)}>
        <DialogContent>
          <DialogTitle>确认测试问题</DialogTitle>
          <form onSubmit={approve} className="contents">
            <DialogBody className="space-y-3">
              <Field label="问题">
                <Textarea required rows={2} value={form.question} onChange={e => setForm({ ...form, question: e.target.value })} />
              </Field>
              <Check label="明确应当没有检索结果" checked={form.expected_empty} onChange={e => setForm({ ...form, expected_empty: e.target.checked })} />
              {!form.expected_empty && (
                <div className="space-y-2">
                  <span className="block text-xs text-dim">期望找到的资料</span>
                  {targets.map((t, i) => (
                    <div key={i} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2">
                      <Select aria-label="期望文件" value={t.path} onChange={e => setTargets(ts => ts.map((x, j) => (j === i ? { ...x, path: e.target.value } : x)))}>
                        <option value="">选择已发布文件</option>
                        {paths.map(p => (
                          <option key={p} value={p}>
                            {p}
                          </option>
                        ))}
                      </Select>
                      <Input aria-label="原文锚点" placeholder="原文锚点（可空）" value={t.anchor} onChange={e => setTargets(ts => ts.map((x, j) => (j === i ? { ...x, anchor: e.target.value } : x)))} />
                      <Button variant="ghost" size="icon" aria-label="删除这一行" onClick={() => setTargets(ts => ts.filter((_, j) => j !== i))}>
                        <X />
                      </Button>
                    </div>
                  ))}
                  <Button size="sm" onClick={() => setTargets(ts => [...ts, { path: '', anchor: '' }])}>
                    <Plus />
                    添加一份期望资料
                  </Button>
                </div>
              )}
              <Field label="确认说明">
                <Input maxLength={8000} value={form.note} onChange={e => setForm({ ...form, note: e.target.value })} />
              </Field>
              <Note>更改期望不会修改已发布的旧题集。原始资料被撤回后，这个问题不能继续发布。</Note>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setApproving(null)}>
                取消
              </Button>
              <Button type="submit" variant="primary">
                确认并保存
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function Queries() {
  const { kb } = useSession()
  const run = useRun()
  const records = useData(() => api.queries(), [kb])
  const items = (records.data?.items ?? []).filter(q => q.kbs.includes(kb)).slice(0, 30)
  const [detail, setDetail] = useState<QueryRecord | null>(null)
  const [diagnoseFor, setDiagnoseFor] = useState<QueryRecord | null>(null)
  const [form, setForm] = useState({ path: '', anchor: '' })
  const [result, setResult] = useState<unknown>(null)

  async function diagnose(event: SyntheticEvent) {
    event.preventDefault()
    if (!diagnoseFor) return
    const r = await run(() => api.diagnose(diagnoseFor.id, { kb, ...form }))
    if (r) setResult(r)
  }

  return (
    <Panel>
      <PanelHeader title="查询记录" meta={<span>保存的候选与执行阶段 · 最多保留 2,000 条普通查询摘要</span>} actions={<Button size="sm" variant="ghost" onClick={() => void records.reload()}><RefreshCw />刷新</Button>} />
      {records.data && items.length === 0 && <Empty title="还没有查询记录">先在“检索”页执行一次查询。</Empty>}
      {items.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>查询</Th>
              <Th>动作</Th>
              <Th>结果</Th>
              <Th>时间</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {items.map(q => (
              <tr key={q.id}>
                <Td className="max-w-[320px] truncate text-fg">{q.request.q || q.request.name || q.request.pattern || <span className="font-mono text-xs text-dim">{tail(q.id, 12)}</span>}</Td>
                <Td className="font-mono text-xs">{q.response.action}</Td>
                <Td className="font-mono text-xs">{q.response.returned}</Td>
                <Td className="whitespace-nowrap font-mono text-xs">{shortTime(q.created)}</Td>
                <Td className="whitespace-nowrap text-right">
                  <Button size="sm" variant="ghost" onClick={async () => setDetail((await run(() => api.queryRecord(q.id)))?.items[0] ?? null)}>
                    保存的过程
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setForm({ path: '', anchor: '' })
                      setResult(null)
                      setDiagnoseFor(q)
                    }}
                  >
                    为什么没找到？
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Dialog open={Boolean(detail)} onOpenChange={o => !o && setDetail(null)}>
        <DialogContent side="sheet">
          <DialogTitle>保存的查询过程</DialogTitle>
          <DialogBody>
            <JsonBlock value={detail} open />
          </DialogBody>
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(diagnoseFor)} onOpenChange={o => !o && setDiagnoseFor(null)}>
        <DialogContent side="sheet">
          <DialogTitle>诊断：{diagnoseFor?.request.q ?? diagnoseFor?.id}</DialogTitle>
          <DialogBody className="space-y-3">
            <form onSubmit={diagnose} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
              <Field label="期望文件的库内路径">
                <Input mono required value={form.path} onChange={e => setForm({ ...form, path: e.target.value })} placeholder="manual/example.md" />
              </Field>
              <Field label="原文锚点（可空）">
                <Input maxLength={2000} value={form.anchor} onChange={e => setForm({ ...form, anchor: e.target.value })} />
              </Field>
              <Button type="submit" variant="primary">
                查看诊断
              </Button>
            </form>
            <Note>对照历史查询与当前资料状态；缺失的历史候选显示为 unknown。</Note>
            {result != null && <JsonBlock value={result} open summary="诊断结果" />}
          </DialogBody>
        </DialogContent>
      </Dialog>
    </Panel>
  )
}

export default function EvalPage() {
  const { profile } = useSession()
  return (
    <Page>
      <PageHeader
        title="评测"
        desc="用同一组问题比较检索策略，查看命中结果和遗漏资料。"
        cli={`xrag eval run SET_ID --wait --profile ${profile}`}
      />
      <Tabs defaultValue="compare">
        <TabsList className="-mx-1 px-1">
          <TabsTrigger value="compare">对比</TabsTrigger>
          <TabsTrigger value="drafts">待确认问题</TabsTrigger>
          <TabsTrigger value="queries">查询诊断</TabsTrigger>
        </TabsList>
        <TabsContent value="compare" className="pt-3">
          <Compare />
        </TabsContent>
        <TabsContent value="drafts" className="pt-3">
          <Drafts />
        </TabsContent>
        <TabsContent value="queries" className="pt-3">
          <Queries />
        </TabsContent>
      </Tabs>
    </Page>
  )
}
