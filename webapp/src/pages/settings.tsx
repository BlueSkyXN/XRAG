import { SyntheticEvent, useEffect, useState } from 'react'
import { Download, Save } from 'lucide-react'
import { api, download, downloadJSON, KbSettings, MaintenancePolicy, ProviderConfig, ProviderState } from '../lib/api'
import { useData, useFeedback, useRun } from '../lib/feedback'
import { navigate, useLocation } from '../lib/router'
import { useSession } from '../lib/session'
import { commaList, lines as splitLines, size, tail, when } from '../lib/utils'
import { Badge, Dot } from '../components/badge'
import { Button } from '../components/button'
import { CodeBlock, JsonBlock } from '../components/code'
import { Check, Field, Input, Select, Textarea } from '../components/input'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, KeyValues, Loading, Note, Panel, PanelBody, PanelHeader } from '../components/panel'
import { Table, Td, Th } from '../components/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/tabs'

/* ---------- 能力 ---------- */

function Capabilities() {
  const { system, caps, kb } = useSession()
  const discover = useData(() => (kb ? api.discover(kb) : Promise.resolve(null)), [kb])
  const d = discover.data?.data
  const rows: { name: string; on: boolean; state: string; desc: string }[] = [
    { name: '关键词', on: true, state: '已启用', desc: system.keyword ?? 'FTS5 BM25' },
    { name: '语义 / 向量', on: Boolean(caps.embedding), state: caps.embedding ? '已配置' : '未配置', desc: caps.qdrant ? 'Qdrant 后端；需要真实联调' : '配置 Embedding 后可做本地精确余弦检索' },
    { name: '重排', on: Boolean(caps.rerank), state: caps.rerank ? '已配置' : '未配置', desc: '组合检索的可选重排步骤' },
    { name: '关系', on: true, state: caps.graph_mode || '显式标注', desc: caps.graph_backend || system.graph_backend },
    { name: 'Chat / Wiki', on: Boolean(caps.wiki), state: caps.wiki ? '已配置' : '未配置', desc: '固定生成流程 → 草稿 → 显式发布；委托调查也需要它' },
    { name: '执行器', on: system.workers, state: system.workers ? '运行中' : '已关闭', desc: system.workers ? '构建、Wiki、评测在本进程执行' : '任务排队；可单独运行 xrag-server worker' }
  ]
  return (
    <div className="space-y-3">
      {caps.configuration_error && <ErrorLine>本库配置不完整：{caps.configuration_error}</ErrorLine>}
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {rows.map(r => (
          <Panel key={r.name} className="px-4 py-3">
            <p className="flex items-center gap-2 text-xs text-dim">
              <Dot tone={r.on ? 'ok' : 'off'} />
              {r.name}
            </p>
            <p className="mt-1 text-[15px] font-semibold">{r.state}</p>
            <p className="mt-1 text-xs leading-relaxed text-faint">{r.desc}</p>
          </Panel>
        ))}
      </div>
      <Panel>
        <PanelHeader title={`知识库 ${kb}`} meta={<span>kb discover · 只读取已登记元数据，不调用模型</span>} />
        <PanelBody>
          {d ? (
            <KeyValues
              rows={[
                ['文件 / 块', `${d.documents} / ${d.chunks}`],
                ['字段', (d.fields ?? []).join('、') || '—'],
                ['关系类型', (d.relation_types ?? []).join('、') || '—'],
                ['调查', d.service_operations?.requires_configured_chat ? '委托调查需要 Chat；固定配方不需要' : '—'],
                ['观察时间', when(d.observed_at)]
              ]}
            />
          ) : (
            <Loading />
          )}
        </PanelBody>
      </Panel>
      <JsonBlock value={system} summary="服务实际信息（/v1/system）" />
    </div>
  )
}

/* ---------- 模型与后端 ---------- */

