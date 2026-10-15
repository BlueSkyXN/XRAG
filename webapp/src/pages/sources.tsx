import { SyntheticEvent, useEffect, useState } from 'react'
import { BookOpenText, Calculator, ChevronDown, FileUp, MoreHorizontal, Play, Plus, Search as SearchIcon } from 'lucide-react'
import { api, Item, SourceDetail } from '../lib/api'
import { useData, useFeedback, useRun } from '../lib/feedback'
import { navigate } from '../lib/router'
import { useSession } from '../lib/session'
import { lines as splitLines, shortTime, tail, when } from '../lib/utils'
import { Badge, Dot, StateBadge } from '../components/badge'
import { Button } from '../components/button'
import { JsonBlock } from '../components/code'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Check, Field, FilePicker, Input, Select, Textarea } from '../components/input'
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from '../components/menu'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, Loading, Note, Panel, PanelHeader } from '../components/panel'
import { Pipeline, Stage } from '../components/pipeline'
import { Table, Td, Th } from '../components/table'
import { ReaderSheet, ReadTarget } from '../features/reader'

const buildModes = [
  { mode: 'incremental', label: '增量构建', hint: '只处理有变化的文件' },
  { mode: 'full', label: '完整构建', hint: '重新扫描全部来源' },
  { mode: 'vectors_only', label: '只建向量', hint: '用已发布正文重建向量，需要向量模型' },
  { mode: 'graph_only', label: '只建图', hint: '用已发布正文重新抽取关系' }
]

const kindLabels: Record<string, string> = { path: '目录', git: 'Git', s3: 'S3', weknora: 'WeKnora', builtin: '上传' }

function sourceState(d: SourceDetail): { label: string; tone: 'ok' | 'warn' | 'err' | 'off' } {
  if (d.source.options?.disabled) return { label: '已停用', tone: 'off' }
  if (d.status.error) return { label: '上次检查未完成', tone: 'err' }
  if (!d.status.indexed_fingerprint) return { label: '等待首次构建', tone: 'warn' }
  if (d.status.observed_fingerprint && d.status.observed_fingerprint !== d.status.indexed_fingerprint) return { label: '有未构建的变化', tone: 'warn' }
  return { label: '已是最新', tone: 'ok' }
}

function triggerText(p: SourceDetail['policy']) {
  const parts: string[] = []
  if (p.schedule) parts.push(`Cron ${p.schedule}`)
  if (p.every_seconds > 0) parts.push(`每 ${p.every_seconds} 秒`)
  if (p.watch_seconds > 0) parts.push(`轮询 ${p.watch_seconds} 秒`)
  if (p.native_watch) parts.push('文件事件')
  if (p.hook_secret_ref || (p.hook_provider && p.hook_provider !== 'custom')) parts.push(`Hook${p.hook_provider && p.hook_provider !== 'custom' ? ` · ${p.hook_provider}` : ''}`)
  return parts.length ? parts.join(' · ') : '手动'
}

