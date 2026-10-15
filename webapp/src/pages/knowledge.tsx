import { SyntheticEvent, useEffect, useState } from 'react'
import { BookmarkPlus, ListPlus, MoreHorizontal, Plus, Sparkles, X } from 'lucide-react'
import { api, KnowledgeItem } from '../lib/api'
import { useData, useFeedback, useRun } from '../lib/feedback'
import { navigate } from '../lib/router'
import { useSession } from '../lib/session'
import { cn, describeRef, tail, when } from '../lib/utils'
import { Badge, Tone } from '../components/badge'
import { Button } from '../components/button'
import { JsonBlock } from '../components/code'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Check, Field, Input, Select, Textarea } from '../components/input'
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '../components/menu'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, Loading, Note, Panel, PanelBody, PanelHeader } from '../components/panel'
import { Table, Td, Th } from '../components/table'

const kindLabels: Record<string, string> = { case: '案例', procedure: '步骤', fact: '事实', rule: '规则', wiki: 'Wiki' }
const stateInfo: Record<string, { label: string; tone: Tone }> = {
  draft: { label: '草稿', tone: 'warn' },
  published: { label: '已发布', tone: 'ok' },
  blocked: { label: '依据已撤回', tone: 'err' },
  archived: { label: '已归档', tone: 'off' }
}

type Draft = { id?: string; expected_revision?: string; title: string; kind: string; body_md: string; refs: string; applies_to: string; not_applies_to: string }
const blank: Draft = { title: '', kind: 'case', body_md: '', refs: '', applies_to: '', not_applies_to: '' }