const providerSpecs: { id: keyof ProviderConfig & string; name: string; fields: { key: string; label: string; type?: 'number'; placeholder?: string }[] }[] = [
  {
    id: 'embedding',
    name: '语义模型 · Embedding',
    fields: [
      { key: 'url', label: '服务地址', placeholder: 'http://127.0.0.1:…' },
      { key: 'model', label: '模型名称' },
      { key: 'key', label: 'Key 引用（可空）', placeholder: 'env:MODEL_KEY 或 file:/路径/key' },
      { key: 'dimension', label: '向量维度', type: 'number', placeholder: '1024' },
      { key: 'revision', label: '模型修订', placeholder: 'r1' },
      { key: 'query_prefix', label: '查询前缀' },
      { key: 'doc_prefix', label: '文档前缀' }
    ]
  },
  { id: 'chat', name: 'Wiki 生成与调查 · Chat', fields: [{ key: 'url', label: '服务地址' }, { key: 'model', label: '模型名称' }, { key: 'key', label: 'Key 引用（可空）', placeholder: 'env:MODEL_KEY' }] },
  { id: 'rerank', name: '结果重排 · Rerank', fields: [{ key: 'url', label: '服务地址' }, { key: 'model', label: '模型名称' }, { key: 'key', label: 'Key 引用（可空）' }] },
  { id: 'qdrant', name: 'Qdrant 向量后端', fields: [{ key: 'url', label: '服务地址' }, { key: 'key', label: 'Key 引用（可空）' }] },
  { id: 'neo4j', name: 'Neo4j 关系后端', fields: [{ key: 'url', label: '服务地址' }, { key: 'database', label: '数据库', placeholder: 'neo4j' }, { key: 'user', label: '用户', placeholder: 'neo4j' }, { key: 'password', label: '密码引用', placeholder: 'env:NEO4J_PASSWORD' }] }
]

function Providers() {
  const run = useRun()
  const { toast } = useFeedback()
  const state = useData(() => api.providers(), [])
  const [cfg, setCfg] = useState<ProviderConfig>({})
  const s: ProviderState | undefined = state.data
  useEffect(() => {
    if (s) setCfg(structuredClone(s.editable ? s.config : s.active))
  }, [s])
  if (!s) return state.error ? <ErrorLine>{state.error}</ErrorLine> : <Loading />

  const setField = (id: string, key: string, value: string, number?: boolean) =>
    setCfg(c => ({ ...c, [id]: { ...(c[id] as object), [key]: number ? (value === '' ? undefined : Number(value)) : value } }))
  const toggle = (id: string, on: boolean) =>
    setCfg(c => {
      const next = { ...c }
      if (on) next[id] = (s.config[id as keyof ProviderConfig] as object) ?? {}
      else delete next[id]
      return next
    })

  function clean(): ProviderConfig {
    const out: ProviderConfig = {}
    for (const [id, ep] of Object.entries(cfg)) {
      if (!ep || typeof ep !== 'object') {
        out[id] = ep
        continue
      }
      out[id] = Object.fromEntries(Object.entries(ep as Record<string, unknown>).filter(([, v]) => v !== '' && v !== undefined))
    }
    return out
  }

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const v = await run(() => api.saveProviders(s!.revision, clean()))
    if (v) {
      toast(v.restart_required ? '配置已保存。重启 Server 和所有 Worker 后生效。' : '配置已保存，运行配置没有变化。', 'ok')
      void state.reload()
    }
  }

  const banner =
    s.source === 'external_file'
      ? '当前由启动参数 --config 文件管理。这里只读；改用内置配置需要去掉 --config 并重启。'
      : s.restart_required
        ? '待启用配置与当前进程不同：重启 Server 和所有 Worker 后生效。向量模型改变后请显式重建向量。'
        : '待启用配置与运行配置一致。保存不会探测外部服务，也不会调用模型。'

  return (
    <form onSubmit={save} className="space-y-3">
      <p className={`rounded-md border px-3 py-2 text-[13px] ${s.restart_required ? 'border-warn/30 bg-warn/[0.06] text-warn' : 'border-line text-fg-soft'}`}>{banner}</p>
      <Note>只保存 env:/file: 引用，不在这里填真实密钥；引用在 Server 或 Worker 所在机器上解析。保存成功不代表外部服务已经测试通过。</Note>
      <div className="grid gap-3 xl:grid-cols-2">
        {providerSpecs.map(p => {
          const on = Boolean(cfg[p.id])
          const ep = (cfg[p.id] as Record<string, unknown>) ?? {}
          return (
            <Panel key={p.id}>
              <PanelHeader title={<Check label={p.name} checked={on} disabled={!s.editable} onChange={e => toggle(p.id, e.target.checked)} className="text-fg" />} />
              {on && (
                <PanelBody className="grid gap-3 sm:grid-cols-2">
                  {p.fields.map(f => (
                    <Field key={f.key} label={f.label}>
                      <Input
                        mono={f.key !== 'model'}
                        type={f.type ?? 'text'}
                        value={ep[f.key] === undefined ? '' : String(ep[f.key])}
                        placeholder={f.placeholder}
                        disabled={!s.editable}
                        onChange={e => setField(p.id, f.key, e.target.value, f.type === 'number')}
                      />
                    </Field>
                  ))}
                </PanelBody>
              )}
            </Panel>
          )
        })}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={!s.editable}>
          <Save />
          保存配置
        </Button>
        <Button onClick={() => void state.reload()}>重新载入</Button>
        <Button onClick={() => downloadJSON(clean(), 'providers.references.json')}>
          <Download />
          导出引用配置
        </Button>
        <span className="font-mono text-xs text-faint">
          修订 {s.revision} · {s.updated ? when(s.updated) : '尚未保存'} · 模型调用 {String((s as unknown as Record<string, unknown>).model_calls ?? 0)}
        </span>
      </div>
      <JsonBlock value={s.active} summary="当前进程实际配置" />
    </form>
  )
}