function RegisterSource({ open, onOpenChange, onDone }: { open: boolean; onOpenChange: (v: boolean) => void; onDone: () => void }) {
  const { kb } = useSession()
  const run = useRun()
  const [kind, setKind] = useState('path')
  const [form, setForm] = useState({ id: '', root: '', mount: '', git_ref: 'HEAD', remote: '' })
  const remoteTemplates: Record<string, object> = {
    s3: { bucket: 'your-bucket', prefix: 'docs/', region: 'us-east-1', access_key: 'env:S3_ACCESS_KEY', secret_key: 'env:S3_SECRET_KEY' },
    weknora: { knowledge_base: 'upstream-kb-id', api_key: 'env:WEKNORA_API_KEY', auth_header: 'X-API-Key' }
  }
  useEffect(() => {
    if (remoteTemplates[kind]) setForm(f => ({ ...f, remote: JSON.stringify(remoteTemplates[kind], null, 2) }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind])

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const body: Record<string, unknown> = { id: form.id.trim(), kind, root: form.root.trim(), mount: form.mount.trim() }
    if (kind === 'git') body.git_ref = form.git_ref.trim() || 'HEAD'
    const ok = await run(async () => {
      if (remoteTemplates[kind]) body.remote = JSON.parse(form.remote)
      return api.addSource(kb, body)
    }, '来源已登记。手动构建或开启自动更新后才会读取。')
    if (ok !== undefined) {
      onOpenChange(false)
      onDone()
    }
  }

  const set = (key: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setForm(f => ({ ...f, [key]: e.target.value }))
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>登记来源</DialogTitle>
        <form onSubmit={submit} className="contents">
          <DialogBody className="grid gap-4 sm:grid-cols-2">
            <Field label="来源 ID" hint="英文、数字、下划线；登记后不能改。">
              <Input mono required pattern="[A-Za-z0-9_-]+" value={form.id} onChange={set('id')} placeholder="project-docs" />
            </Field>
            <Field label="类型">
              <Select value={kind} onChange={e => setKind(e.target.value)}>
                <option value="path">服务器目录</option>
                <option value="git">Git 仓库</option>
                <option value="s3">S3 兼容对象存储</option>
                <option value="weknora">WeKnora 已解析文本</option>
              </Select>
            </Field>
            <Field label={kind === 'git' ? '仓库路径或地址' : kind === 's3' ? '服务地址' : kind === 'weknora' ? 'WeKnora 地址' : '服务器上的绝对路径'} className="sm:col-span-2">
              <Input mono required value={form.root} onChange={set('root')} placeholder={kind === 'git' ? '/srv/repo 或 https://…/repo.git' : kind === 'path' ? '/data/docs' : 'https://…'} />
            </Field>
            <Field label="挂载前缀" hint="资料在知识库里的路径前缀。">
              <Input mono required value={form.mount} onChange={set('mount')} placeholder="project" />
            </Field>
            {kind === 'git' && (
              <Field label="分支 / 标签 / 提交" hint="只读取已提交的文件树，不切换源仓库分支。">
                <Input mono value={form.git_ref} onChange={set('git_ref')} />
              </Field>
            )}
            {remoteTemplates[kind] && (
              <Field label="连接选项 JSON" hint="凭据只填 env:/file: 引用。登记不会读取来源；远程来源尚未在真实云端实例联调。" className="sm:col-span-2">
                <Textarea mono rows={7} value={form.remote} onChange={set('remote')} spellCheck={false} />
              </Field>
            )}
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" variant="primary">
              登记来源
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function PolicyDialog({ detail, onClose, onDone }: { detail: SourceDetail | null; onClose: () => void; onDone: () => void }) {
  const { kb } = useSession()
  const run = useRun()
  const [p, setP] = useState<SourceDetail['policy'] | null>(null)
  useEffect(() => setP(detail ? { timezone: 'UTC', hook_provider: 'custom', ...detail.policy } : null), [detail])
  if (!detail || !p) return null
  const s = detail.source
  const num = (key: 'every_seconds' | 'watch_seconds' | 'debounce_seconds') => (e: React.ChangeEvent<HTMLInputElement>) => setP({ ...p, [key]: Number(e.target.value) })
  const str = (key: 'schedule' | 'timezone' | 'hook_secret_ref' | 'hook_repository' | 'hook_ref') => (e: React.ChangeEvent<HTMLInputElement>) => setP({ ...p, [key]: e.target.value })

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const policy = { ...p! }
    if (s.kind === 'git') policy.watch_seconds = 0
    if (policy.hook_provider === 'custom') {
      policy.hook_repository = ''
      policy.hook_ref = ''
    }
    const ok = await run(() => api.sourcePolicy(kb, s.id, policy), '自动更新设置已保存。没有变化时不会创建构建任务。')
    if (ok !== undefined) {
      onClose()
      onDone()
    }
  }

  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent>
        <DialogTitle>自动更新 · {s.id}</DialogTitle>
        <form onSubmit={submit} className="contents">
          <DialogBody className="grid gap-4 sm:grid-cols-2">
            <Field label="定时检查间隔（秒，0 关闭）">
              <Input type="number" min={0} value={p.every_seconds} onChange={num('every_seconds')} />
            </Field>
            <Field label="目录轮询间隔（秒，0 关闭）" hint={s.kind === 'git' ? 'Git 来源不使用目录轮询。' : '按内容指纹判断变化。'}>
              <Input type="number" min={0} value={p.watch_seconds} onChange={num('watch_seconds')} disabled={s.kind === 'git'} />
            </Field>
            <Field label="Cron（五字段，可空）">
              <Input mono value={p.schedule ?? ''} onChange={str('schedule')} placeholder="*/15 * * * *" />
            </Field>
            <Field label="时区">
              <Input mono value={p.timezone ?? 'UTC'} onChange={str('timezone')} />
            </Field>
            <Field label="变化稳定等待（秒）">
              <Input type="number" min={1} max={300} value={p.debounce_seconds || 10} onChange={num('debounce_seconds')} />
            </Field>
            <div className="flex items-end pb-1.5">
              <Check
                label="本机文件事件（Linux inotify；其他系统定时兜底）"
                checked={Boolean(p.native_watch)}
                disabled={!['path', 'builtin'].includes(s.kind)}
                onChange={e => setP({ ...p, native_watch: e.target.checked })}
              />
            </div>
            <div className="space-y-3 rounded-lg border border-line p-3 sm:col-span-2">
              <p className="text-xs text-dim">
                推送通知 · 接收地址 <code className="text-fg">/v1/hooks/{kb}/{s.id}</code>
              </p>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="协议">
                  <Select value={p.hook_provider ?? 'custom'} onChange={e => setP({ ...p, hook_provider: e.target.value })}>
                    <option value="custom">XRAG v1</option>
                    {s.kind === 'git' && (
                      <>
                        <option value="github">GitHub Push</option>
                        <option value="gitlab">GitLab Push / Tag Push</option>
                        <option value="gitea">Gitea Push</option>
                      </>
                    )}
                  </Select>
                </Field>
                <Field label="密钥引用（可空）">
                  <Input mono value={p.hook_secret_ref ?? ''} onChange={str('hook_secret_ref')} placeholder="env:XRAG_HOOK_SECRET" />
                </Field>
                {p.hook_provider && p.hook_provider !== 'custom' && (
                  <>
                    <Field label="仓库路径">
                      <Input mono value={p.hook_repository ?? ''} onChange={str('hook_repository')} placeholder="group/project" />
                    </Field>
                    <Field label="选定 ref">
                      <Input mono value={p.hook_ref ?? ''} onChange={str('hook_ref')} placeholder="refs/heads/main" />
                    </Field>
                  </>
                )}
              </div>
              <Note>通知只安排重新扫描已登记的来源，不读取请求里的仓库地址，也不执行提交中的命令。</Note>
            </div>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => setP({ ...p, every_seconds: 0, watch_seconds: 0, schedule: '', native_watch: false })}
            >
              全部关闭
            </Button>
            <Button variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" variant="primary">
              保存设置
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function PatternDialog({ detail, onClose, onDone }: { detail: SourceDetail | null; onClose: () => void; onDone: () => void }) {
  const { kb } = useSession()
  const run = useRun()
  const [include, setInclude] = useState('')
  const [exclude, setExclude] = useState('')
  useEffect(() => {
    setInclude((detail?.source.options?.include ?? []).join('\n'))
    setExclude((detail?.source.options?.exclude ?? []).join('\n'))
  }, [detail])
  if (!detail) return null
  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const s = detail!.source
    const ok = await run(
      () => api.editSource(kb, s.id, { expected_revision: s.options?.revision ?? 0, include: splitLines(include), exclude: splitLines(exclude) }),
      '范围已保存。下次完整构建按新范围更新。'
    )
    if (ok !== undefined) {
      onClose()
      onDone()
    }
  }
  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogTitle>包含 / 排除 · {detail.source.id}</DialogTitle>
        <form onSubmit={submit} className="contents">
          <DialogBody className="space-y-4">
            <Field label="include（每行一个 glob，空为全部）">
              <Textarea mono rows={3} value={include} onChange={e => setInclude(e.target.value)} placeholder="manual/**" />
            </Field>
            <Field label="exclude（每行一个 glob）">
              <Textarea mono rows={3} value={exclude} onChange={e => setExclude(e.target.value)} />
            </Field>
            <Note>保存不扫描来源。</Note>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" variant="primary">
              保存范围
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function UploadDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { kb } = useSession()
  const run = useRun()
  const [file, setFile] = useState<File | null>(null)
  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (!file) return
    const job = await run(async () => {
      if (file.size > 5 * 1024 * 1024) throw new Error('单个文件不能超过 5 MiB')
      return api.upload(kb, file.name, await file.text())
    })
    if (job) {
      onOpenChange(false)
      navigate(`/jobs?id=${job.id}`)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogTitle>上传 Markdown / TXT</DialogTitle>
        <form onSubmit={submit} className="contents">
          <DialogBody className="space-y-3">
            <FilePicker accept=".md,.markdown,.txt" file={file} onChange={setFile} label="选择文件" />
            <Note>文件进入“上传”来源，并自动提交一次构建。单个文件不超过 5 MiB。</Note>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" variant="primary" disabled={!file}>
              上传并构建
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export default function SourcesPage() {
  const { kb, may, caps, profile, setScope } = useSession()
  const run = useRun()
  const { confirm } = useFeedback()
  const maintainer = may('maintainer')

  const files = useData(() => api.tree(kb), [kb])
  const discover = useData(() => api.discover(kb), [kb])
  const sources = useData(maintainer ? () => api.sources(kb) : null, [kb, maintainer])
  const builds = useData(() => api.jobs({ kb, kind: 'build' }), [kb])

  const [registerOpen, setRegisterOpen] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [policyFor, setPolicyFor] = useState<SourceDetail | null>(null)
  const [patternFor, setPatternFor] = useState<SourceDetail | null>(null)
  const [estimate, setEstimate] = useState<Record<string, unknown> | null>(null)
  const [reading, setReading] = useState<ReadTarget | null>(null)

  const details = sources.data?.details ?? []
  const latest = builds.data?.items[0]
  const d = discover.data?.data

  async function build(mode: string) {
    const job = await run(() => api.build(kb, mode))
    if (job) navigate(`/jobs?id=${job.id}`)
  }

  async function onlyThisFile(item: Item) {
    const r = await run(() => api.tree(kb, { filters: [{ field: 'path', op: 'eq', value: item.path }], save_scope: 'required' }))
    if (r?.handle) {
      setScope({ handle: r.handle, label: `只查 ${item.path}` })
      navigate('/search')
    }
  }

  async function toggleSource(detail: SourceDetail) {
    const s = detail.source
    await run(
      () => api.editSource(kb, s.id, { expected_revision: s.options?.revision ?? 0, disabled: !s.options?.disabled }),
      s.options?.disabled ? '来源已启用' : '来源已停用；已发布资料保留，但不再读取来源'
    )
    void sources.reload()
  }

  async function removeSource(detail: SourceDetail) {
    const s = detail.source
    const ok = await confirm({
      title: `移除来源 ${s.id}`,
      body: '来源下的资料会移出当前检索；原始目录不改，允许的历史引用保留。这个来源 ID 以后不能再用。',
      confirm: '移除来源',
      danger: true,
      requireText: s.id
    })
    if (!ok) return
    await run(() => api.removeSource(kb, s.id, s.options?.revision ?? 0), '来源已移除')
    void sources.reload()
    void files.reload()
  }

  const stages: Stage[] = [
    { label: '来源', value: maintainer ? details.length : '—', title: maintainer ? undefined : '查看来源需要 Maintainer' },
    { label: '已发布文件', value: d?.documents ?? files.data?.total.value ?? '—' },
    { label: '文本块', value: d?.chunks ?? '—' },
    { label: '全文索引', value: d?.chunks ?? '—', unit: '块', route: 'kw' },
    { label: caps.embedding ? '向量' : '向量 · 未配置', value: caps.embedding ? '已配置' : '—', route: 'sem', skipped: !caps.embedding },
    { label: '图 · 关系类型', value: d?.relation_types?.length ?? '—', unit: '类', route: 'gr' }
  ]

  return (
    <Page>
      <PageHeader
        title="资料与来源"
        desc="登记来源、构建并发布。新版本准备好之后才替换当前文件；查询始终读取已发布的文件版本。"
        cli={`xrag build run ${kb} --mode incremental --wait --profile ${profile}`}
        actions={
          maintainer && (
            <>
              <Button variant="ghost" onClick={async () => setEstimate((await run(() => api.estimate(kb))) ?? null)}>
                <Calculator />
                估算
              </Button>
              <Button variant="ghost" onClick={() => setUploadOpen(true)}>
                <FileUp />
                上传文件
              </Button>
              <Menu>
                <MenuTrigger asChild>
                  <Button variant="primary">
                    <Play />
                    构建
                    <ChevronDown />
                  </Button>
                </MenuTrigger>
                <MenuContent>
                  <MenuLabel>构建方式</MenuLabel>
                  {buildModes.map(m => (
                    <MenuItem key={m.mode} disabled={m.mode === 'vectors_only' && !caps.embedding} onSelect={() => void build(m.mode)} className="h-auto items-start py-1.5">
                      <code className="w-24 shrink-0 text-[12px]">{m.mode}</code>
                      <span className="text-xs leading-relaxed text-dim">
                        {m.label} · {m.mode === 'vectors_only' && !caps.embedding ? '未配置向量模型' : m.hint}
                      </span>
                    </MenuItem>
                  ))}
                </MenuContent>
              </Menu>
            </>
          )
        }
      />

      <Pipeline
        label="资料链路"
        stages={stages}
        total={{ label: latest ? `最近构建 · ${buildModes.find(m => m.mode === latest.payload?.mode)?.label ?? latest.payload?.mode ?? ''}` : '最近构建', value: latest ? `${shortTime(latest.created)} ${latest.state === 'succeeded' ? '成功' : latest.state}` : '还没有' }}
      />

      <div className="grid gap-3 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        {maintainer ? (
          <Panel>
            <PanelHeader
              title="来源"
              meta={<span>{details.length} 个</span>}
              actions={
                <Button size="sm" onClick={() => setRegisterOpen(true)}>
                  <Plus />
                  登记来源
                </Button>
              }
            />
            {sources.loading && !sources.data && <Loading />}
            <ErrorLine className="m-4">{sources.error}</ErrorLine>
            {sources.data && details.length === 0 && (
              <Empty mark title="还没有来源">上传文件会自动建立“上传”来源；也可以登记服务器目录、Git 仓库、S3 或 WeKnora。</Empty>
            )}
            {details.length > 0 && (
              <Table>
                <thead>
                  <tr>
                    <Th>ID</Th>
                    <Th>类型 · 位置</Th>
                    <Th>触发</Th>
                    <Th>状态</Th>
                    <Th>上次检查</Th>
                    <Th className="w-10" />
                  </tr>
                </thead>
                <tbody>
                  {details.map(detail => {
                    const s = detail.source
                    const st = sourceState(detail)
                    return (
                      <tr key={s.id}>
                        <Td>
                          <code className="text-fg">{s.id}</code>
                        </Td>
                        <Td>
                          <Badge className="mr-2">{kindLabels[s.kind] ?? s.kind}</Badge>
                          <span className="font-mono text-xs text-dim" title={s.root}>
                            {s.root.length > 36 ? `…${s.root.slice(-36)}` : s.root} → {s.mount || '/'}
                            {s.git_ref ? ` @ ${s.git_ref}` : ''}
                          </span>
                          {detail.status.error && <p className="mt-1 text-xs text-err">{detail.status.error}</p>}
                        </Td>
                        <Td className="whitespace-nowrap">{triggerText(detail.policy)}</Td>
                        <Td className="whitespace-nowrap">
                          <span className="inline-flex items-center gap-1.5">
                            <Dot tone={st.tone} />
                            {st.label}
                          </span>
                        </Td>
                        <Td className="whitespace-nowrap font-mono text-xs" title={`成功构建 ${when(detail.status.last_success)}`}>
                          {shortTime(detail.status.last_checked)}
                        </Td>
                        <Td>
                          <Menu>
                            <MenuTrigger asChild>
                              <Button size="icon-sm" variant="ghost" aria-label={`${s.id} 的操作`}>
                                <MoreHorizontal />
                              </Button>
                            </MenuTrigger>
                            <MenuContent>
                              <MenuItem onSelect={() => setPolicyFor(detail)}>自动更新与推送通知</MenuItem>
                              <MenuItem onSelect={() => setPatternFor(detail)}>包含 / 排除</MenuItem>
                              <MenuItem onSelect={() => void toggleSource(detail)}>{s.options?.disabled ? '启用来源' : '停用来源'}</MenuItem>
                              <MenuSeparator />
                              <MenuItem className="text-err" onSelect={() => void removeSource(detail)}>
                                移除来源
                              </MenuItem>
                            </MenuContent>
                          </Menu>
                        </Td>
                      </tr>
                    )
                  })}
                </tbody>
              </Table>
            )}
          </Panel>
        ) : (
          <Panel>
            <PanelHeader title="来源" />
            <Empty title="查看和管理来源需要 Maintainer 角色">当前身份可以阅读已发布文件，也可以把检索范围限定到单个文件。</Empty>
          </Panel>
        )}

        <Panel>
          <PanelHeader title="构建记录" meta={<span>本知识库</span>} actions={<Button size="sm" variant="ghost" onClick={() => navigate('/jobs')}>全部任务</Button>} />
          {builds.data && builds.data.items.length === 0 && <Empty title="还没有构建任务" />}
          {builds.data && builds.data.items.length > 0 && (
            <Table>
              <thead>
                <tr>
                  <Th>任务</Th>
                  <Th>方式</Th>
                  <Th>状态</Th>
                  <Th>变更 / 失败</Th>
                  <Th>开始</Th>
                </tr>
              </thead>
              <tbody>
                {builds.data.items.slice(0, 8).map(job => (
                  <tr key={job.id} className="cursor-pointer hover:bg-hover" onClick={() => navigate(`/jobs?id=${job.id}`)}>
                    <Td className="font-mono text-xs text-fg" title={job.id}>
                      {tail(job.id, 12)}
                    </Td>
                    <Td>{buildModes.find(m => m.mode === job.payload?.mode)?.label ?? job.payload?.mode ?? '—'}</Td>
                    <Td>
                      <StateBadge state={job.state} />
                    </Td>
                    <Td className="font-mono text-xs">
                      {job.result ? `${job.result.changed ?? 0} / ${job.result.failed ?? 0}` : '—'}
                    </Td>
                    <Td className="whitespace-nowrap font-mono text-xs">{shortTime(job.created)}</Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Panel>
      </div>

      <Panel>
        <PanelHeader
          title="已发布文件"
          meta={
            <>
              <span>{files.data ? `${files.data.total.value ?? files.data.returned} 个文件版本` : ''}</span>
              <span>按文件发布 · 草稿不在这里</span>
            </>
          }
        />
        {files.loading && !files.data && <Loading />}
        <ErrorLine className="m-4">{files.error}</ErrorLine>
        {files.data && files.data.items.length === 0 && <Empty mark title="还没有已发布文件">登记来源或上传文件后，构建成功的文件会出现在这里。</Empty>}
        {files.data && files.data.items.length > 0 && (
          <Table>
            <thead>
              <tr>
                <Th>路径</Th>
                <Th>标题</Th>
                {(d?.fields ?? []).filter((f: string) => f !== 'title').map((f: string) => (
                  <Th key={f}>{f}</Th>
                ))}
                <Th>版本</Th>
                <Th className="text-right">操作</Th>
              </tr>
            </thead>
            <tbody>
              {files.data.items.map(item => (
                <tr key={item.path}>
                  <Td className="font-mono text-xs text-fg">{item.path}</Td>
                  <Td>{item.title}</Td>
                  {(d?.fields ?? []).filter((f: string) => f !== 'title').map((f: string) => (
                    <Td key={f}>{item.fields?.[f] !== undefined ? String(item.fields[f]) : <span className="text-faint">—</span>}</Td>
                  ))}
                  <Td className="font-mono text-xs" title={item.edition_id}>
                    {tail(item.edition_id)}
                  </Td>
                  <Td className="whitespace-nowrap text-right">
                    <div className="flex justify-end gap-1">
                      <Button size="sm" variant="ghost" onClick={() => item.ref && setReading({ ref: item.ref, kb, path: item.path, title: item.title })}>
                        <BookOpenText />
                        阅读
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => void onlyThisFile(item)}>
                        <SearchIcon />
                        只查此文件
                      </Button>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      <RegisterSource open={registerOpen} onOpenChange={setRegisterOpen} onDone={() => void sources.reload()} />
      <UploadDialog open={uploadOpen} onOpenChange={setUploadOpen} />
      <PolicyDialog detail={policyFor} onClose={() => setPolicyFor(null)} onDone={() => void sources.reload()} />
      <PatternDialog detail={patternFor} onClose={() => setPatternFor(null)} onDone={() => void sources.reload()} />
      <ReaderSheet target={reading} onClose={() => setReading(null)} />
      <Dialog open={Boolean(estimate)} onOpenChange={open => !open && setEstimate(null)}>
        <DialogContent>
          <DialogTitle>构建估算</DialogTitle>
          <DialogBody className="space-y-3">
            <p className="text-[13px] text-fg-soft">
              {String(estimate?.files ?? '—')} 个文件 · {String(estimate?.characters ?? '—')} 字符 · 预计 {String(estimate?.estimated_chunks ?? '—')} 块 · 模型调用 {String(estimate?.models_called ?? 0)} 次
            </p>
            <Note>估算只读取来源，不写入任何索引。Git 来源可能会更新本机仓库缓存。</Note>
            <JsonBlock value={estimate} />
          </DialogBody>
        </DialogContent>
      </Dialog>
    </Page>
  )
}