function Editor({ draft, setDraft, onSaved }: { draft: Draft; setDraft: (d: Draft) => void; onSaved: (k: KnowledgeItem) => void }) {
  const { kb, cites, removeCite, clearCites } = useSession()
  const run = useRun()
  const set = (key: keyof Draft) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => setDraft({ ...draft, [key]: e.target.value })
  const kbCites = cites.filter(c => c.kb === kb)
  const refList = draft.refs.split(/\s+/).filter(Boolean)

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const body: Record<string, unknown> = {
      title: draft.title,
      kind: draft.kind,
      body_md: draft.body_md,
      refs: draft.refs.split(/\s+/).filter(Boolean),
      applies_to: draft.applies_to,
      not_applies_to: draft.not_applies_to
    }
    if (draft.id) {
      body.id = draft.id
      body.expected_revision = draft.expected_revision
    }
    const saved = await run(async () => {
      if (!refList.length) throw new Error('至少需要一条依据引用')
      return api.saveKnowledge(kb, body)
    }, '草稿已保存；已发布版本不受影响')
    if (saved) onSaved(saved)
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_10rem]">
        <Field label="标题">
          <Input required value={draft.title} onChange={set('title')} placeholder="例如：日志丢失的调查步骤" />
        </Field>
        <Field label="类型">
          <Select value={draft.kind} onChange={set('kind')} disabled={draft.kind === 'wiki'}>
            {Object.entries(kindLabels).map(([v, l]) => (
              <option key={v} value={v} disabled={v === 'wiki' && draft.kind !== 'wiki'}>
                {l}
              </option>
            ))}
          </Select>
        </Field>
      </div>
      <Field label="正文（Markdown）" hint="说明情境、步骤、结果；不确定的内容写明不确定。">
        <Textarea required rows={9} value={draft.body_md} onChange={set('body_md')} />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="适用情境">
          <Textarea rows={2} value={draft.applies_to} onChange={set('applies_to')} />
        </Field>
        <Field label="不适用条件">
          <Textarea rows={2} value={draft.not_applies_to} onChange={set('not_applies_to')} />
        </Field>
      </div>
      <div className="space-y-1.5">
        <span className="block text-xs text-dim">依据引用</span>
        {refList.length === 0 ? (
          <p className="rounded-md border border-dashed border-line-strong px-3 py-2.5 text-xs text-faint">还没有依据。在检索结果上点“用作草稿依据”，再回来填入。没有依据的条目不能保存。</p>
        ) : (
          <ul className="divide-y divide-line rounded-md border border-line-strong">
            {refList.map(ref => {
              const info = describeRef(ref)
              return (
                <li key={ref} className="flex items-center gap-2 px-3 py-1.5 text-xs">
                  <span className="min-w-0 flex-1 truncate font-mono text-fg-soft" title={ref}>
                    {info ? `${info.path || (info.result ? `结果 ${tail(info.result, 10)}` : '依据')}${info.chunk ? ' · 文本块' : info.range ? ' · 原文片段' : ''}` : ref}
                  </span>
                  {info?.edition && <span className="font-mono text-faint">版本 {tail(info.edition)}</span>}
                  <button type="button" className="rounded p-0.5 text-faint hover:text-fg" aria-label="移除这条引用" onClick={() => setDraft({ ...draft, refs: refList.filter(r => r !== ref).join('\n') })}>
                    <X className="h-3.5 w-3.5" />
                  </button>
                </li>
              )
            })}
          </ul>
        )}
        <details className="text-xs text-faint">
          <summary className="cursor-pointer hover:text-dim">手动编辑引用（每行一个 xr1 引用）</summary>
          <Textarea mono rows={3} value={draft.refs} onChange={set('refs')} spellCheck={false} className="mt-1.5" />
        </details>
      </div>
      {kbCites.length > 0 && (
        <div className="rounded-lg border border-line p-3">
          <div className="mb-2 flex items-center gap-2 text-xs text-dim">
            <BookmarkPlus className="h-3.5 w-3.5" />
            检索时收集的依据 {kbCites.length} 条
            <span className="ml-auto flex gap-1.5">
              <Button
                size="sm"
                onClick={() => {
                  const existing = new Set(draft.refs.split(/\s+/).filter(Boolean))
                  const added = kbCites.map(c => c.ref).filter(r => !existing.has(r))
                  setDraft({ ...draft, refs: [...existing, ...added].join('\n') })
                }}
              >
                <ListPlus />
                全部填入
              </Button>
              <Button size="sm" variant="ghost" onClick={clearCites}>
                <X />
                清空
              </Button>
            </span>
          </div>
          <ul className="space-y-1">
            {kbCites.map(c => (
              <li key={c.ref} className="flex items-center gap-2 font-mono text-xs text-fg-soft">
                <span className="min-w-0 flex-1 truncate">{c.label}</span>
                <button type="button" className="rounded p-0.5 text-faint hover:text-fg" onClick={() => removeCite(c.ref)} aria-label="移除这条依据">
                  <X className="h-3.5 w-3.5" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex items-center gap-2">
        <Button type="submit" variant="primary">
          保存草稿
        </Button>
        {draft.id && <span className="font-mono text-xs text-faint">编辑 {draft.id} · 草稿修订 {tail(draft.expected_revision ?? '')}</span>}
      </div>
    </form>
  )
}

function SyncTargets() {
  const { kb } = useSession()
  const run = useRun()
  const targets = useData(() => api.syncTargets(kb), [kb])
  const [open, setOpen] = useState(false)
  const [advanced, setAdvanced] = useState(false)
  const [t, setT] = useState({ id: '', name: '', kind: 'path', root: '', prefix: 'xrag-knowledge', branch: 'xrag/knowledge' })
  const [json, setJson] = useState(
    JSON.stringify(
      { id: 'object-out', name: '对象存储', kind: 's3', root: 'https://s3.example', prefix: 'xrag-knowledge', auto_sync: false, remote: { bucket: 'your-bucket', region: 'us-east-1', access_key: 'env:S3_ACCESS_KEY', secret_key: 'env:S3_SECRET_KEY' } },
      null,
      2
    )
  )
  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const ok = await run(async () => {
      const target = advanced ? JSON.parse(json) : { id: t.id, name: t.name, kind: t.kind, root: t.root, prefix: t.prefix, ...(t.kind === 'git' ? { branch: t.branch } : {}) }
      return api.addSyncTarget(kb, target)
    }, '写回目标已登记，尚未写入任何文件')
    if (ok !== undefined) {
      setOpen(false)
      void targets.reload()
    }
  }
  const set = (k: keyof typeof t) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setT({ ...t, [k]: e.target.value })
  return (
    <Panel>
      <PanelHeader
        title="写回目标"
        meta={<span>默认只手动写回；auto_sync 目标在之后发布成功时排队</span>}
        actions={
          <Button size="sm" onClick={() => setOpen(true)}>
            <Plus />
            登记目标
          </Button>
        }
      />
      {targets.data && targets.data.items.length === 0 && <Empty title="还没有写回目标">登记服务器目录、本机 Git 专用分支或 S3 后，才能把已发布条目写回。</Empty>}
      {targets.data && targets.data.items.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>名称</Th>
              <Th>类型 · 位置</Th>
              <Th>前缀 · 分支</Th>
              <Th>自动</Th>
            </tr>
          </thead>
          <tbody>
            {targets.data.items.map(x => (
              <tr key={x.id}>
                <Td>
                  {x.name} <code className="ml-1 text-xs text-dim">{x.id}</code>
                </Td>
                <Td className="font-mono text-xs">
                  {x.kind} · {x.root}
                </Td>
                <Td className="font-mono text-xs">
                  {x.prefix}
                  {x.branch ? ` · ${x.branch}` : ''}
                </Td>
                <Td>{x.auto_sync ? '发布后自动' : '手动'}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogTitle>登记写回目标</DialogTitle>
          <form onSubmit={submit} className="contents">
            <DialogBody className="space-y-4">
              <Check label="高级 JSON（S3、自动同步、推送）" checked={advanced} onChange={e => setAdvanced(e.target.checked)} />
              {advanced ? (
                <Field label="目标 JSON" hint="auto_sync 是显式订阅；push=true 只推指定的 xrag/* 分支。凭据只填 env:/file: 引用。">
                  <Textarea mono rows={14} value={json} onChange={e => setJson(e.target.value)} spellCheck={false} />
                </Field>
              ) : (
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="目标 ID">
                    <Input mono required pattern="[A-Za-z0-9_-]+" value={t.id} onChange={set('id')} />
                  </Field>
                  <Field label="名称">
                    <Input required value={t.name} onChange={set('name')} />
                  </Field>
                  <Field label="类型">
                    <Select value={t.kind} onChange={set('kind')}>
                      <option value="path">本机目录</option>
                      <option value="git">本机 Git</option>
                    </Select>
                  </Field>
                  <Field label="写入前缀">
                    <Input mono required value={t.prefix} onChange={set('prefix')} />
                  </Field>
                  <Field label="根目录" className="sm:col-span-2">
                    <Input mono required value={t.root} onChange={set('root')} placeholder="服务器上的绝对路径" />
                  </Field>
                  {t.kind === 'git' && (
                    <Field label="Git 分支">
                      <Input mono value={t.branch} onChange={set('branch')} />
                    </Field>
                  )}
                </div>
              )}
              <Note>已有目标不能原地改地址；需要时用新 ID 登记。</Note>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                取消
              </Button>
              <Button type="submit" variant="primary">
                确认登记（不写文件）
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Panel>
  )
}

export default function KnowledgePage() {
  const { kb, may, caps, profile } = useSession()
  const run = useRun()
  const { confirm } = useFeedback()
  const list = useData(() => api.knowledge(kb), [kb])
  const [draft, setDraft] = useState<Draft>(blank)
  const [mode, setMode] = useState<'edit' | 'view'>('edit')
  const [viewing, setViewing] = useState<KnowledgeItem | null>(null)
  const [records, setRecords] = useState<unknown>(null)
  const [wikiOpen, setWikiOpen] = useState(false)
  const [topic, setTopic] = useState('')
  const [regen, setRegen] = useState<KnowledgeItem | null>(null)
  const [regenForm, setRegenForm] = useState({ topic: '', path: '' })
  const [syncFor, setSyncFor] = useState<KnowledgeItem | null>(null)
  const [syncTarget, setSyncTarget] = useState('')
  const targets = useData(may('maintainer') ? () => api.syncTargets(kb) : null, [kb, syncFor])

  useEffect(() => {
    setDraft(blank)
    setViewing(null)
  }, [kb])

  async function open(item: KnowledgeItem, edit: boolean) {
    const k = await run(() => api.knowledgeItem(item.id))
    if (!k) return
    if (edit) {
      setDraft({
        id: k.id,
        expected_revision: k.draft_revision,
        title: k.title,
        kind: k.kind,
        body_md: k.body_md ?? '',
        refs: (k.refs ?? []).join('\n'),
        applies_to: k.applies_to ?? '',
        not_applies_to: k.not_applies_to ?? ''
      })
      setMode('edit')
    } else {
      setViewing(k)
      setMode('view')
    }
  }

  async function publish(item: KnowledgeItem) {
    const ok = await confirm({ title: `发布“${item.title}”的当前草稿？`, body: '发布会排队一次构建；构建成功后才进入检索。之前的已发布版本保留在修订记录里。', confirm: '发布' })
    if (!ok) return
    const job = await run(() => api.publishKnowledge(item.id, item.draft_revision))
    if (job) navigate(`/jobs?id=${job.id}`)
  }

  async function archive(item: KnowledgeItem) {
    const ok = await confirm({ title: `归档“${item.title}”？`, body: '条目会移出当前检索视图，历史引用仍然保留。这不是强制撤回。', confirm: '归档', danger: true })
    if (!ok || !item.published_revision) return
    await run(() => api.archiveKnowledge(item.id, item.published_revision!), '条目已归档')
    void list.reload()
  }

  async function showRecords(item: KnowledgeItem) {
    const revisions = await run(() => api.knowledgeRevisions(item.id))
    const sync = may('maintainer') ? await run(() => api.knowledgeSyncs(item.id)) : undefined
    if (revisions) setRecords({ revisions, sync })
  }

  async function submitWiki(event: SyntheticEvent) {
    event.preventDefault()
    const job = await run(() => api.wiki(kb, topic))
    if (job) navigate(`/jobs?id=${job.id}`)
  }

  async function submitRegen(event: SyntheticEvent) {
    event.preventDefault()
    if (!regen) return
    const job = await run(() => api.regenerate(regen.id, { ...regenForm, expected_revision: regen.draft_revision }))
    if (job) navigate(`/jobs?id=${job.id}`)
  }

  async function submitSync(event: SyntheticEvent) {
    event.preventDefault()
    if (!syncFor?.published_revision) return
    const job = await run(() => api.syncKnowledge(syncFor.id, syncTarget, syncFor.published_revision!))
    if (job) navigate(`/jobs?id=${job.id}`)
  }

  const items = list.data?.items ?? []

  return (
    <Page>
      <PageHeader
        title="知识与 Wiki"
        desc="经验先写成草稿，明确发布后才参与检索。草稿和已发布版本分开保存，引用不会因为编辑而改变。"
        cli={`xrag knowledge revisions ITEM_ID --profile ${profile}`}
        actions={
          <>
            <Button onClick={() => setWikiOpen(true)} disabled={!caps.wiki} title={caps.wiki ? undefined : '本库未配置 Chat 模型'}>
              <Sparkles />
              生成 Wiki 草稿
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                setDraft(blank)
                setMode('edit')
              }}
            >
              <Plus />
              新建草稿
            </Button>
          </>
        }
      />
      <div className="grid gap-3 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:items-start">
        <Panel>
          <PanelHeader title="条目" meta={<span>{items.length} 条</span>} />
          {list.loading && !list.data && <Loading />}
          <ErrorLine className="m-4">{list.error}</ErrorLine>
          {list.data && items.length === 0 && <Empty mark title="还没有知识条目">在检索结果上点“用作草稿依据”，再回到这里写草稿。</Empty>}
          <ul>
            {items.map(item => {
              const st = stateInfo[item.state] ?? { label: item.state, tone: 'neutral' as Tone }
              const active = draft.id === item.id || viewing?.id === item.id
              return (
                <li key={item.id} className={cn('flex items-start gap-3 border-b px-4 py-3', active && 'bg-raised shadow-[inset_2px_0_0_var(--color-fg)]')}>
                  <button type="button" className="min-w-0 flex-1 text-left" onClick={() => void open(item, false)}>
                    <span className="flex items-center gap-2">
                      <b className="truncate font-semibold">{item.title}</b>
                      <Badge tone={st.tone} dot>
                        {st.label}
                      </Badge>
                    </span>
                    <span className="mt-1 block font-mono text-xs text-dim">
                      {kindLabels[item.kind] ?? item.kind} · 草稿 {tail(item.draft_revision)} · 已发布 {item.published_revision ? tail(item.published_revision) : '无'}
                    </span>
                  </button>
                  <Menu>
                    <MenuTrigger asChild>
                      <Button size="icon-sm" variant="ghost" aria-label={`${item.title} 的操作`}>
                        <MoreHorizontal />
                      </Button>
                    </MenuTrigger>
                    <MenuContent>
                      <MenuItem onSelect={() => void open(item, true)}>编辑草稿</MenuItem>
                      <MenuItem disabled={item.state === 'blocked'} onSelect={() => void publish(item)}>
                        发布当前草稿
                      </MenuItem>
                      <MenuItem onSelect={() => void showRecords(item)}>修订与写回记录</MenuItem>
                      {may('maintainer') && item.kind === 'wiki' && item.state !== 'blocked' && (
                        <MenuItem
                          onSelect={() => {
                            setRegen(item)
                            setRegenForm({ topic: item.title, path: '' })
                          }}
                        >
                          再生成新草稿
                        </MenuItem>
                      )}
                      {may('maintainer') && item.published_revision && !['blocked', 'archived'].includes(item.state) && (
                        <>
                          <MenuSeparator />
                          <MenuItem onSelect={() => setSyncFor(item)}>写回已发布内容</MenuItem>
                          <MenuItem className="text-err" onSelect={() => void archive(item)}>
                            移出当前发布
                          </MenuItem>
                        </>
                      )}
                    </MenuContent>
                  </Menu>
                </li>
              )
            })}
          </ul>
        </Panel>

        <Panel>
          <PanelHeader title={mode === 'view' && viewing ? viewing.title : draft.id ? '编辑草稿' : '新的知识草稿'} meta={mode === 'view' && viewing && <span>草稿管理视图 · {when(viewing.updated)}</span>} actions={mode === 'view' && viewing && <Button size="sm" onClick={() => void open(viewing, true)}>编辑</Button>} />
          <PanelBody>
            {mode === 'view' && viewing ? (
              <div className="space-y-4">
                <pre className="whitespace-pre-wrap break-words font-sans text-[13.5px] leading-relaxed text-fg-soft">{viewing.body_md}</pre>
                {(viewing.applies_to || viewing.not_applies_to) && (
                  <div className="grid gap-3 text-[13px] sm:grid-cols-2">
                    <p>
                      <span className="block text-xs text-dim">适用</span>
                      {viewing.applies_to || '—'}
                    </p>
                    <p>
                      <span className="block text-xs text-dim">不适用</span>
                      {viewing.not_applies_to || '—'}
                    </p>
                  </div>
                )}
                <JsonBlock value={viewing} summary="完整条目" />
              </div>
            ) : (
              <Editor
                draft={draft}
                setDraft={setDraft}
                onSaved={k => {
                  setDraft({ ...draft, id: k.id, expected_revision: k.draft_revision })
                  void list.reload()
                }}
              />
            )}
          </PanelBody>
        </Panel>
      </div>

      {may('maintainer') && <SyncTargets />}

      <Dialog open={wikiOpen} onOpenChange={setWikiOpen}>
        <DialogContent className="max-w-md">
          <DialogTitle>生成 Wiki 主题草稿</DialogTitle>
          <form onSubmit={submitWiki} className="contents">
            <DialogBody className="space-y-3">
              <Field label="主题">
                <Input required value={topic} onChange={e => setTopic(e.target.value)} placeholder="例如：日志" />
              </Field>
              <Note>以非 Wiki 的原始资料为依据，调用已配置的 Chat 模型生成草稿，费用计入本库预算；发布需要你在草稿页确认。</Note>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setWikiOpen(false)}>
                取消
              </Button>
              <Button type="submit" variant="primary">
                提交生成
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(regen)} onOpenChange={o => !o && setRegen(null)}>
        <DialogContent className="max-w-md">
          <DialogTitle>再生成 · {regen?.title}</DialogTitle>
          <form onSubmit={submitRegen} className="contents">
            <DialogBody className="space-y-3">
              <Field label="主题">
                <Input required value={regenForm.topic} onChange={e => setRegenForm({ ...regenForm, topic: e.target.value })} />
              </Field>
              <Field label="限定来源目录（可空）">
                <Input mono value={regenForm.path} onChange={e => setRegenForm({ ...regenForm, path: e.target.value })} placeholder="manual/" />
              </Field>
              <Note>只选原始资料，排除全部知识条目和 Wiki，避免引用自己的总结。当前已发布正文不变。</Note>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setRegen(null)}>
                取消
              </Button>
              <Button type="submit" variant="primary">
                提交再生成
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(syncFor)} onOpenChange={o => !o && setSyncFor(null)}>
        <DialogContent className="max-w-md">
          <DialogTitle>写回 · {syncFor?.title}</DialogTitle>
          <form onSubmit={submitSync} className="contents">
            <DialogBody className="space-y-3">
              {targets.data?.items.length ? (
                <Field label="写回目标">
                  <Select required value={syncTarget} onChange={e => setSyncTarget(e.target.value)}>
                    <option value="">选择目标</option>
                    {targets.data.items.map(t => (
                      <option key={t.id} value={t.id}>
                        {t.name} · {t.kind} · {t.root}
                      </option>
                    ))}
                  </Select>
                </Field>
              ) : (
                <p className="text-[13px] text-dim">还没有写回目标，先在下方“写回目标”登记。</p>
              )}
              <Note>只写当前已发布版本。目标被他人修改时拒绝覆盖；Git 默认只写本机分支。</Note>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setSyncFor(null)}>
                取消
              </Button>
              <Button type="submit" variant="primary" disabled={!syncTarget}>
                确认写回
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(records)} onOpenChange={o => !o && setRecords(null)}>
        <DialogContent side="sheet">
          <DialogTitle>修订与写回记录</DialogTitle>
          <DialogBody>
            <JsonBlock value={records} open />
          </DialogBody>
        </DialogContent>
      </Dialog>
    </Page>
  )
}