/* ---------- 知识库配置 ---------- */

function KbConfig() {
  const { kb, kbName, system, may, refresh, setKb, kbs } = useSession()
  const run = useRun()
  const { confirm } = useFeedback()
  const settings = useData(() => api.kbSettings(kb), [kb])
  const usage = useData(() => api.usage(kb), [kb])
  const [f, setF] = useState({
    parser: 'builtin',
    parser_plugin: '',
    chunk_max_chars: 1200,
    dictionary: '',
    fields: '{}',
    mode: 'explicit',
    plugin: '',
    entity_types: '',
    relation_types: '',
    include: '',
    exclude: '',
    optional: false,
    providers: 'null',
    max_calls: 200,
    max_tokens: 0,
    day_tokens: 0
  })
  const [name, setName] = useState(kbName)
  const k = settings.data

  useEffect(() => {
    if (!k) return
    const g = k.graph ?? {}
    const b = k.budget ?? {}
    setF({
      parser: k.parser_plugin ? 'commonmark' : (k.parser ?? 'builtin'),
      parser_plugin: k.parser_plugin ?? '',
      chunk_max_chars: k.chunk_max_chars ?? 1200,
      dictionary: (k.dictionary ?? []).join('\n'),
      fields: JSON.stringify(k.fields ?? {}, null, 2),
      mode: g.mode ?? 'explicit',
      plugin: g.plugin ?? '',
      entity_types: (g.entity_types ?? []).join(','),
      relation_types: (g.relation_types ?? []).join(','),
      include: (g.include ?? []).join('\n'),
      exclude: (g.exclude ?? []).join('\n'),
      optional: Boolean(g.optional),
      providers: JSON.stringify(k.providers ?? null, null, 2),
      max_calls: b.max_calls_per_run ?? 200,
      max_tokens: b.max_tokens_per_run ?? 0,
      day_tokens: b.max_tokens_per_day ?? 0
    })
  }, [k])
  useEffect(() => setName(kbName), [kbName])

  if (!k) return settings.error ? <ErrorLine>{settings.error}</ErrorLine> : <Loading />
  const plugins = Object.entries(system.plugins ?? {})
  const set = (key: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
    setF({ ...f, [key]: e.target.type === 'number' ? Number(e.target.value) : e.target.value })

  async function save(next: KbSettings, message: string) {
    const ok = await run(() => api.saveKbSettings(kb, k!.revision, next), message)
    if (ok !== undefined) {
      await refresh()
      void settings.reload()
    }
  }

  async function submitProcessing(event: SyntheticEvent) {
    event.preventDefault()
    try {
      const next: KbSettings = {
        ...k!,
        parser: f.parser,
        parser_plugin: f.parser === 'commonmark' ? f.parser_plugin : '',
        chunk_max_chars: f.chunk_max_chars,
        dictionary: splitLines(f.dictionary),
        fields: JSON.parse(f.fields),
        graph: {
          ...(k!.graph ?? {}),
          mode: f.mode,
          plugin: f.mode === 'plugin' ? f.plugin : '',
          entity_types: commaList(f.entity_types),
          relation_types: commaList(f.relation_types),
          include: splitLines(f.include),
          exclude: splitLines(f.exclude),
          optional: f.optional
        }
      }
      if (may('admin')) next.providers = JSON.parse(f.providers)
      await save(next, '配置已保存；现有资料没有重建')
    } catch (err) {
      await run(() => Promise.reject(err))
    }
  }

  async function rebuild(mode: string) {
    const job = await run(() => api.build(kb, mode))
    if (job) navigate(`/jobs?id=${job.id}`)
  }

  async function remove() {
    const ok = await confirm({
      title: `删除知识库 ${kb}`,
      body: '停止返回这个知识库的当前资料和历史引用，不删除源目录。知识库 ID 以后不能再用。',
      confirm: '删除知识库',
      danger: true,
      requireText: kb
    })
    if (!ok) return
    const done = await run(() => api.deleteKb(kb), '知识库已删除')
    if (done === undefined) return
    const next = kbs.find(x => x.id !== kb)
    await refresh()
    if (next) setKb(next.id)
    navigate('/overview')
  }

  return (
    <div className="space-y-3">
      <p className="rounded-md border border-line px-3 py-2 text-[13px] text-fg-soft">只影响知识库 <code className="text-fg">{kb}</code>。保存后新请求和新任务使用新配置；现有索引不会自动重建。</p>
      <div className="grid gap-3 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] xl:items-start">
        <Panel>
          <PanelHeader title="知识处理方式" meta={<span className="font-mono">修订 {k.revision}</span>} />
          <PanelBody>
            <form onSubmit={submitProcessing} className="grid gap-3.5 sm:grid-cols-2">
              <Field label="解析器">
                <Select value={f.parser} onChange={set('parser')}>
                  <option value="builtin">内置轻量解析</option>
                  <option value="commonmark">CommonMark＋YAML 插件（需 Server 登记）</option>
                </Select>
              </Field>
              <Field label="解析插件 ID">
                <Select value={f.parser_plugin} onChange={set('parser_plugin')} disabled={f.parser !== 'commonmark'}>
                  <option value="">Server 默认</option>
                  {plugins.filter(([, p]) => p.kind === 'parser').map(([id, p]) => (
                    <option key={id} value={id}>
                      {id} · {p.version}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="分块目标长度">
                <Input type="number" min={100} max={20000} value={f.chunk_max_chars} onChange={set('chunk_max_chars')} />
              </Field>
              <Field label="领域词典（每行一个）">
                <Textarea rows={2} value={f.dictionary} onChange={set('dictionary')} />
              </Field>
              <Field label="字段类型（JSON）" hint="string / number / boolean / date。不满足类型时文件保持旧发布版本，不会悄悄丢弃字段。" className="sm:col-span-2">
                <Textarea mono rows={3} value={f.fields} onChange={set('fields')} spellCheck={false} />
              </Field>
              <Field label="图抽取方式">
                <Select value={f.mode} onChange={set('mode')}>
                  <option value="explicit">明确标注关系</option>
                  <option value="llm">使用已配置 Chat 抽取</option>
                  <option value="plugin">Server 登记的 JSONL 抽取器</option>
                </Select>
              </Field>
              <Field label="抽取插件 ID">
                <Select value={f.plugin} onChange={set('plugin')} disabled={f.mode !== 'plugin'}>
                  <option value="">Server 默认</option>
                  {plugins.filter(([, p]) => p.kind === 'extractor').map(([id, p]) => (
                    <option key={id} value={id}>
                      {id} · {p.version}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="实体类型（逗号分隔）">
                <Input value={f.entity_types} onChange={set('entity_types')} />
              </Field>
              <Field label="关系类型（逗号分隔）">
                <Input value={f.relation_types} onChange={set('relation_types')} />
              </Field>
              <Field label="建图 include（每行一个 glob，空为全部）">
                <Textarea mono rows={2} value={f.include} onChange={set('include')} placeholder="manual/**" />
              </Field>
              <Field label="建图 exclude">
                <Textarea mono rows={2} value={f.exclude} onChange={set('exclude')} />
              </Field>
              <Check label="抽取失败时仍发布基础文本，并标明图不完整" checked={f.optional} onChange={e => setF({ ...f, optional: e.target.checked })} className="sm:col-span-2" />
              {may('admin') && (
                <Field label="本库独立模型与外部检索（JSON）" hint="null 表示继承 Server 默认；只填 env:/file: 凭据引用。" className="sm:col-span-2">
                  <Textarea mono rows={6} value={f.providers} onChange={set('providers')} spellCheck={false} />
                </Field>
              )}
              <div className="sm:col-span-2">
                <Button type="submit" variant="primary">
                  <Save />
                  保存本库配置
                </Button>
              </div>
            </form>
          </PanelBody>
        </Panel>
        <div className="space-y-3">
          <Panel>
            <PanelHeader title="单次任务与每日预算" />
            <PanelBody>
              <form
                onSubmit={e => {
                  e.preventDefault()
                  void save({ ...k, budget: { ...(k.budget ?? {}), max_calls_per_run: f.max_calls, max_tokens_per_run: f.max_tokens, max_tokens_per_day: f.day_tokens } }, '预算已保存')
                }}
                className="space-y-3"
              >
                <div className="grid grid-cols-3 gap-3">
                  <Field label="每次最多模型调用">
                    <Input type="number" min={0} value={f.max_calls} onChange={set('max_calls')} />
                  </Field>
                  <Field label="单次 Token（0 不设）">
                    <Input type="number" min={0} value={f.max_tokens} onChange={set('max_tokens')} />
                  </Field>
                  <Field label="每日 Token（UTC）">
                    <Input type="number" min={0} value={f.day_tokens} onChange={set('day_tokens')} />
                  </Field>
                </div>
                <Note>发送前按请求字节和输出上限保守预留，收到 usage 后结算；响应未知时保留预留额。预留不是精确分词，也不是供应商账单。</Note>
                <Button type="submit">保存预算</Button>
              </form>
            </PanelBody>
          </Panel>
          <Panel>
            <PanelHeader title="专项重建" />
            <PanelBody className="space-y-2">
              <div className="flex flex-wrap gap-2">
                <Button onClick={() => void rebuild('vectors_only')}>仅重建向量</Button>
                <Button onClick={() => void rebuild('graph_only')}>仅重建图</Button>
                <Button onClick={() => void rebuild('full')}>完整重建</Button>
              </div>
              <Note>专项重建使用已发布正文。向量模式准备好全部文件后一次切换；失败时保持原来的查询模型和资料。</Note>
            </PanelBody>
          </Panel>
          <Panel>
            <PanelHeader title="名称与删除" />
            <PanelBody className="space-y-3">
              <form
                className="flex items-end gap-2"
                onSubmit={async e => {
                  e.preventDefault()
                  const ok = await run(() => api.renameKb(kb, name.trim()), '名称已修改；ID 与引用不变')
                  if (ok !== undefined) await refresh()
                }}
              >
                <Field label="显示名称" className="flex-1">
                  <Input required value={name} onChange={e => setName(e.target.value)} />
                </Field>
                <Button type="submit" disabled={!name.trim() || name.trim() === kbName}>
                  改名
                </Button>
              </form>
              {may('admin') && (
                <Button variant="danger" onClick={() => void remove()}>
                  删除知识库
                </Button>
              )}
            </PanelBody>
          </Panel>
          <Panel>
            <PanelHeader title="模型调用与用量" actions={<Button size="sm" variant="ghost" onClick={() => void usage.reload()}>刷新</Button>} />
            {usage.data && usage.data.totals.length === 0 && <Empty title="还没有模型调用记录" />}
            {usage.data && usage.data.totals.length > 0 && (
              <Table>
                <thead>
                  <tr>
                    <Th>操作 · 状态</Th>
                    <Th>调用</Th>
                    <Th>记账 Token</Th>
                  </tr>
                </thead>
                <tbody>
                  {usage.data.totals.map((x, i) => (
                    <tr key={i}>
                      <Td>
                        {x.operation} · {x.state}
                      </Td>
                      <Td className="font-mono">{x.calls}</Td>
                      <Td className="font-mono">{x.tokens}</Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            {usage.data && <JsonBlock value={usage.data} summary="完整记录" className="m-3" />}
          </Panel>
        </div>
      </div>
    </div>
  )
}

/* ---------- 数据 ---------- */

function DataPane() {
  const { kb, may } = useSession()
  const run = useRun()
  const { confirm } = useFeedback()
  const admin = may('admin')
  const backups = useData(admin ? () => api.backups() : null, [admin])
  const [cache, setCache] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  async function createBackup() {
    if (!(await confirm({ title: '创建 SQLite 快照？', body: '快照包含入库正文、版本、草稿、反馈和 Key 摘要；不包含外部来源目录、实际 Key 和外部数据库。大库备份会占用磁盘与查询连接。', confirm: '创建快照' }))) return
    setBusy(true)
    await run(() => api.createBackup(), '快照已完成')
    setBusy(false)
    void backups.reload()
  }

  async function removeBackup(id: string) {
    if (!(await confirm({ title: `删除快照 ${id}？`, body: '只删除这份备份，不影响正在运行的资料库。', confirm: '删除', danger: true }))) return
    await run(() => api.removeBackup(id), '快照已删除')
    void backups.reload()
  }

  return (
    <div className="space-y-3">
      <div className="grid gap-3 lg:grid-cols-2">
        <Panel>
          <PanelHeader title="导出当前知识库" />
          <PanelBody className="space-y-3">
            <p className="text-[13px] text-fg-soft">导出 {kb} 全部已发布的 MD/TXT 和来源清单，不只是某次搜索的前 K 项。不包含草稿、密钥或向量。</p>
            <Button variant="primary" onClick={() => void run(() => download(api.exportPath(kb), `xrag-${kb}-published.zip`), '文件已交给浏览器保存')}>
              <Download />
              下载已发布资料 ZIP
            </Button>
            <Note>上限 10,000 份文件、正文 64 MiB。这是内容导出，不是完整系统备份。</Note>
          </PanelBody>
        </Panel>
        {admin && (
          <Panel>
            <PanelHeader title="数据库快照" meta={backups.data && <span>最多保留 {backups.data.max_snapshots} 个</span>} actions={<Button size="sm" variant="primary" disabled={busy} onClick={() => void createBackup()}>{busy ? '创建中…' : '创建快照'}</Button>} />
            {backups.data && backups.data.items.length === 0 && <Empty title="还没有快照" />}
            {backups.data && backups.data.items.length > 0 && (
              <Table>
                <thead>
                  <tr>
                    <Th>快照</Th>
                    <Th>大小</Th>
                    <Th>时间</Th>
                    <Th />
                  </tr>
                </thead>
                <tbody>
                  {backups.data.items.map(b => (
                    <tr key={b.id}>
                      <Td>
                        <code className="text-fg">{b.id}</code>
                        <span className="mt-0.5 block font-mono text-[11px] text-faint" title={b.sha256}>
                          sha256 {b.sha256.slice(0, 16)}…
                        </span>
                      </Td>
                      <Td className="font-mono text-xs">{size(b.size)}</Td>
                      <Td className="whitespace-nowrap text-xs">
                        {when(b.created)}
                        {!b.available && <Badge tone="warn" className="ml-2">文件不在原位置</Badge>}
                      </Td>
                      <Td className="whitespace-nowrap text-right">
                        <Button size="sm" variant="ghost" disabled={!b.available} onClick={() => void run(() => download(api.backupPath(b.id), `${b.id}.sqlite`), '文件已交给浏览器保存')}>
                          下载
                        </Button>
                        <Button size="sm" variant="ghost" className="text-err" onClick={() => void removeBackup(b.id)}>
                          删除
                        </Button>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </Panel>
        )}
      </div>
      {admin && (
        <div className="grid gap-3 lg:grid-cols-2">
          <Panel>
            <PanelHeader title="恢复到一个新目录" />
            <PanelBody className="space-y-3">
              <p className="text-[13px] text-fg-soft">恢复必须在命令行显式执行，网页不能覆盖运行中的库。新目录会生成恢复管理员 Key，并关闭原自动扫描与模型配置。</p>
              <CodeBlock label="命令" code={'./bin/xrag-server restore --from snapshot.sqlite \\\n  --data /新的/不存在的目录 --confirm'} />
              <Note>文档身份和旧引用保留；原 Key 停用、排队任务停止。外部来源路径要重新确认后再启用。</Note>
            </PanelBody>
          </Panel>
          <Panel>
            <PanelHeader title="清理过期临时数据" />
            <PanelBody className="space-y-3">
              <p className="text-[13px] text-fg-soft">只清理已过期的结果页、范围句柄和查询摘要；不删除知识、历史文件、模型缓存或任务。</p>
              <div className="flex gap-2">
                <Button onClick={async () => setCache((await run(() => api.cachePreview())) ?? null)}>查看可清理数量</Button>
                <Button
                  variant="danger"
                  onClick={async () => {
                    if (!(await confirm({ title: '清理过期临时数据？', body: '不会清理正文或历史版本。', confirm: '确认清理', danger: true }))) return
                    setCache((await run(() => api.cacheClean(), '已清理')) ?? null)
                  }}
                >
                  确认清理
                </Button>
              </div>
              {cache != null && <JsonBlock value={cache} open summary="结果" />}
              <CodeBlock label="退休向量集合回收（离线）" code={'./bin/xrag-server vector-gc --data /资料目录 --keep-days 30\n# 确认后才加 --confirm'} />
            </PanelBody>
          </Panel>
        </div>
      )}
    </div>
  )
}

/* ---------- 自动维护 ---------- */

function Maintenance() {
  const run = useRun()
  const { confirm } = useFeedback()
  const policy = useData(() => api.maintenancePolicy(), [])
  const runs = useData(() => api.maintenanceRuns(), [])
  const [p, setP] = useState<MaintenancePolicy | null>(null)
  const [preview, setPreview] = useState<unknown>(null)
  useEffect(() => {
    if (policy.data) setP(policy.data.policy)
  }, [policy.data])
  if (!p || !policy.data) return policy.error ? <ErrorLine>{policy.error}</ErrorLine> : <Loading />

  const num = (key: keyof MaintenancePolicy) => (e: React.ChangeEvent<HTMLInputElement>) => setP({ ...p, [key]: Number(e.target.value) })
  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const ok = await run(() => api.saveMaintenancePolicy(policy.data!.revision, p!), '计划已保存，没有立即执行')
    if (ok !== undefined) void policy.reload()
  }
  async function runOnce(body: { backup?: boolean; records?: boolean; content?: boolean }, title: string) {
    if (!(await confirm({ title, confirm: '排队执行' }))) return
    await run(() => api.runMaintenance(body), '已排队，等待执行器')
    void runs.reload()
  }

  return (
    <div className="space-y-3">
      <p className="rounded-md border border-line px-3 py-2 text-[13px] text-fg-soft">默认关闭，保存不会立即执行。只在带执行器的 Server 或独立 Worker 中运行。快照只含 SQLite，不含外部向量库、图库、来源目录或实际凭据。</p>
      <div className="grid gap-3 lg:grid-cols-2 lg:items-start">
        <Panel>
          <PanelHeader title="维护计划" meta={<span className="font-mono">修订 {policy.data.revision}</span>} />
          <PanelBody>
            <form onSubmit={save} className="grid gap-3 sm:grid-cols-2">
              <Check label="启用计划" checked={p.enabled} onChange={e => setP({ ...p, enabled: e.target.checked })} className="sm:col-span-2" />
              <Field label="备份间隔（秒，0 关闭）">
                <Input type="number" min={0} max={604800} value={p.backup_every_seconds} onChange={num('backup_every_seconds')} />
              </Field>
              <Field label="保留计划快照数">
                <Input type="number" min={1} max={15} value={p.backup_keep} onChange={num('backup_keep')} />
              </Field>
              <Field label="记录整理间隔（秒，0 关闭）">
                <Input type="number" min={0} max={604800} value={p.records_every_seconds} onChange={num('records_every_seconds')} />
              </Field>
              <Field label="操作记录保留天数">
                <Input type="number" min={1} max={36500} value={p.records_keep_days} onChange={num('records_keep_days')} />
              </Field>
              <Field label="内容整理间隔（秒，0 关闭）">
                <Input type="number" min={0} max={604800} value={p.content_every_seconds} onChange={num('content_every_seconds')} />
              </Field>
              <Field label="历史内容最低保留天数">
                <Input type="number" min={1} max={36500} value={p.content_keep_days} onChange={num('content_keep_days')} />
              </Field>
              <Field label="每批最多处理记录">
                <Input type="number" min={1} max={1000} value={p.batch_size} onChange={num('batch_size')} />
              </Field>
              <div className="flex items-end pb-1.5">
                <Check label="允许按原绑定清理外部文件版" checked={p.content_external} onChange={e => setP({ ...p, content_external: e.target.checked })} />
              </div>
              <Note className="sm:col-span-2">已结算用量先汇总再整理；结果未知的调用不清除。知识依据和人工题集不直接删除；手动快照不会被计划删除；关闭计划不取消正在执行的任务。</Note>
              <div className="sm:col-span-2">
                <Button type="submit" variant="primary">
                  <Save />
                  保存计划
                </Button>
              </div>
            </form>
          </PanelBody>
        </Panel>
        <Panel>
          <PanelHeader title="现在运行一次" />
          <PanelBody className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <Button onClick={async () => setPreview((await run(() => api.maintenanceRecords())) ?? null)}>预览旧记录</Button>
              <Button onClick={async () => setPreview((await run(() => api.maintenanceContent())) ?? null)}>预览历史内容</Button>
              <Button onClick={() => void runOnce({ backup: true }, '排队创建一次 SQLite 快照，并按保留数整理计划快照？')}>备份一次</Button>
              <Button onClick={() => void runOnce({ records: true }, '排队整理一批过期操作记录？保留用量汇总，不删除正文与依据。')}>整理一批记录</Button>
              <Button onClick={() => void runOnce({ content: true }, '整理一批超过保留期、没有引用持有的历史内容？本批次会短暂阻挡内容操作。')}>整理一批内容</Button>
            </div>
            <KeyValues
              rows={[
                ['下次备份', when(policy.data.next_backup_at)],
                ['下次记录整理', when(policy.data.next_records_at)],
                ['下次内容整理', when(policy.data.next_content_at)]
              ]}
            />
            {preview != null && <JsonBlock value={preview} open summary="预览" />}
          </PanelBody>
        </Panel>
      </div>
      <Panel>
        <PanelHeader title="最近维护任务" actions={<Button size="sm" variant="ghost" onClick={() => void runs.reload()}>刷新</Button>} />
        {runs.data && runs.data.items.length === 0 && <Empty title="还没有维护任务">默认不会自动删除或备份。</Empty>}
        {runs.data && runs.data.items.length > 0 && (
          <Table>
            <thead>
              <tr>
                <Th>任务</Th>
                <Th>内容</Th>
                <Th>状态</Th>
                <Th>时间</Th>
              </tr>
            </thead>
            <tbody>
              {runs.data.items.map(x => (
                <tr key={x.id}>
                  <Td className="font-mono text-xs text-fg">{tail(x.id, 12)}</Td>
                  <Td>
                    {[x.backup && '数据库快照', x.records && '记录整理', x.content && '内容整理'].filter(Boolean).join(' · ')} · {x.automatic ? '计划' : '显式提交'}
                    {x.error && <p className="text-xs text-err">{x.error}</p>}
                  </Td>
                  <Td>{x.state}</Td>
                  <Td className="whitespace-nowrap font-mono text-xs">{when(x.created)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
    </div>
  )
}

/* ---------- 页面 ---------- */

const tabs = [
  { id: 'capabilities', label: '能力', min: 'reader' as const },
  { id: 'providers', label: '模型与后端', min: 'admin' as const },
  { id: 'kb', label: '知识库配置', min: 'maintainer' as const },
  { id: 'data', label: '数据', min: 'maintainer' as const },
  { id: 'maintenance', label: '自动维护', min: 'admin' as const }
]

export default function SettingsPage() {
  const { may, profile, kb } = useSession()
  const location = useLocation()
  const visible = tabs.filter(t => may(t.min))
  const current = visible.find(t => t.id === location.segments[1])?.id ?? 'capabilities'
  const cli: Record<string, string> = {
    capabilities: `xrag settings show --profile ${profile}`,
    providers: `xrag settings show --profile ${profile}`,
    kb: `xrag kb settings ${kb} --profile ${profile}`,
    data: `xrag backup create --confirm --profile ${profile}`,
    maintenance: `xrag maintenance preview --profile ${profile}`
  }
  return (
    <Page>
      <PageHeader title="设置" desc="能力、模型连接、知识库处理方式和数据维护。打开设置不会探测网络或调用模型。" cli={cli[current]} />
      <Tabs value={current} onValueChange={v => navigate(`/settings/${v}`, true)}>
        <TabsList className="-mx-1 px-1">
          {visible.map(t => (
            <TabsTrigger key={t.id} value={t.id}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="capabilities" className="pt-3">
          <Capabilities />
        </TabsContent>
        {may('admin') && (
          <TabsContent value="providers" className="pt-3">
            <Providers />
          </TabsContent>
        )}
        {may('maintainer') && (
          <TabsContent value="kb" className="pt-3">
            {kb ? <KbConfig /> : <Empty title="先选择一个知识库" />}
          </TabsContent>
        )}
        {may('maintainer') && (
          <TabsContent value="data" className="pt-3">
            {kb ? <DataPane /> : <Empty title="先选择一个知识库" />}
          </TabsContent>
        )}
        {may('admin') && (
          <TabsContent value="maintenance" className="pt-3">
            <Maintenance />
          </TabsContent>
        )}
      </Tabs>
    </Page>
  )
}
